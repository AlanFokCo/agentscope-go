package tool

import (
	"context"
	"fmt"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/exception"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/sandbox"
)

// Orchestrator composes permission checking and tool execution into a pipeline.
// It can be adapted to loop.ToolExecutor via AsToolExecutor.
type Orchestrator struct {
	toolkit            *Toolkit
	permEngine         *permission.Engine
	sandbox            sandbox.Sandbox
	onPermDenied       func(string, permission.Decision)
	defaultToolTimeout time.Duration
	maxToolResultBytes int
}

// OrchestratorConfig holds the configuration for creating an Orchestrator.
type OrchestratorConfig struct {
	Toolkit            *Toolkit
	PermEngine         *permission.Engine
	Sandbox            sandbox.Sandbox
	OnPermissionDenied func(toolName string, decision permission.Decision)

	// DefaultToolTimeout bounds each tool execution. Zero means no timeout (a
	// blocking custom/MCP tool could otherwise hang the whole loop).
	DefaultToolTimeout time.Duration
	// MaxToolResultBytes caps the text size of a tool result. Zero means no cap.
	MaxToolResultBytes int
}

// OrchestratorResult pairs a tool call with its execution result or error.
type OrchestratorResult struct {
	Call     message.ToolCallBlock
	Response *ToolResponse
	Err      error
}

// NewOrchestrator creates an Orchestrator from the given config.
func NewOrchestrator(cfg OrchestratorConfig) *Orchestrator {
	return &Orchestrator{
		toolkit:            cfg.Toolkit,
		permEngine:         cfg.PermEngine,
		sandbox:            cfg.Sandbox,
		onPermDenied:       cfg.OnPermissionDenied,
		defaultToolTimeout: cfg.DefaultToolTimeout,
		maxToolResultBytes: cfg.MaxToolResultBytes,
	}
}

// Execute runs a single tool call through the permission + execution pipeline.
func (o *Orchestrator) Execute(ctx context.Context, call message.ToolCallBlock) (*ToolResponse, error) { //nolint:gocritic // public API
	// 1. Look up tool
	t := o.toolkit.Get(call.Name)
	if t == nil {
		return nil, &exception.ToolNotFoundError{ToolName: call.Name}
	}

	// 2. Parse input
	input, err := call.ParseInput()
	if err != nil {
		return NewErrorResponse(fmt.Errorf("parse input: %w", err)), nil
	}

	// 3. Permission check
	if o.permEngine != nil {
		decision, permErr := o.permEngine.CheckPermission(t, input)
		if permErr != nil {
			return nil, fmt.Errorf("permission check: %w", permErr)
		}

		switch decision.Behavior {
		case permission.BehaviorDeny:
			if o.onPermDenied != nil {
				o.onPermDenied(call.Name, decision)
			}
			return &ToolResponse{
				Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: decision.Message}},
				State:   message.ToolResultError,
			}, nil
		case permission.BehaviorAsk:
			if o.onPermDenied != nil {
				o.onPermDenied(call.Name, decision)
			}
			return &ToolResponse{
				Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: "Permission required: " + decision.Message}},
				State:   message.ToolResultError,
			}, nil
		}
	}

	// 4. Execute via toolkit (handles validation + middleware), bounded by an
	// optional per-tool timeout and result-size cap.
	execCtx := ctx
	if o.defaultToolTimeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, o.defaultToolTimeout)
		defer cancel()
	}
	resp, err := o.toolkit.CallTool(execCtx, call.Name, input)
	if err != nil {
		return resp, err
	}
	return truncateToolResponse(resp, o.maxToolResultBytes), nil
}

// truncateToolResponse caps the total text length of a tool response at max
// bytes (max <= 0 disables the cap), appending a truncation notice.
func truncateToolResponse(resp *ToolResponse, max int) *ToolResponse {
	if resp == nil || max <= 0 {
		return resp
	}
	total := 0
	for i, b := range resp.Content {
		tb, ok := b.(message.TextBlock)
		if !ok {
			continue
		}
		if total+len(tb.Text) <= max {
			total += len(tb.Text)
			continue
		}
		keep := max - total
		if keep < 0 {
			keep = 0
		}
		over := total + len(tb.Text) - max
		tb.Text = tb.Text[:keep] + fmt.Sprintf("\n... [truncated, %d bytes over %d-byte limit]", over, max)
		resp.Content[i] = tb
		resp.Content = resp.Content[:i+1] // drop any blocks past the cap
		break
	}
	return resp
}

// BatchExecute runs each call through Execute sequentially and collects results.
func (o *Orchestrator) BatchExecute(ctx context.Context, calls []message.ToolCallBlock) []*OrchestratorResult {
	results := make([]*OrchestratorResult, len(calls))
	for i, call := range calls {
		resp, err := o.Execute(ctx, call)
		results[i] = &OrchestratorResult{
			Call:     call,
			Response: resp,
			Err:      err,
		}
	}
	return results
}

// toolExecutorAdapter wraps an Orchestrator with method signatures matching
// loop.ToolExecutor. Since the tool package cannot import loop (circular
// dependency), this adapter is structurally compatible but does not reference
// loop types directly. The loop package can assign it via interface assertion.
type toolExecutorAdapter struct {
	o *Orchestrator
}

// Execute delegates to the underlying Orchestrator. The signature matches
// loop.ToolExecutor.Execute.
func (a *toolExecutorAdapter) Execute(ctx context.Context, call message.ToolCallBlock) (*ToolResponse, error) { //nolint:gocritic // interface
	return a.o.Execute(ctx, call)
}

// BatchExecute delegates to the underlying Orchestrator. Returns
// []*OrchestratorResult which has the same fields as loop.ToolResult.
func (a *toolExecutorAdapter) BatchExecute(ctx context.Context, calls []message.ToolCallBlock) []*OrchestratorResult {
	return a.o.BatchExecute(ctx, calls)
}

// AsToolExecutor returns an adapter whose Execute and BatchExecute methods
// match the loop.ToolExecutor interface signatures (modulo return types for
// BatchExecute due to circular import constraints). Use a type assertion in
// the loop package to satisfy the interface.
func (o *Orchestrator) AsToolExecutor() *toolExecutorAdapter {
	return &toolExecutorAdapter{o: o}
}
