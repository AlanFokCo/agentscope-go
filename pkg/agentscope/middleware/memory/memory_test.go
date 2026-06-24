package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/middleware"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

// ==================== InMemoryStore ====================

func TestInMemoryStore_AddAndList(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	if err := store.Add(ctx, "likes coffee", "user1", "agent1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ctx, "prefers dark mode", "user1", "agent1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ctx, "other user data", "user2", "agent1"); err != nil {
		t.Fatal(err)
	}

	list, err := store.List(ctx, "user1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 memories for user1, got %d", len(list))
	}

	list2, _ := store.List(ctx, "user2")
	if len(list2) != 1 {
		t.Errorf("expected 1 memory for user2, got %d", len(list2))
	}
}

func TestInMemoryStore_AddEmpty(t *testing.T) {
	store := NewInMemoryStore()
	if err := store.Add(context.Background(), "", "user1", ""); err != nil {
		t.Fatal(err)
	}
	list, _ := store.List(context.Background(), "user1")
	if len(list) != 0 {
		t.Error("empty text should not be stored")
	}
}

func TestInMemoryStore_Search(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	_ = store.Add(ctx, "The user likes coffee in the morning", "u1", "a1")
	_ = store.Add(ctx, "The user prefers dark mode for coding", "u1", "a1")
	_ = store.Add(ctx, "The user has a pet dog named Max", "u1", "a1")
	_ = store.Add(ctx, "Different user likes tea", "u2", "a1")

	results, err := store.Search(ctx, "coffee", "u1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'coffee', got %d", len(results))
	}
	if !strings.Contains(results[0].Text, "coffee") {
		t.Error("result should contain 'coffee'")
	}
}

func TestInMemoryStore_Search_MultipleWords(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	_ = store.Add(ctx, "likes coffee", "u1", "")
	_ = store.Add(ctx, "has a dog", "u1", "")
	_ = store.Add(ctx, "unrelated fact", "u1", "")

	results, _ := store.Search(ctx, "coffee dog", "u1", nil)
	if len(results) != 2 {
		t.Errorf("expected 2 results matching any word, got %d", len(results))
	}
}

func TestInMemoryStore_Search_AgentScope(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	_ = store.Add(ctx, "from agent1", "u1", "a1")
	_ = store.Add(ctx, "from agent2", "u1", "a2")

	results, _ := store.Search(ctx, "from", "u1", &SearchOptions{AgentID: "a1"})
	if len(results) != 1 {
		t.Errorf("expected 1 result scoped to a1, got %d", len(results))
	}
}

func TestInMemoryStore_Search_TopK(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		_ = store.Add(ctx, "item with keyword", "u1", "")
	}

	results, _ := store.Search(ctx, "keyword", "u1", &SearchOptions{TopK: 3})
	if len(results) != 3 {
		t.Errorf("expected 3 results with TopK=3, got %d", len(results))
	}
}

func TestInMemoryStore_Delete(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	_ = store.Add(ctx, "to delete", "u1", "")
	list, _ := store.List(ctx, "u1")
	if len(list) != 1 {
		t.Fatal("setup failed")
	}

	if err := store.Delete(ctx, list[0].ID); err != nil {
		t.Fatal(err)
	}

	list2, _ := store.List(ctx, "u1")
	if len(list2) != 0 {
		t.Error("memory should be deleted")
	}
}

func TestInMemoryStore_DeleteNotFound(t *testing.T) {
	store := NewInMemoryStore()
	err := store.Delete(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for non-existent id")
	}
}

// ==================== helpers ====================

func respText(t *testing.T, resp *tool.ToolResponse) string {
	t.Helper()
	if len(resp.Content) == 0 {
		return ""
	}
	if tb, ok := resp.Content[0].(message.TextBlock); ok {
		return tb.Text
	}
	t.Fatal("first content block is not TextBlock")
	return ""
}

// ==================== Tools ====================

