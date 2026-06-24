package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

// --- HITL mock tool ---

// hitlMockTool is a simple tool for HITL testing that returns a configurable result.
type hitlMockTool struct {
	tool.BaseTool
	result   string
	external bool
}

func newHITLMockTool(name string, external bool) *hitlMockTool {
	return &hitlMockTool{
		BaseTool: tool.BaseTool{
			ToolName:        name,
			ToolDescription: "Test tool for HITL",
			ToolSchema:      json.RawMessage(`{"type":"object","properties":{}}`),
		},
		result:   "tool executed successfully",
		external: external,
	}
}

func (t *hitlMockTool) Execute(ctx context.Context, input map[string]any) (*tool.ToolResponse, error) {
	return tool.NewTextResponse(t.result), nil
}

func (t *hitlMockTool) IsExternalTool() bool { return t.external }

// --- Tests ---

func TestHITL_RequireUserConfirmEvent_EmittedOnAsk(t *testing.T) {
	// When permission mode is Default and no allow rules match, the engine
	// returns ASK. The agent should emit a RequireUserConfirmEvent.
	toolCallResp := model.ChatResponse{
		Content: []message.ContentBlock{
			message.ToolCallBlock{
				Type:  "tool_call",
				ID:    "tc_1",
				Name:  "test_tool",
				Input: `{}`,
				State: message.ToolCallPending,
			},
		},
		IsLast: true,
	}
	finalResp := model.ChatResponse{
		Content: []message.ContentBlock{
			message.TextBlock{Type: "text", ID: "t1", Text: "Done"},
		},
		IsLast: true,
	}

	mock := &mockChatModel{
		responses: []model.ChatResponse{toolCallResp, finalResp},
	}

	tk := tool.NewToolkit(newHITLMockTool("test_tool", false))
	permCtx := permission.NewContext(permission.ModeDefault)

	agent := NewUnifiedAgent("hitl-agent", "You are helpful.", mock,
		WithToolkit(tk),
		WithPermissionContext(permCtx),
		WithReactConfig(ReactConfig{MaxIters: 3}),
	)

	ch, err := agent.ReplyStream(context.Background(), "Do something")
	if err != nil {
		t.Fatal(err)
	}

	// In a goroutine, listen for RequireUserConfirmEvent and confirm it
	go func() {
		for evt := range ch {
			if _, ok := evt.(event.RequireUserConfirmEvent); ok {
				agent.SubmitUserConfirm(event.UserConfirmResultEvent{
					ConfirmResults: []event.ConfirmResult{
						{
							Confirmed: true,
							ToolCall: message.ToolCallBlock{
								Type:  "tool_call",
								ID:    "tc_1",
								Name:  "test_tool",
								Input: `{}`,
								State: message.ToolCallAllowed,
							},
						},
					},
				})
				return
			}
		}
	}()

	// Wait for the stream to finish by collecting events
	// We already started consuming in the goroutine, but let's make sure it completes
	time.Sleep(500 * time.Millisecond)

	// Verify the agent completed
	if mock.callCount < 1 {
		t.Error("expected at least 1 model call")
	}
}

