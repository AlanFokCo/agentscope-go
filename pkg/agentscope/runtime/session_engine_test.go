package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/loop"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
)

func TestSessionEngineSubmitMessage(t *testing.T) {
	mc := &seMockModelCaller{
		resp: &model.ChatResponse{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "response"}},
			IsLast:  true,
		},
	}

	se := NewSessionEngine(SessionEngineConfig{
		LoopOptions: []loop.Option{loop.WithModelCaller(mc), loop.WithMaxIters(1)},
	})

	var events []event.Event
	for ev := range se.SubmitMessage(context.Background(), "hello") {
		events = append(events, ev)
	}

	if len(events) == 0 {
		t.Fatal("expected events")
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

func TestSessionEngineID(t *testing.T) {
	se := NewSessionEngine(SessionEngineConfig{})
	if se.ID() == "" {
		t.Error("SessionEngine ID should not be empty")
	}
}

func TestSessionEngineInterrupt(t *testing.T) {
	// Create a model caller that blocks until context is cancelled
	mc := &blockingModelCaller{done: make(chan struct{})}
	se := NewSessionEngine(SessionEngineConfig{
		LoopOptions: []loop.Option{loop.WithModelCaller(mc), loop.WithMaxIters(10)},
	})

	ch := se.SubmitMessage(context.Background(), "long task")

	// Wait a bit then interrupt
	time.Sleep(10 * time.Millisecond)
	se.Interrupt()

	// Drain events — should complete without hanging
	var events []event.Event
	for ev := range ch {
		events = append(events, ev)
	}
	close(mc.done)
	// Test passes if it doesn't hang
}

func TestSessionEngineBudgetEnforcement(t *testing.T) {
	mc := &seMockModelCaller{
		resp: &model.ChatResponse{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "ok"}},
			IsLast:  true,
		},
	}

	se := NewSessionEngine(SessionEngineConfig{
		LoopOptions: []loop.Option{loop.WithModelCaller(mc), loop.WithMaxIters(1)},
		Budget:      Budget{MaxTurns: 1},
	})

	// First turn should succeed
	for range se.SubmitMessage(context.Background(), "first") {
	}

	// Second turn should hit budget limit
	var events []event.Event
	for ev := range se.SubmitMessage(context.Background(), "second") {
		events = append(events, ev)
	}

	hasBudgetEvent := false
	for _, ev := range events {
		if ce, ok := ev.(event.CustomEvent); ok && ce.Name == "turn.budget_exceeded" {
			hasBudgetEvent = true
		}
	}
	if !hasBudgetEvent {
		t.Error("expected budget exceeded event on second turn")
	}
}

func TestSessionEngineSessionHooks(t *testing.T) {
	mc := &seMockModelCaller{
		resp: &model.ChatResponse{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "ok"}},
			IsLast:  true,
		},
	}

	se := NewSessionEngine(SessionEngineConfig{
		LoopOptions: []loop.Option{loop.WithModelCaller(mc), loop.WithMaxIters(1)},
	})

	var fired []SessionHookEvent
	se.Hooks().Register(&FuncHook{
		Fn:   func(e SessionHookEvent, _ any) error { fired = append(fired, e); return nil },
		Evts: []SessionHookEvent{HookSessionStart, HookSessionEnd, HookPreTurn, HookPostTurn},
	})

	for range se.SubmitMessage(context.Background(), "test") {
	}

	expected := []SessionHookEvent{HookSessionStart, HookPreTurn, HookPostTurn, HookSessionEnd}
	if len(fired) != len(expected) {
		t.Fatalf("fired %d hooks, want %d: %v", len(fired), len(expected), fired)
	}
	for i, want := range expected {
		if fired[i] != want {
			t.Errorf("fired[%d] = %v, want %v", i, fired[i], want)
		}
	}
}

func TestSessionEngineStatePersisted(t *testing.T) {
	mc := &seMockModelCaller{
		resp: &model.ChatResponse{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "ok"}},
			IsLast:  true,
		},
	}

	store := NewInMemorySessionStore()
	se := NewSessionEngine(SessionEngineConfig{
		LoopOptions: []loop.Option{loop.WithModelCaller(mc), loop.WithMaxIters(1)},
		Store:       store,
	})

	for range se.SubmitMessage(context.Background(), "test") {
	}

	loaded, err := store.Load(se.ID())
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if loaded.ID != se.ID() {
		t.Errorf("persisted ID = %q, want %q", loaded.ID, se.ID())
	}
}

func TestSessionEngineState(t *testing.T) {
	se := NewSessionEngine(SessionEngineConfig{})
	state := se.State()
	if state.ID != se.ID() {
		t.Errorf("State().ID = %q, want %q", state.ID, se.ID())
	}
	if state.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

// --- Mocks ---

type seMockModelCaller struct {
	resp *model.ChatResponse
}

func (m *seMockModelCaller) Call(_ context.Context, _ []*message.Msg, _ []model.ToolSchema) (*model.ChatResponse, error) {
	return m.resp, nil
}

type blockingModelCaller struct {
	done chan struct{}
}

func (m *blockingModelCaller) Call(ctx context.Context, _ []*message.Msg, _ []model.ToolSchema) (*model.ChatResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.done:
		return &model.ChatResponse{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "done"}},
			IsLast:  true,
		}, nil
	}
}