func TestSearchMemoryTool(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	_ = store.Add(ctx, "user likes golang", "u1", "a1")
	_ = store.Add(ctx, "user prefers vim", "u1", "a1")

	st := newSearchMemoryTool(store, "u1", "a1", true)

	resp, err := st.Execute(ctx, map[string]any{
		"keywords": []any{"golang"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(respText(t, resp), "golang") {
		t.Error("search should return golang memory")
	}
}

func TestSearchMemoryTool_NoKeywords(t *testing.T) {
	store := NewInMemoryStore()
	st := newSearchMemoryTool(store, "u1", "", false)

	resp, _ := st.Execute(context.Background(), map[string]any{})
	if !strings.Contains(respText(t, resp), "no keywords") {
		t.Error("should return no-keywords message")
	}
}

func TestSearchMemoryTool_NoResults(t *testing.T) {
	store := NewInMemoryStore()
	st := newSearchMemoryTool(store, "u1", "", false)

	resp, _ := st.Execute(context.Background(), map[string]any{
		"keywords": []any{"nonexistent"},
	})
	if !strings.Contains(respText(t, resp), "no relevant memories") {
		t.Error("should return no-results message")
	}
}

func TestSearchMemoryTool_Deduplication(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	_ = store.Add(ctx, "user likes both coffee and tea", "u1", "")

	st := newSearchMemoryTool(store, "u1", "", false)
	resp, _ := st.Execute(ctx, map[string]any{
		"keywords": []any{"coffee", "tea"},
	})
	if strings.Count(respText(t, resp), "coffee and tea") != 1 {
		t.Error("duplicate results should be deduped")
	}
}

func TestAddMemoryTool(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	at := newAddMemoryTool(store, "u1", "a1")

	resp, err := at.Execute(ctx, map[string]any{
		"thinking": "user mentioned their preference",
		"content":  []any{"prefers dark mode", "uses vim"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(respText(t, resp), "2") {
		t.Error("should report 2 stored items")
	}

	list, _ := store.List(ctx, "u1")
	if len(list) != 2 {
		t.Errorf("expected 2 stored memories, got %d", len(list))
	}
}

func TestAddMemoryTool_EmptyContent(t *testing.T) {
	store := NewInMemoryStore()
	at := newAddMemoryTool(store, "u1", "")

	resp, _ := at.Execute(context.Background(), map[string]any{
		"thinking": "test",
		"content":  []any{},
	})
	if resp.State != message.ToolResultError {
		t.Error("empty content should return error")
	}
}

func TestAddMemoryTool_MissingContent(t *testing.T) {
	store := NewInMemoryStore()
	at := newAddMemoryTool(store, "u1", "")

	resp, _ := at.Execute(context.Background(), map[string]any{
		"thinking": "test",
	})
	if resp.State != message.ToolResultError {
		t.Error("missing content should return error")
	}
}

func TestNewMemoryTools(t *testing.T) {
	store := NewInMemoryStore()
	tools := NewMemoryTools(store, "u1", "a1", true)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name() != "search_memory" {
		t.Errorf("first tool should be search_memory, got %s", tools[0].Name())
	}
	if tools[1].Name() != "add_memory" {
		t.Errorf("second tool should be add_memory, got %s", tools[1].Name())
	}
}

// ==================== Middleware ====================

func TestNew_RequiresUserID(t *testing.T) {
	_, err := New(Config{Store: NewInMemoryStore()})
	if err == nil {
		t.Error("expected error for missing user_id")
	}
}

func TestNew_RequiresStore(t *testing.T) {
	_, err := New(Config{UserID: "u1"})
	if err == nil {
		t.Error("expected error for missing store")
	}
}

func TestNew_DefaultMode(t *testing.T) {
	mw, _ := New(Config{UserID: "u1", Store: NewInMemoryStore()})
	if mw.cfg.Mode != ModeBoth {
		t.Errorf("expected default mode=both, got %s", mw.cfg.Mode)
	}
}

func TestNew_StaticControl_NoTools(t *testing.T) {
	mw, _ := New(Config{UserID: "u1", Store: NewInMemoryStore(), Mode: ModeStaticControl})
	if len(mw.Tools()) != 0 {
		t.Error("static_control should not expose tools")
	}
}

func TestNew_AgentControl_HasTools(t *testing.T) {
	mw, _ := New(Config{UserID: "u1", Store: NewInMemoryStore(), Mode: ModeAgentControl})
	if len(mw.Tools()) != 2 {
		t.Errorf("agent_control should expose 2 tools, got %d", len(mw.Tools()))
	}
}

func TestNew_Both_HasTools(t *testing.T) {
	mw, _ := New(Config{UserID: "u1", Store: NewInMemoryStore(), Mode: ModeBoth})
	if len(mw.Tools()) != 2 {
		t.Errorf("both mode should expose 2 tools, got %d", len(mw.Tools()))
	}
}

// ==================== OnSystemPrompt ====================

func TestOnSystemPrompt_StaticControl(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	_ = store.Add(ctx, "user likes Go", "u1", "")

	mw, _ := New(Config{
		UserID: "u1",
		Store:  store,
		Mode:   ModeStaticControl,
	})

	// Simulate memories being cached in MiddleContext
	mc := middleware.MiddleContext{}
	ctx = middleware.WithMiddleContext(ctx, mc)
	mc.Set(mw.Key(), mcFieldMemories, []Memory{{ID: "1", Text: "user likes Go"}})

	prompt := mw.OnSystemPrompt(ctx, "agent", "Base prompt.")
	if !strings.Contains(prompt, "user likes Go") {
		t.Error("should inject memories into prompt")
	}
	if strings.Contains(prompt, "search_memory") {
		t.Error("static_control should not add tool instructions")
	}
}

func TestOnSystemPrompt_AgentControl(t *testing.T) {
	mw, _ := New(Config{
		UserID: "u1",
		Store:  NewInMemoryStore(),
		Mode:   ModeAgentControl,
	})

	ctx := middleware.WithMiddleContext(context.Background(), middleware.MiddleContext{})
	prompt := mw.OnSystemPrompt(ctx, "agent", "Base prompt.")

	if !strings.Contains(prompt, "search_memory") {
		t.Error("agent_control should add tool instructions")
	}
	if strings.Contains(prompt, DefaultMemorySectionHeader) {
		t.Error("agent_control should not inject memory section")
	}
}

func TestOnSystemPrompt_Both(t *testing.T) {
	mw, _ := New(Config{
		UserID: "u1",
		Store:  NewInMemoryStore(),
		Mode:   ModeBoth,
	})

	mc := middleware.MiddleContext{}
	ctx := middleware.WithMiddleContext(context.Background(), mc)
	mc.Set(mw.Key(), mcFieldMemories, []Memory{{ID: "1", Text: "user fact"}})

	prompt := mw.OnSystemPrompt(ctx, "agent", "Base prompt.")
	if !strings.Contains(prompt, "user fact") {
		t.Error("both mode should inject memories")
	}
	if !strings.Contains(prompt, "search_memory") {
		t.Error("both mode should add tool instructions")
	}
}

func TestOnSystemPrompt_NoMemories(t *testing.T) {
	mw, _ := New(Config{
		UserID: "u1",
		Store:  NewInMemoryStore(),
		Mode:   ModeStaticControl,
	})

	ctx := middleware.WithMiddleContext(context.Background(), middleware.MiddleContext{})
	prompt := mw.OnSystemPrompt(ctx, "agent", "Base prompt.")
	if prompt != "Base prompt." {
		t.Error("no memories should leave prompt unchanged")
	}
}

// ==================== OnReply ====================

func TestOnReply_AgentControlPassthrough(t *testing.T) {
	mw, _ := New(Config{
		UserID: "u1",
		Store:  NewInMemoryStore(),
		Mode:   ModeAgentControl,
	})

	ctx := middleware.WithMiddleContext(context.Background(), middleware.MiddleContext{})
	called := false
	next := func(ctx context.Context, input middleware.ReplyInput) <-chan event.Event {
		called = true
		ch := make(chan event.Event)
		close(ch)
		return ch
	}

	ch := mw.OnReply(ctx, middleware.ReplyInput{UserInput: "hello"}, next)
	for range ch {
	}
	if !called {
		t.Error("agent_control should pass through to next")
	}
}

func TestOnReply_StaticControl_SearchAndInject(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	_ = store.Add(ctx, "user likes pizza", "u1", "")

	mw, _ := New(Config{
		UserID:             "u1",
		Store:              store,
		Mode:               ModeStaticControl,
		ScopeSearchByAgent: false,
	})

	mc := middleware.MiddleContext{}
	ctx = middleware.WithMiddleContext(ctx, mc)

	next := func(ctx context.Context, input middleware.ReplyInput) <-chan event.Event {
		ch := make(chan event.Event, 2)
		ch <- event.NewTextBlockDeltaEvent("r1", "b1", "I see you like pizza!")
		close(ch)
		return ch
	}

	outCh := mw.OnReply(ctx, middleware.ReplyInput{UserInput: "what do I like"}, next)
	var events []event.Event
	for ev := range outCh {
		events = append(events, ev)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Check memories were cached in MiddleContext
	if v, ok := mc.Get(mw.Key(), mcFieldMemories); ok {
		mems := v.([]Memory)
		if len(mems) == 0 {
			t.Error("should have cached memories")
		}
	} else {
		t.Error("memories should be in MiddleContext")
	}
}

func TestOnReply_StaticControl_WriteBack(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()

	mw, _ := New(Config{
		UserID: "u1",
		Store:  store,
		Mode:   ModeStaticControl,
	})

	mc := middleware.MiddleContext{}
	ctx = middleware.WithMiddleContext(ctx, mc)

	next := func(ctx context.Context, input middleware.ReplyInput) <-chan event.Event {
		ch := make(chan event.Event, 1)
		ch <- event.NewTextBlockDeltaEvent("r1", "b1", "Hello there!")
		close(ch)
		return ch
	}

	outCh := mw.OnReply(ctx, middleware.ReplyInput{UserInput: "greetings"}, next)
	for range outCh {
	}

	// Verify write-back
	list, _ := store.List(ctx, "u1")
	if len(list) != 1 {
		t.Fatalf("expected 1 memory after write-back, got %d", len(list))
	}
	if !strings.Contains(list[0].Text, "greetings") {
		t.Error("write-back should contain user query")
	}
	if !strings.Contains(list[0].Text, "Hello there!") {
		t.Error("write-back should contain assistant response")
	}
}

func TestOnReply_EmptyInput_NoWriteBack(t *testing.T) {
	store := NewInMemoryStore()
	mw, _ := New(Config{
		UserID: "u1",
		Store:  store,
		Mode:   ModeStaticControl,
	})

	ctx := middleware.WithMiddleContext(context.Background(), middleware.MiddleContext{})
	next := func(ctx context.Context, input middleware.ReplyInput) <-chan event.Event {
		ch := make(chan event.Event, 1)
		ch <- event.NewTextBlockDeltaEvent("r1", "b1", "response")
		close(ch)
		return ch
	}

	outCh := mw.OnReply(ctx, middleware.ReplyInput{UserInput: ""}, next)
	for range outCh {
	}

	list, _ := store.List(context.Background(), "u1")
	if len(list) != 0 {
		t.Error("empty input should not trigger write-back")
	}
}

func TestOnReply_EventsForwarded(t *testing.T) {
	mw, _ := New(Config{
		UserID: "u1",
		Store:  NewInMemoryStore(),
		Mode:   ModeStaticControl,
	})

	ctx := middleware.WithMiddleContext(context.Background(), middleware.MiddleContext{})
	next := func(ctx context.Context, input middleware.ReplyInput) <-chan event.Event {
		ch := make(chan event.Event, 3)
		ch <- event.NewReplyStartEvent("s1", "r1", "agent", "assistant")
		ch <- event.NewTextBlockDeltaEvent("r1", "b1", "hello")
		ch <- event.NewReplyEndEvent("s1", "r1")
		close(ch)
		return ch
	}

	outCh := mw.OnReply(ctx, middleware.ReplyInput{UserInput: "hi"}, next)
	var count int
	for range outCh {
		count++
	}
	if count != 3 {
		t.Errorf("expected 3 forwarded events, got %d", count)
	}
}

// ==================== Middleware Key ====================

func TestMiddlewareKey(t *testing.T) {
	mw, _ := New(Config{UserID: "u1", Store: NewInMemoryStore()})
	if mw.Key() != defaultMiddlewareKey {
		t.Errorf("expected key %q, got %q", defaultMiddlewareKey, mw.Key())
	}
}

// ==================== matchesAny ====================

func TestMatchesAny(t *testing.T) {
	if !matchesAny("hello world", []string{"hello"}) {
		t.Error("should match 'hello'")
	}
	if !matchesAny("hello world", []string{"xyz", "world"}) {
		t.Error("should match 'world'")
	}
	if matchesAny("hello world", []string{"xyz", "abc"}) {
		t.Error("should not match")
	}
	if matchesAny("hello world", nil) {
		t.Error("nil words should not match")
	}
}