func TestHITL_SubmitUserConfirm_ResumesExecution(t *testing.T) {
	// Tool call that triggers permission ASK, then user confirms.
	// The tool should execute after confirmation.
	toolCallResp := model.ChatResponse{
		Content: []message.ContentBlock{
			message.ToolCallBlock{
				Type:  "tool_call",
				ID:    "tc_confirm",
				Name:  "test_tool",
				Input: `{}`,
				State: message.ToolCallPending,
			},
		},
		IsLast: true,
	}
	finalResp := model.ChatResponse{
		Content: []message.ContentBlock{
			message.TextBlock{Type: "text", ID: "t1", Text: "Completed"},
		},
		IsLast: true,
	}

	mock := &mockChatModel{
		responses: []model.ChatResponse{toolCallResp, finalResp},
	}

	tk := tool.NewToolkit(newHITLMockTool("test_tool", false))
	permCtx := permission.NewContext(permission.ModeDefault)

	agent := NewUnifiedAgent("hitl-agent", "You are helpful.", mock,
		WithToolkit(tk),
		WithPermissionContext(permCtx),
		WithReactConfig(ReactConfig{MaxIters: 3}),
	)

	ch, err := agent.ReplyStream(context.Background(), "Run the tool")
	if err != nil {
		t.Fatal(err)
	}

	var gotConfirmEvent bool
	var events []event.Event

	// Collect events in background, confirm when asked
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			events = append(events, evt)
			if confirmEvt, ok := evt.(event.RequireUserConfirmEvent); ok {
				gotConfirmEvent = true
				// Simulate user confirming
				agent.SubmitUserConfirm(event.UserConfirmResultEvent{
					ConfirmResults: []event.ConfirmResult{
						{
							Confirmed: true,
							ToolCall:  confirmEvt.ToolCalls[0],
						},
					},
				})
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for reply to complete")
	}

	if !gotConfirmEvent {
		t.Error("expected RequireUserConfirmEvent to be emitted")
	}

	// Verify tool result was recorded in context
	foundToolResult := false
	for _, m := range agent.state.Context {
		for _, b := range m.GetContentBlocks(message.ContentBlockToolResult) {
			if tr, ok := b.(message.ToolResultBlock); ok && tr.Name == "test_tool" {
				foundToolResult = true
				if tr.State == message.ToolResultDenied {
					t.Error("tool result should not be denied after confirmation")
				}
			}
		}
	}
	if !foundToolResult {
		t.Error("expected tool result to be recorded in context after confirmation")
	}
}

func TestHITL_ToolRejection_ProducesDenied(t *testing.T) {
	// When user denies (Confirmed=false), the tool result should be ToolResultDenied.
	toolCallResp := model.ChatResponse{
		Content: []message.ContentBlock{
			message.ToolCallBlock{
				Type:  "tool_call",
				ID:    "tc_deny",
				Name:  "test_tool",
				Input: `{}`,
				State: message.ToolCallPending,
			},
		},
		IsLast: true,
	}
	finalResp := model.ChatResponse{
		Content: []message.ContentBlock{
			message.TextBlock{Type: "text", ID: "t1", Text: "OK"},
		},
		IsLast: true,
	}

	mock := &mockChatModel{
		responses: []model.ChatResponse{toolCallResp, finalResp},
	}

	tk := tool.NewToolkit(newHITLMockTool("test_tool", false))
	permCtx := permission.NewContext(permission.ModeDefault)

	agent := NewUnifiedAgent("hitl-agent", "You are helpful.", mock,
		WithToolkit(tk),
		WithPermissionContext(permCtx),
		WithReactConfig(ReactConfig{MaxIters: 3}),
	)

	ch, err := agent.ReplyStream(context.Background(), "Run the tool")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			if _, ok := evt.(event.RequireUserConfirmEvent); ok {
				// Reject the tool call
				agent.SubmitUserConfirm(event.UserConfirmResultEvent{
					ConfirmResults: []event.ConfirmResult{
						{
							Confirmed: false,
							ToolCall: message.ToolCallBlock{
								Type:  "tool_call",
								ID:    "tc_deny",
								Name:  "test_tool",
								Input: `{}`,
								State: message.ToolCallPending,
							},
						},
					},
				})
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for reply to complete")
	}

	// Verify tool result state is Denied
	foundDenied := false
	for _, m := range agent.state.Context {
		for _, b := range m.GetContentBlocks(message.ContentBlockToolResult) {
			if tr, ok := b.(message.ToolResultBlock); ok && tr.Name == "test_tool" {
				if tr.State == message.ToolResultDenied {
					foundDenied = true
				}
			}
		}
	}
	if !foundDenied {
		t.Error("expected ToolResultDenied state when user rejects")
	}
}

