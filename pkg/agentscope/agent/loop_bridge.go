package agent

import (
	"context"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/loop"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

// UnifiedAgentRunner adapts a UnifiedAgent for use with runtime.SessionEngine.
// It creates loop.Option values that wire the agent's model and toolkit into
// a loop.Loop configuration.
type UnifiedAgentRunner struct {
	agent     *UnifiedAgent
	loopHooks []loop.Hook
}

// NewUnifiedAgentRunner creates a runner that bridges a UnifiedAgent to loop.Loop.
func NewUnifiedAgentRunner(agent *UnifiedAgent, hooks ...loop.Hook) *UnifiedAgentRunner {
	return &UnifiedAgentRunner{
		agent:     agent,
		loopHooks: hooks,
	}
}

// LoopOptions returns loop.Option values for configuring a loop.Loop that
// delegates model calls and tool execution to the underlying UnifiedAgent.
func (r *UnifiedAgentRunner) LoopOptions() []loop.Option {
	opts := []loop.Option{
		loop.WithModelCaller(&modelCallerAdapter{agent: r.agent}),
		loop.WithToolExecutor(&toolExecutorAdapter{agent: r.agent}),
		loop.WithSchemaProvider(r.agent.toolkit),
		loop.WithMaxIters(r.agent.reactCfg.MaxIters),
		loop.WithSystemPrompt(r.agent.systemPrompt),
	}
	if len(r.loopHooks) > 0 {
		opts = append(opts, loop.WithHooks(r.loopHooks...))
	}
	return opts
}

// modelCallerAdapter implements loop.ModelCaller by delegating to the
// agent's callModel method, which handles retries and fallback.
type modelCallerAdapter struct {
	agent *UnifiedAgent
}

func (m *modelCallerAdapter) Call(ctx context.Context, msgs []*message.Msg, tools []model.ToolSchema) (*model.ChatResponse, error) {
	var opts []model.CallOption
	if len(tools) > 0 {
		opts = append(opts, model.WithTools(tools))
	}
	return m.agent.callModel(ctx, msgs, opts)
}

// toolExecutorAdapter implements loop.ToolExecutor by delegating to the
// agent's toolkit. This is a simplified path without permission checks or
// HITL — those features stay in UnifiedAgent.ReplyStream for the full
// experience.
type toolExecutorAdapter struct {
	agent *UnifiedAgent
}

func (t *toolExecutorAdapter) Execute(ctx context.Context, call message.ToolCallBlock) (*tool.ToolResponse, error) { //nolint:gocritic // interface
	return t.agent.toolkit.CallToolFromBlock(ctx, &call)
}

func (t *toolExecutorAdapter) BatchExecute(ctx context.Context, calls []message.ToolCallBlock) []*loop.ToolResult {
	var results []*loop.ToolResult
	for _, c := range calls {
		resp, err := t.Execute(ctx, c)
		results = append(results, &loop.ToolResult{Call: c, Response: resp, Err: err})
	}
	return results
}
