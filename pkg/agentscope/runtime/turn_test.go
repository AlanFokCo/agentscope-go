package runtime

import (
	"context"
	"testing"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/loop"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
)

func TestTurnForwardsEvents(t *testing.T) {
	mc := &turnMockModelCaller{
		resp: &model.ChatResponse{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "hello"}},
			IsLast:  true,
		},
	}
	l := loop.New(loop.WithModelCaller(mc), loop.WithMaxIters(1))

	turn := NewTurn(TurnConfig{Loop: l})
	var events []event.Event
	for ev := range turn.Run(context.Background(), "hi") {
		events = append(events, ev)
	}

	if len(events) == 0 {
		t.Fatal("expected events from turn")
	}

	hasReplyEnd := false
	for _, ev := range events {
		if ev.GetEventType() == event.EventReplyEnd {
			hasReplyEnd = true
		}
	}
	if !hasReplyEnd {
		t.Error("missing ReplyEnd event")
	}
}

func TestTurnFiresHooks(t *testing.T) {
	mc := &turnMockModelCaller{
		resp: &model.ChatResponse{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "ok"}},
			IsLast:  true,
		},
	}
	l := loop.New(loop.WithModelCaller(mc), loop.WithMaxIters(1))

	hooks := NewSessionHookManager()
	var fired []SessionHookEvent
	hooks.Register(&FuncHook{
		Fn:   func(e SessionHookEvent, _ any) error { fired = append(fired, e); return nil },
		Evts: []SessionHookEvent{HookPreTurn, HookPostTurn},
	})

	turn := NewTurn(TurnConfig{Loop: l, Hooks: hooks})
	for ev := range turn.Run(context.Background(), "test") {
		_ = ev
	}

	if len(fired) != 2 {
		t.Fatalf("got %d hooks fired, want 2", len(fired))
	}
	if fired[0] != HookPreTurn {
		t.Errorf("first hook = %v, want HookPreTurn", fired[0])
	}
	if fired[1] != HookPostTurn {
		t.Errorf("second hook = %v, want HookPostTurn", fired[1])
	}
}

func TestTurnBudgetExceeded(t *testing.T) {
	mc := &turnMockModelCaller{
		resp: &model.ChatResponse{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "ok"}},
			IsLast:  true,
		},
	}
	l := loop.New(loop.WithModelCaller(mc), loop.WithMaxIters(1))
	bt := NewBudgetTracker(Budget{MaxTurns: 1})
	bt.AddTurn() // exhaust the budget

	turn := NewTurn(TurnConfig{Loop: l, Budget: bt})
	var events []event.Event
	for ev := range turn.Run(context.Background(), "should fail") {
		events = append(events, ev)
	}

	hasBudgetEvent := false
	for _, ev := range events {
		if ce, ok := ev.(event.CustomEvent); ok && ce.Name == "turn.budget_exceeded" {
			hasBudgetEvent = true
		}
	}
	if !hasBudgetEvent {
		t.Error("expected turn.budget_exceeded event")
	}
}

func TestTurnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	mc := &turnMockModelCaller{
		resp: &model.ChatResponse{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "never"}},
			IsLast:  true,
		},
	}
	l := loop.New(loop.WithModelCaller(mc), loop.WithMaxIters(1))

	turn := NewTurn(TurnConfig{Loop: l})
	var events []event.Event
	for ev := range turn.Run(ctx, "canceled") {
		events = append(events, ev)
	}
	// Should complete without hanging
	if len(events) == 0 {
		t.Log("no events (expected with canceled context)")
	}
}

func TestTurnID(t *testing.T) {
	turn := NewTurn(TurnConfig{})
	if turn.ID() == "" {
		t.Error("Turn ID should not be empty")
	}
}

func TestTurnNilLoop(t *testing.T) {
	turn := NewTurn(TurnConfig{})
	var events []event.Event
	for ev := range turn.Run(context.Background(), "test") {
		events = append(events, ev)
	}

	hasError := false
	for _, ev := range events {
		if ce, ok := ev.(event.CustomEvent); ok && ce.Name == "turn.error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected turn.error event when loop is nil")
	}
}

// --- Mock ---

type turnMockModelCaller struct {
	resp *model.ChatResponse
}

func (m *turnMockModelCaller) Call(_ context.Context, _ []*message.Msg, _ []model.ToolSchema) (*model.ChatResponse, error) {
	return m.resp, nil
}
