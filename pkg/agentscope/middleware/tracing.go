package middleware

import (
	"context"
	"fmt"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tracing"
)

// TracingMiddleware creates nested spans for agent lifecycle events using a
// tracing.Tracer. It wraps OnReply (invoke_agent span), OnModelCall (chat
// span), and OnActing (execute_tool span).
type TracingMiddleware struct {
	BaseMiddleware
	Tracer tracing.Tracer
}

// NewTracingMiddleware creates a tracing middleware. If tracer is nil, it
// uses the globally installed tracer from tracing.TracerInstance().
func NewTracingMiddleware(tracer tracing.Tracer) *TracingMiddleware {
	if tracer == nil {
		tracer = tracing.TracerInstance()
	}
	return &TracingMiddleware{
		BaseMiddleware: BaseMiddleware{MiddlewareKey: "tracing"},
		Tracer:         tracer,
	}
}

// OnReply creates an "invoke_agent" span that wraps the entire reply lifecycle.
func (m *TracingMiddleware) OnReply(ctx context.Context, input ReplyInput, next ReplyHandler) <-chan event.Event {
	spanName := fmt.Sprintf("invoke_agent %s", input.AgentName)
	ctx, endSpan := m.Tracer.StartSpan(ctx, spanName)

	innerCh := next(ctx, input)
	outCh := make(chan event.Event, 16)

	go func() {
		defer close(outCh)
		defer endSpan()
		for ev := range innerCh {
			outCh <- ev
		}
	}()

	return outCh
}

// OnModelCall creates a "chat" span for each model API call.
func (m *TracingMiddleware) OnModelCall(ctx context.Context, input ModelCallInput, next ModelCallHandler) (*model.ChatResponse, error) {
	spanName := "chat"
	ctx, endSpan := m.Tracer.StartSpan(ctx, spanName)
	defer endSpan()

	return next(ctx, input)
}

// OnActing creates an "execute_tool" span for each tool execution.
func (m *TracingMiddleware) OnActing(ctx context.Context, input ActingInput, next ActingHandler) (*tool.ToolResponse, error) {
	spanName := fmt.Sprintf("execute_tool %s", input.ToolCall.Name)
	ctx, endSpan := m.Tracer.StartSpan(ctx, spanName)
	defer endSpan()

	return next(ctx, input)
}