func TestHITL_ExternalTool_EmitsRequireExternalExecution(t *testing.T) {
	// When a tool has IsExternalTool() == true, the agent should emit
	// RequireExternalExecutionEvent and wait for SubmitExternalResult.
	toolCallResp := model.ChatResponse{
		Content: []message.ContentBlock{
			message.ToolCallBlock{
				Type:  "tool_call",
				ID:    "tc_ext",
				Name:  "external_tool",
				Input: `{}`,
				State: message.ToolCallPending,
			},
		},
		IsLast: true,
	}
	finalResp := model.ChatResponse{
		Content: []message.ContentBlock{
			message.TextBlock{Type: "text", ID: "t1", Text: "External done"},
		},
		IsLast: true,
	}

	mock := &mockChatModel{
		responses: []model.ChatResponse{toolCallResp, finalResp},
	}

	// Use bypass mode so permission check doesn't block
	tk := tool.NewToolkit(newHITLMockTool("external_tool", true))
	permCtx := permission.NewContext(permission.ModeBypass)

	agent := NewUnifiedAgent("hitl-agent", "You are helpful.", mock,
		WithToolkit(tk),
		WithPermissionContext(permCtx),
		WithReactConfig(ReactConfig{MaxIters: 3}),
	)

	ch, err := agent.ReplyStream(context.Background(), "Call external")
	if err != nil {
		t.Fatal(err)
	}

	var gotExternalEvent bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			if _, ok := evt.(event.RequireExternalExecutionEvent); ok {
				gotExternalEvent = true
				// Submit external result
				agent.SubmitExternalResult(event.ExternalExecutionResultEvent{
					ExecutionResults: []message.ToolResultBlock{
						{
							Type:   "tool_result",
							ID:     "tc_ext",
							Name:   "external_tool",
							Output: "external result data",
							State:  message.ToolResultSuccess,
						},
					},
				})
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for reply to complete")
	}

	if !gotExternalEvent {
		t.Error("expected RequireExternalExecutionEvent to be emitted for external tool")
	}
}

