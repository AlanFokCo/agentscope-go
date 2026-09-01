package middleware

import (
	"context"
	"errors"
	"strings"
	"testing"

	agenterrors "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/errors"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
)

func actingCtx(t *testing.T) (context.Context, MiddleContext) {
	t.Helper()
	mc := MiddleContext{}
	return WithMiddleContext(context.Background(), mc), mc
}

func runActing(m *RepetitionBreakerMiddleware, ctx context.Context, name, input string, fail bool) (*tool.ToolResponse, error) {
	next := func(_ context.Context, _ *ActingInput) (*tool.ToolResponse, error) {
		if fail {
			return nil, errors.New("transient failure")
		}
		return tool.NewTextResponse("ok"), nil
	}
	in := &ActingInput{AgentName: "agent", ToolCall: message.ToolCallBlock{Type: "tool_call", ID: "tc", Name: name, Input: input}}
	return m.OnActing(ctx, in, next)
}

func TestRepetitionBreaker_BreaksAfterThreshold(t *testing.T) {
	m := NewRepetitionBreaker() // threshold 3
	ctx, _ := actingCtx(t)

	for i := 0; i < 3; i++ {
		if _, err := runActing(m, ctx, "search", `{"q":"x"}`, false); err != nil {
			t.Fatalf("call %d should pass, got %v", i+1, err)
		}
	}
	// 4th identical success: past the threshold -> typed error.
	_, err := runActing(m, ctx, "search", `{"q":"x"}`, false)
	if !errors.Is(err, agenterrors.ErrToolRepetition) {
		t.Fatalf("expected ErrToolRepetition, got %v", err)
	}
}

func TestRepetitionBreaker_HintAtThreshold(t *testing.T) {
	m := NewRepetitionBreaker()
	ctx, _ := actingCtx(t)
	for i := 0; i < 3; i++ {
		_, _ = runActing(m, ctx, "search", `{"q":"x"}`, false)
	}
	prompt := m.OnSystemPrompt(ctx, "agent", "base prompt")
	if !strings.Contains(prompt, "base prompt") || !strings.Contains(prompt, "different approach") {
		t.Errorf("hint not injected at threshold: %q", prompt)
	}
}

func TestRepetitionBreaker_DifferentInputsDoNotTrip(t *testing.T) {
	m := NewRepetitionBreaker()
	ctx, _ := actingCtx(t)
	for i := 0; i < 6; i++ {
		input := `{"q":"x` + string(rune('a'+i)) + `"}`
		if _, err := runActing(m, ctx, "search", input, false); err != nil {
			t.Fatalf("varying inputs must not trip: %v", err)
		}
	}
}

func TestRepetitionBreaker_FailedRetriesResetStreak(t *testing.T) {
	m := NewRepetitionBreaker()
	ctx, _ := actingCtx(t)
	_, _ = runActing(m, ctx, "fetch", `{}`, false)
	_, _ = runActing(m, ctx, "fetch", `{}`, false)
	// A failure resets the streak...
	_, _ = runActing(m, ctx, "fetch", `{}`, true)
	// ...so two more successes are below the threshold.
	if _, err := runActing(m, ctx, "fetch", `{}`, false); err != nil {
		t.Fatalf("post-failure retry should pass: %v", err)
	}
	if _, err := runActing(m, ctx, "fetch", `{}`, false); err != nil {
		t.Fatalf("post-failure retry should pass: %v", err)
	}
}

func TestRepetitionBreaker_AllowlistExempts(t *testing.T) {
	m := NewRepetitionBreaker(WithRepetitionAllowlist("read_file"))
	ctx, _ := actingCtx(t)
	for i := 0; i < 10; i++ {
		if _, err := runActing(m, ctx, "read_file", `{"p":"/a"}`, false); err != nil {
			t.Fatalf("allowlisted tool must never trip: %v", err)
		}
	}
}

func TestRepetitionBreaker_ResetPerReply(t *testing.T) {
	m := NewRepetitionBreaker()
	ctx, mc := actingCtx(t)
	for i := 0; i < 3; i++ {
		_, _ = runActing(m, ctx, "search", `{"q":"x"}`, false)
	}
	// New reply resets state.
	m.OnReply(ctx, ReplyInput{AgentName: "agent"}, func(_ context.Context, _ ReplyInput) <-chan event.Event {
		return nil
	})
	if _, err := runActing(m, ctx, "search", `{"q":"x"}`, false); err != nil {
		t.Fatalf("streak must reset per reply: %v", err)
	}
	_ = mc
}
