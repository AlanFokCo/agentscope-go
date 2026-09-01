package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
)

type fakeStateSaver struct {
	mu     sync.Mutex
	states map[string]*AgentState
}

func newFakeStateSaver() *fakeStateSaver {
	return &fakeStateSaver{states: map[string]*AgentState{}}
}

func (f *fakeStateSaver) SaveState(_ context.Context, sessionID string, state *AgentState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *state
	f.states[sessionID] = &cp
	return nil
}

func (f *fakeStateSaver) LoadState(_ context.Context, sessionID string) (*AgentState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.states[sessionID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	cp := *st
	return &cp, nil
}

func (f *fakeStateSaver) ListSessions(_ context.Context) ([]SessionInfo, error) { return nil, nil }
func (f *fakeStateSaver) DeleteSession(_ context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.states, sessionID)
	return nil
}

func TestCheckpoint_SavedWithSchemaVersionAndResults(t *testing.T) {
	saver := newFakeStateSaver()
	mock := &mockChatModel{responses: []model.ChatResponse{
		{Content: []message.ContentBlock{
			message.ToolCallBlock{Type: "tool_call", ID: "tc_c", Name: "echo_a", Input: `{}`, State: message.ToolCallPending},
		}, IsLast: true},
		{Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "done"}}, IsLast: true},
	}}
	a := NewUnifiedAgent("cp-agent", "helpful", mock,
		WithToolkit(tool.NewToolkit(echoToolFixtureA())),
		WithPermissionContext(permission.NewContext(permission.ModeBypass)),
		WithReactConfig(ReactConfig{MaxIters: 4}),
		WithStateSaver(saver),
	)
	a.state.SessionID = "sess-cp"

	ch, err := a.ReplyStream(context.Background(), "use tool")
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}

	st, err := saver.LoadState(context.Background(), "sess-cp")
	if err != nil {
		t.Fatalf("no checkpoint saved: %v", err)
	}
	if st.SchemaVersion != StateSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", st.SchemaVersion, StateSchemaVersion)
	}
	// The checkpoint after the batch must include the tool result.
	found := false
	for _, m := range st.Context {
		for _, b := range m.GetContentBlocks(message.ContentBlockToolResult) {
			if tr, ok := b.(message.ToolResultBlock); ok && tr.ID == "tc_c" {
				found = true
			}
		}
	}
	if !found {
		t.Error("checkpoint missing the tool result from the executed batch")
	}
}

func TestLoadCheckpoint_RejectsNewerSchema(t *testing.T) {
	saver := newFakeStateSaver()
	saver.states["future"] = &AgentState{SchemaVersion: StateSchemaVersion + 1, SessionID: "future"}
	_, err := LoadCheckpoint(context.Background(), saver, "future")
	if err == nil {
		t.Fatal("newer schema must be rejected")
	}
}

func TestResume_RepromptsAskingCall(t *testing.T) {
	// Simulate a crash while parked on an ASKING call: restore state via
	// WithState, start a new reply, expect the confirmation re-prompt.
	echoExecutions = 0
	saver := newFakeStateSaver()
	mock := &mockChatModel{responses: []model.ChatResponse{
		{Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "resumed fine"}}, IsLast: true},
	}}

	restored := &AgentState{
		SchemaVersion: StateSchemaVersion,
		SessionID:     "sess-resume",
		Context: []*message.Msg{
			message.UserMsg("user", "do the thing"),
			message.AssistantMsg("cp-agent", []message.ContentBlock{
				message.ToolCallBlock{Type: "tool_call", ID: "tc_ask", Name: "echo_a", Input: `{}`, State: message.ToolCallAsking},
			}),
		},
	}

	a := NewUnifiedAgent("cp-agent", "helpful", mock,
		WithToolkit(tool.NewToolkit(echoToolFixtureA())),
		WithPermissionContext(permission.NewContext(permission.ModeDefault)),
		WithReactConfig(ReactConfig{MaxIters: 4}),
		WithState(restored),
		WithStateSaver(saver),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := a.ReplyStream(ctx, "continue")
	if err != nil {
		t.Fatal(err)
	}

	reprompted := false
	finalText := ""
	for evt := range ch {
		if ce, ok := evt.(event.RequireUserConfirmEvent); ok && !reprompted {
			reprompted = true
			a.SubmitUserConfirm(&event.UserConfirmResultEvent{
				ConfirmResults: []event.ConfirmResult{{Confirmed: true, ToolCall: ce.ToolCalls[0]}},
			})
		}
		if de, ok := evt.(event.TextBlockDeltaEvent); ok {
			finalText += de.Delta
		}
	}
	if !reprompted {
		t.Fatal("resume did not re-emit RequireUserConfirmEvent for the asking call")
	}
	if finalText == "" {
		t.Error("resumed reply produced no final text")
	}
	if echoExecutions != 1 {
		t.Errorf("confirmed tool call must execute exactly once after resume, ran %d times", echoExecutions)
	}
}

// echoExecutions records how many times the echo tool actually ran (HARNESS
// review H1: the resume test must prove the confirmed call executes, not
// just that the re-prompt fired).
var echoExecutions int

// echoToolFixtureA is a local echo tool for checkpoint tests.
func echoToolFixtureA() tool.Tool {
	return tool.NewFunctionTool("echo_a", "echo", nil,
		func(_ context.Context, _ map[string]any) (any, error) {
			echoExecutions++
			return "echo-a", nil
		})
}

func TestResume_BatchConfirmationNotLost(t *testing.T) {
	// HARNESS review M2: one ConfirmResults event answering BOTH pending
	// calls must confirm each — the first waiter may not eat the second
	// call's answer.
	echoExecutions = 0
	mock := &mockChatModel{responses: []model.ChatResponse{
		{Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "both done"}}, IsLast: true},
	}}
	restored := &AgentState{
		SchemaVersion: StateSchemaVersion,
		SessionID:     "sess-batch",
		Context: []*message.Msg{
			message.UserMsg("user", "do two things"),
			message.AssistantMsg("cp-agent", []message.ContentBlock{
				message.ToolCallBlock{Type: "tool_call", ID: "tc_1", Name: "echo_a", Input: `{}`, State: message.ToolCallAsking},
				message.ToolCallBlock{Type: "tool_call", ID: "tc_2", Name: "echo_a", Input: `{}`, State: message.ToolCallAsking},
			}),
		},
	}
	a := NewUnifiedAgent("cp-agent", "helpful", mock,
		WithToolkit(tool.NewToolkit(echoToolFixtureA())),
		WithPermissionContext(permission.NewContext(permission.ModeDefault)),
		WithReactConfig(ReactConfig{MaxIters: 4}),
		WithState(restored),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := a.ReplyStream(ctx, "continue")
	if err != nil {
		t.Fatal(err)
	}

	batched := false
	for evt := range ch {
		if ce, ok := evt.(event.RequireUserConfirmEvent); ok && !batched {
			batched = true
			// The consumer knows both calls are pending (from the restored
			// checkpoint state) and answers them with ONE batched event —
			// the reprompt loop is sequential, so the second call's answer
			// must survive via the confirmation stash.
			first := ce.ToolCalls[0]
			second := first
			second.ID = "tc_2"
			a.SubmitUserConfirm(&event.UserConfirmResultEvent{
				ConfirmResults: []event.ConfirmResult{
					{Confirmed: true, ToolCall: first},
					{Confirmed: true, ToolCall: second},
				},
			})
		}
	}
	if echoExecutions != 2 {
		t.Errorf("both confirmed calls must execute, ran %d times", echoExecutions)
	}
}
