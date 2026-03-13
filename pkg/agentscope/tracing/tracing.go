package tracing

import (
	"context"
	"log"
	"time"

	as "github.com/alanfokco/agentscope-go/pkg/agentscope"
)

// Tracer is a minimal tracing interface that can be backed by OpenTelemetry or any other implementation.
type Tracer interface {
	StartSpan(ctx context.Context, name string) (context.Context, func())
}

// NoopTracer is the default implementation that performs no tracing and only keeps the interface wired.
type NoopTracer struct{}

func (NoopTracer) StartSpan(ctx context.Context, _ string) (context.Context, func()) {
	return ctx, func() {}
}

var tracer Tracer = NoopTracer{}

// LoggerTracer is a reference implementation that logs span start/end events to a logger.
type LoggerTracer struct {
	Logger *log.Logger
}

func (l LoggerTracer) StartSpan(ctx context.Context, name string) (context.Context, func()) {
	if l.Logger == nil {
		l.Logger = as.Logger()
	}
	start := time.Now()
	l.Logger.Printf("[trace] start span name=%s at=%s", name, start.Format(time.RFC3339Nano))
	return ctx, func() {
		end := time.Now()
		l.Logger.Printf("[trace] end span name=%s duration=%s", name, end.Sub(start))
	}
}

// SetupTracing installs a custom Tracer, for example one backed by OpenTelemetry.
func SetupTracing(t Tracer) {
	if t == nil {
		tracer = NoopTracer{}
		return
	}
	tracer = t
	cfg := as.ConfigSnapshot()
	cfg.TraceEnabled = true
	as.Logger().Printf("tracing enabled at %s", time.Now().Format(time.RFC3339Nano))
}

// TracerInstance returns the currently installed global Tracer.
func TracerInstance() Tracer {
	return tracer
}

