package middleware

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

// recordingTracer records span names and their nesting via context propagation.
type recordingTracer struct {
	mu    sync.Mutex
	spans []spanRecord
}

type spanRecord struct {
	name   string
	parent string // parent span name if nested, empty for root
	ended  bool
}

type spanCtxKey struct{}

func (t *recordingTracer) StartSpan(ctx context.Context, name string) (context.Context, func()) {
	t.mu.Lock()

	parent := ""
	if p, ok := ctx.Value(spanCtxKey{}).(string); ok {
		parent = p
	}
	idx := len(t.spans)
	t.spans = append(t.spans, spanRecord{name: name, parent: parent})
	t.mu.Unlock()

	ctx = context.WithValue(ctx, spanCtxKey{}, name)
	return ctx, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.spans[idx].ended = true
	}
}

func TestTracingMiddleware_ReplySpan(t *testing.T) {
	tracer := &recordingTracer{}
	mw := NewTracingMiddleware(tracer)

	next := func(ctx context.Context, input ReplyInput) <-chan event.Event {
		ch := make(chan event.Event, 1)
		ch <- event.NewReplyEndEvent("s1", "r1")
		close(ch)
		return ch
	}

	ch := mw.OnReply(context.Background(), ReplyInput{AgentName: "TestAgent"}, next)
	for range ch {
	}

	if len(tracer.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(tracer.spans))
	}
	s := tracer.spans[0]
	if !strings.Contains(s.name, "invoke_agent TestAgent") {
		t.Errorf("expected invoke_agent span, got %q", s.name)
	}
	if !s.ended {
		t.Error("span should be ended after reply completes")
	}
}

func TestTracingMiddleware_ModelCallSpan(t *testing.T) {
	tracer := &recordingTracer{}
	mw := NewTracingMiddleware(tracer)

	core := func(_ context.Context, _ ModelCallInput) (*model.ChatResponse, error) {
		return &model.ChatResponse{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "hi"}},
		}, nil
	}

	_, err := mw.OnModelCall(context.Background(), ModelCallInput{AgentName: "test"}, core)
	if err != nil {
		t.Fatal(err)
	}

	if len(tracer.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(tracer.spans))
	}
	if tracer.spans[0].name != "chat" {
		t.Errorf("expected 'chat' span, got %q", tracer.spans[0].name)
	}
	if !tracer.spans[0].ended {
		t.Error("chat span should be ended")
	}
}

func TestTracingMiddleware_ActingSpan(t *testing.T) {
	tracer := &recordingTracer{}
	mw := NewTracingMiddleware(tracer)

	core := func(_ context.Context, _ ActingInput) (*tool.ToolResponse, error) {
		return tool.NewTextResponse("done"), nil
	}

	_, err := mw.OnActing(context.Background(), ActingInput{
		AgentName: "test",
		ToolCall:  message.ToolCallBlock{Name: "search"},
	}, core)
	if err != nil {
		t.Fatal(err)
	}

	if len(tracer.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(tracer.spans))
	}
	if tracer.spans[0].name != "execute_tool search" {
		t.Errorf("expected 'execute_tool search', got %q", tracer.spans[0].name)
	}
}

func TestTracingMiddleware_NestedSpans(t *testing.T) {
	tracer := &recordingTracer{}
	mw := NewTracingMiddleware(tracer)

	// Simulate: OnReply wraps OnModelCall which wraps OnActing
	// Build chains
	modelCore := func(ctx context.Context, _ ModelCallInput) (*model.ChatResponse, error) {
		// Simulate a tool call within the model call
		actingCore := func(ctx context.Context, _ ActingInput) (*tool.ToolResponse, error) {
			return tool.NewTextResponse("tool result"), nil
		}
		_, _ = mw.OnActing(ctx, ActingInput{ToolCall: message.ToolCallBlock{Name: "calc"}}, actingCore)
		return &model.ChatResponse{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "hi"}},
		}, nil
	}

	replyCore := func(ctx context.Context, _ ReplyInput) <-chan event.Event {
		_, _ = mw.OnModelCall(ctx, ModelCallInput{}, modelCore)
		ch := make(chan event.Event)
		close(ch)
		return ch
	}

	ch := mw.OnReply(context.Background(), ReplyInput{AgentName: "Agent"}, replyCore)
	for range ch {
	}

	if len(tracer.spans) != 3 {
		t.Fatalf("expected 3 nested spans, got %d", len(tracer.spans))
	}

	// Verify nesting: invoke_agent -> chat -> execute_tool
	if tracer.spans[0].name != "invoke_agent Agent" {
		t.Errorf("span 0: %q", tracer.spans[0].name)
	}
	if tracer.spans[1].parent != "invoke_agent Agent" {
		t.Errorf("chat span should be child of invoke_agent, parent=%q", tracer.spans[1].parent)
	}
	if tracer.spans[2].parent != "chat" {
		t.Errorf("execute_tool should be child of chat, parent=%q", tracer.spans[2].parent)
	}

	for i, s := range tracer.spans {
		if !s.ended {
			t.Errorf("span %d (%s) not ended", i, s.name)
		}
	}
}

func TestTracingMiddleware_Key(t *testing.T) {
	mw := NewTracingMiddleware(nil)
	if mw.Key() != "tracing" {
		t.Errorf("expected key 'tracing', got %s", mw.Key())
	}
}

func TestTracingMiddleware_NilTracer_UsesGlobal(t *testing.T) {
	mw := NewTracingMiddleware(nil)
	if mw.Tracer == nil {
		t.Error("should fall back to global tracer instance")
	}
}

func TestTracingMiddleware_EventsPassthrough(t *testing.T) {
	tracer := &recordingTracer{}
	mw := NewTracingMiddleware(tracer)

	events := []event.Event{
		event.NewReplyStartEvent("s1", "r1", "agent", "assistant"),
		event.NewTextBlockDeltaEvent("r1", "b1", "hello"),
		event.NewReplyEndEvent("s1", "r1"),
	}

	next := func(_ context.Context, _ ReplyInput) <-chan event.Event {
		ch := make(chan event.Event, len(events))
		for _, ev := range events {
			ch <- ev
		}
		close(ch)
		return ch
	}

	ch := mw.OnReply(context.Background(), ReplyInput{AgentName: "test"}, next)
	var count int
	for range ch {
		count++
	}
	if count != 3 {
		t.Errorf("expected 3 pass-through events, got %d", count)
	}
}
