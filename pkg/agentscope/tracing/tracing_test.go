package tracing

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
)

func TestNoopTracer(t *testing.T) {
	tr := NoopTracer{}
	ctx, end := tr.StartSpan(context.Background(), "test_span")
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	end() // should not panic
}

func TestLoggerTracer(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	tr := LoggerTracer{Logger: logger}

	_, end := tr.StartSpan(context.Background(), "my_span")
	end()

	output := buf.String()
	if !strings.Contains(output, "start span name=my_span") {
		t.Errorf("expected start span log, got: %s", output)
	}
	if !strings.Contains(output, "end span name=my_span") {
		t.Errorf("expected end span log, got: %s", output)
	}
	if !strings.Contains(output, "duration=") {
		t.Errorf("expected duration in end span log, got: %s", output)
	}
}

func TestSetupTracing_Nil(t *testing.T) {
	SetupTracing(nil)
	if _, ok := TracerInstance().(NoopTracer); !ok {
		t.Error("expected NoopTracer after SetupTracing(nil)")
	}
}

func TestSetupTracing_Custom(t *testing.T) {
	custom := &LoggerTracer{Logger: log.New(&bytes.Buffer{}, "", 0)}
	SetupTracing(custom)
	if TracerInstance() != custom {
		t.Error("expected custom tracer to be installed")
	}
	// Reset
	SetupTracing(nil)
}

func TestAttributedTracer_Interface(t *testing.T) {
	// Verify that types implementing AttributedTracer also implement Tracer
	var _ Tracer = NoopTracer{}

	// SpanAttribute construction
	attr := SpanAttribute{Key: "model", Value: "gpt-4"}
	if attr.Key != "model" {
		t.Error("unexpected key")
	}
	if attr.Value != "gpt-4" {
		t.Error("unexpected value")
	}
}
