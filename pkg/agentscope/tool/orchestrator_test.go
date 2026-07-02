package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/exception"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
)

func makeEchoTool(name string) *FunctionTool {
	return NewFunctionTool(name, "echoes input", json.RawMessage(`{"type":"object","properties":{}}`),
		func(ctx context.Context, input map[string]any) (any, error) {
			return "ok:" + name, nil
		},
	)
}

func makeToolCall(name, inputJSON string) message.ToolCallBlock {
	return message.ToolCallBlock{
		Type:  "tool_call",
		ID:    "call-" + name,
		Name:  name,
		Input: inputJSON,
	}
}

func TestOrchestratorExecuteSuccess(t *testing.T) {
	tk := NewToolkit(makeEchoTool("greet"))
	o := NewOrchestrator(OrchestratorConfig{Toolkit: tk})

	resp, err := o.Execute(context.Background(), makeToolCall("greet", `{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.State != message.ToolResultSuccess {
		t.Fatalf("expected success state, got %v", resp.State)
	}
	if len(resp.Content) == 0 {
		t.Fatal("expected non-empty content")
	}
	tb, ok := resp.Content[0].(message.TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock, got %T", resp.Content[0])
	}
	if tb.Text != "ok:greet" {
		t.Fatalf("expected 'ok:greet', got %q", tb.Text)
	}
}

func TestOrchestratorExecutePermissionDenied(t *testing.T) {
	tk := NewToolkit(makeEchoTool("Bash"))
	permCtx := permission.NewContext(permission.ModeDefault)
	engine := permission.NewEngine(permCtx)
	engine.AddRule(permission.Rule{
		ToolName: "Bash",
		Behavior: permission.BehaviorDeny,
		Source:   "test",
	})

	var deniedTool string
	o := NewOrchestrator(OrchestratorConfig{
		Toolkit:    tk,
		PermEngine: engine,
		OnPermissionDenied: func(name string, d permission.Decision) {
			deniedTool = name
		},
	})

	resp, err := o.Execute(context.Background(), makeToolCall("Bash", `{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.State != message.ToolResultError {
		t.Fatalf("expected error state, got %v", resp.State)
	}
	if deniedTool != "Bash" {
		t.Fatalf("expected OnPermissionDenied called with 'Bash', got %q", deniedTool)
	}
}

func TestOrchestratorExecutePermissionAsk(t *testing.T) {
	tk := NewToolkit(makeEchoTool("Write"))
	permCtx := permission.NewContext(permission.ModeDefault)
	engine := permission.NewEngine(permCtx)
	// In ModeDefault with no allow rules, the engine returns BehaviorAsk.
	// No explicit ask rule needed — the default behavior is Ask.

	var deniedTool string
	o := NewOrchestrator(OrchestratorConfig{
		Toolkit:    tk,
		PermEngine: engine,
		OnPermissionDenied: func(name string, d permission.Decision) {
			deniedTool = name
		},
	})

	resp, err := o.Execute(context.Background(), makeToolCall("Write", `{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.State != message.ToolResultError {
		t.Fatalf("expected error state, got %v", resp.State)
	}
	tb, ok := resp.Content[0].(message.TextBlock)
	if !ok {
		t.Fatalf("expected TextBlock, got %T", resp.Content[0])
	}
	if len(tb.Text) < len("Permission required: ") {
		t.Fatalf("expected 'Permission required: ...' prefix, got %q", tb.Text)
	}
	if tb.Text[:len("Permission required: ")] != "Permission required: " {
		t.Fatalf("expected 'Permission required: ...' prefix, got %q", tb.Text)
	}
	if deniedTool != "Write" {
		t.Fatalf("expected OnPermissionDenied called with 'Write', got %q", deniedTool)
	}
}

func TestOrchestratorExecuteToolNotFound(t *testing.T) {
	tk := NewToolkit() // empty toolkit
	o := NewOrchestrator(OrchestratorConfig{Toolkit: tk})

	_, err := o.Execute(context.Background(), makeToolCall("nonexistent", `{}`))
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
	if _, ok := err.(*exception.ToolNotFoundError); !ok {
		t.Fatalf("expected ToolNotFoundError, got %T: %v", err, err)
	}
}

func TestOrchestratorBatchExecute(t *testing.T) {
	tk := NewToolkit(makeEchoTool("allowed"), makeEchoTool("denied"))
	permCtx := permission.NewContext(permission.ModeDefault)
	engine := permission.NewEngine(permCtx)
	engine.AddRule(permission.Rule{
		ToolName: "allowed",
		Behavior: permission.BehaviorAllow,
		Source:   "test",
	})
	engine.AddRule(permission.Rule{
		ToolName: "denied",
		Behavior: permission.BehaviorDeny,
		Source:   "test",
	})

	o := NewOrchestrator(OrchestratorConfig{
		Toolkit:    tk,
		PermEngine: engine,
	})

	calls := []message.ToolCallBlock{
		makeToolCall("allowed", `{}`),
		makeToolCall("denied", `{}`),
	}

	results := o.BatchExecute(context.Background(), calls)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First call should succeed
	if results[0].Err != nil {
		t.Fatalf("expected no error for 'allowed', got %v", results[0].Err)
	}
	if results[0].Response.State != message.ToolResultSuccess {
		t.Fatalf("expected success for 'allowed', got %v", results[0].Response.State)
	}

	// Second call should be denied
	if results[1].Err != nil {
		t.Fatalf("expected no error (denial is a response, not an error), got %v", results[1].Err)
	}
	if results[1].Response.State != message.ToolResultError {
		t.Fatalf("expected error state for 'denied', got %v", results[1].Response.State)
	}
}

func TestOrchestratorAsToolExecutor(t *testing.T) {
	// Define a local interface mirroring loop.ToolExecutor.Execute signature
	// to verify structural compatibility without importing loop (which would
	// create a circular dependency).
	type executor interface {
		Execute(ctx context.Context, call message.ToolCallBlock) (*ToolResponse, error)
	}

	tk := NewToolkit(makeEchoTool("ping"))
	o := NewOrchestrator(OrchestratorConfig{Toolkit: tk})

	adapter := o.AsToolExecutor()

	// Compile-time check: adapter satisfies the executor interface.
	var _ executor = adapter

	// Runtime check: Execute works through the adapter.
	resp, err := adapter.Execute(context.Background(), makeToolCall("ping", `{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.State != message.ToolResultSuccess {
		t.Fatalf("expected success, got %v", resp.State)
	}

	// Verify BatchExecute also works through the adapter.
	results := adapter.BatchExecute(context.Background(), []message.ToolCallBlock{
		makeToolCall("ping", `{}`),
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("unexpected error: %v", results[0].Err)
	}
}
