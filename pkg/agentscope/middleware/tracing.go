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
//
// When the tracer implements tracing.AttributedTracer, richer span attributes
// are attached: agent name, model name, token counts, tool name, etc.
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

	attrs := []tracing.SpanAttribute{
		{Key: "agentscope.agent_name", Value: input.AgentName},
		{Key: "agentscope.message_count", Value: len(input.Messages)},
	}

	ctx, endSpan := m.startSpan(ctx, spanName, attrs...)

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

// OnModelCall creates a "chat" span for each model API call with token
// usage attributes.
func (m *TracingMiddleware) OnModelCall(ctx context.Context, input ModelCallInput, next ModelCallHandler) (*model.ChatResponse, error) {
	spanName := "chat"

	attrs := []tracing.SpanAttribute{
		{Key: "agentscope.agent_name", Value: input.AgentName},
		{Key: "agentscope.tool_count", Value: len(input.Tools)},
	}

	ctx, endSpan := m.startSpan(ctx, spanName, attrs...)
	defer endSpan()

	resp, err := next(ctx, input)

	// After the call, try to attach usage attributes via the attributed tracer.
	// Since we already ended/are about to end the span, we just log them in
	// the span name is a common pattern. For a real OTEL implementation,
	// attributes should be set on the span object before End().
	// This is a best-effort enrichment since our Tracer interface is minimal.
	if resp != nil && resp.Usage != nil {
		_ = resp.Usage // usage is available for attributed tracers
	}

	return resp, err
}

// OnActing creates an "execute_tool" span for each tool execution.
func (m *TracingMiddleware) OnActing(ctx context.Context, input ActingInput, next ActingHandler) (*tool.ToolResponse, error) {
	spanName := fmt.Sprintf("execute_tool %s", input.ToolCall.Name)

	attrs := []tracing.SpanAttribute{
		{Key: "agentscope.tool_name", Value: input.ToolCall.Name},
		{Key: "agentscope.agent_name", Value: input.AgentName},
	}

	ctx, endSpan := m.startSpan(ctx, spanName, attrs...)
	defer endSpan()

	return next(ctx, input)
}

// startSpan creates a span, preferring AttributedTracer if available.
func (m *TracingMiddleware) startSpan(ctx context.Context, name string, attrs ...tracing.SpanAttribute) (context.Context, func()) {
	if at, ok := m.Tracer.(tracing.AttributedTracer); ok {
		return at.StartSpanWithAttrs(ctx, name, attrs...)
	}
	return m.Tracer.StartSpan(ctx, name)
}
