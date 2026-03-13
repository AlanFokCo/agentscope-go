package tracing

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// OTELTracer is a Tracer implementation backed by OpenTelemetry's trace.Tracer.
// Users are expected to configure the OTEL TracerProvider and pass a trace.Tracer
// instance into this struct.
type OTELTracer struct {
	Tracer trace.Tracer
}

func (o OTELTracer) StartSpan(ctx context.Context, name string) (context.Context, func()) {
	if o.Tracer == nil {
		// Fallback to the global noop tracer if none provided.
		return ctx, func() {}
	}
	ctx, span := o.Tracer.Start(ctx, name)
	return ctx, func() {
		span.End()
	}
}