func TestHITL_SubmitExternalResult_ProvidesResultBack(t *testing.T) {
	// The external tool result text should appear in the agent's context.
	toolCallResp := model.ChatResponse{
		Content: []message.ContentBlock{
			message.ToolCallBlock{
				Type:  "tool_call",
				ID:    "tc_ext2",
				Name:  "ext_tool",
				Input: `{}`,
				State: message.ToolCallPending,
			},
		},
		IsLast: true,
	}
	finalResp := model.ChatResponse{
		Content: []message.ContentBlock{
			message.TextBlock{Type: "text", ID: "t1", Text: "Got it"},
		},
		IsLast: true,
	}

	mock := &mockChatModel{
		responses: []model.ChatResponse{toolCallResp, finalResp},
	}

	tk := tool.NewToolkit(newHITLMockTool("ext_tool", true))
	permCtx := permission.NewContext(permission.ModeBypass)

	agent := NewUnifiedAgent("hitl-agent", "You are helpful.", mock,
		WithToolkit(tk),
		WithPermissionContext(permCtx),
		WithReactConfig(ReactConfig{MaxIters: 3}),
	)

	ch, err := agent.ReplyStream(context.Background(), "Call external tool")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range ch {
			if _, ok := evt.(event.RequireExternalExecutionEvent); ok {
				agent.SubmitExternalResult(event.ExternalExecutionResultEvent{
					ExecutionResults: []message.ToolResultBlock{
						{
							Type:   "tool_result",
							ID:     "tc_ext2",
							Name:   "ext_tool",
							Output: "external payload from remote system",
							State:  message.ToolResultSuccess,
						},
					},
				})
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for reply to complete")
	}

	// Verify the external result text ended up in context
	found := false
	for _, m := range agent.state.Context {
		for _, b := range m.GetContentBlocks(message.ContentBlockToolResult) {
			if tr, ok := b.(message.ToolResultBlock); ok && tr.Name == "ext_tool" {
				if output, ok := tr.Output.(string); ok && output == "external payload from remote system" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("expected external result text to appear in agent context")
	}
}

// --- Observe validation tests ---

func TestObserve_RejectsSystemRoleMessages(t *testing.T) {
	mock := &mockChatModel{}
	agent := NewUnifiedAgent("test", "prompt", mock)

	err := agent.Observe(context.Background(), []*message.Msg{
		message.SystemMsg("sys", "you are a system"),
	})
	if err == nil {
		t.Fatal("expected error for system-role message")
	}
	if !containsStr(err.Error(), "system-role") {
		t.Errorf("error should mention system-role, got: %s", err.Error())
	}
}

func TestObserve_RejectsToolCallBlocks(t *testing.T) {
	mock := &mockChatModel{}
	agent := NewUnifiedAgent("test", "prompt", mock)

	msg := message.NewMsg("bot", message.RoleAssistant, []message.ContentBlock{
		message.ToolCallBlock{
			Type:  "tool_call",
			ID:    "tc_1",
			Name:  "some_tool",
			Input: `{"a":1}`,
			State: message.ToolCallPending,
		},
	})

	err := agent.Observe(context.Background(), []*message.Msg{msg})
	if err == nil {
		t.Fatal("expected error for message with tool_call blocks")
	}
	if !containsStr(err.Error(), "tool_call") {
		t.Errorf("error should mention tool_call, got: %s", err.Error())
	}
}

func TestObserve_RejectsThinkingBlocks(t *testing.T) {
	mock := &mockChatModel{}
	agent := NewUnifiedAgent("test", "prompt", mock)

	msg := message.NewMsg("bot", message.RoleAssistant, []message.ContentBlock{
		message.ThinkingBlock{
			Type:     "thinking",
			ID:       "th_1",
			Thinking: "I am thinking...",
		},
	})

	err := agent.Observe(context.Background(), []*message.Msg{msg})
	if err == nil {
		t.Fatal("expected error for message with thinking blocks")
	}
	if !containsStr(err.Error(), "thinking") {
		t.Errorf("error should mention thinking, got: %s", err.Error())
	}
}

func TestObserve_AcceptsValidUserMessage(t *testing.T) {
	mock := &mockChatModel{}
	agent := NewUnifiedAgent("test", "prompt", mock)

	err := agent.Observe(context.Background(), []*message.Msg{
		message.UserMsg("user", "Hello, this is a valid user message"),
	})
	if err != nil {
		t.Fatalf("expected no error for valid user message, got: %v", err)
	}

	if len(agent.state.Context) != 1 {
		t.Errorf("expected 1 message in context, got %d", len(agent.state.Context))
	}
}

func TestObserve_AcceptsValidAssistantMessage(t *testing.T) {
	mock := &mockChatModel{}
	agent := NewUnifiedAgent("test", "prompt", mock)

	msg := message.AssistantMsg("bot", "I am a helpful assistant response")

	err := agent.Observe(context.Background(), []*message.Msg{msg})
	if err != nil {
		t.Fatalf("expected no error for valid assistant message, got: %v", err)
	}

	if len(agent.state.Context) != 1 {
		t.Errorf("expected 1 message in context, got %d", len(agent.state.Context))
	}
}

func TestObserve_AcceptsMultipleValidMessages(t *testing.T) {
	mock := &mockChatModel{}
	agent := NewUnifiedAgent("test", "prompt", mock)

	err := agent.Observe(context.Background(), []*message.Msg{
		message.UserMsg("user", "First message"),
		message.AssistantMsg("bot", "First reply"),
		message.UserMsg("user", "Second message"),
	})
	if err != nil {
		t.Fatalf("expected no error for valid messages, got: %v", err)
	}

	if len(agent.state.Context) != 3 {
		t.Errorf("expected 3 messages in context, got %d", len(agent.state.Context))
	}
}

func TestObserve_SkipsNilMessages(t *testing.T) {
	mock := &mockChatModel{}
	agent := NewUnifiedAgent("test", "prompt", mock)

	err := agent.Observe(context.Background(), []*message.Msg{
		nil,
		message.UserMsg("user", "valid message"),
		nil,
	})
	if err != nil {
		t.Fatalf("expected no error when nil messages are present, got: %v", err)
	}

	if len(agent.state.Context) != 1 {
		t.Errorf("expected 1 non-nil message in context, got %d", len(agent.state.Context))
	}
}

func TestObserve_RejectsToolResultBlocks(t *testing.T) {
	mock := &mockChatModel{}
	agent := NewUnifiedAgent("test", "prompt", mock)

	msg := message.NewMsg("bot", message.RoleAssistant, []message.ContentBlock{
		message.ToolResultBlock{
			Type:   "tool_result",
			ID:     "tr_1",
			Name:   "some_tool",
			Output: "result",
			State:  message.ToolResultSuccess,
		},
	})

	err := agent.Observe(context.Background(), []*message.Msg{msg})
	if err == nil {
		t.Fatal("expected error for message with tool_result blocks")
	}
	if !containsStr(err.Error(), "tool_result") {
		t.Errorf("error should mention tool_result, got: %s", err.Error())
	}
}
