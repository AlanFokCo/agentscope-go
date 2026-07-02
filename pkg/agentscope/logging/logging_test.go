package logging

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
)

type capturingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r)
	h.mu.Unlock()
	return nil
}

func (h *capturingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }

func (h *capturingHandler) WithGroup(_ string) slog.Handler { return h }

func (h *capturingHandler) snapshot() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

func TestSetDefaultAndDefault(t *testing.T) {
	orig := Default()
	defer SetDefault(orig)

	custom := slog.New(&capturingHandler{})
	SetDefault(custom)

	if Default() != custom {
		t.Fatalf("Default() did not return the logger set via SetDefault")
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"trace":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"":        slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"fatal":   slog.LevelError,
		"panic":   slog.LevelError,
		"unknown": slog.LevelInfo,
	}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestErrAttr(t *testing.T) {
	attr := Err(errors.New("x"))
	if attr.Key != "error" {
		t.Fatalf("Err key = %q, want %q", attr.Key, "error")
	}
}

func TestConvenienceFunctions(t *testing.T) {
	orig := Default()
	defer SetDefault(orig)

	h := &capturingHandler{}
	SetDefault(slog.New(h))

	Debug("d")
	Info("i")
	Warn("w")
	Error("e")

	recs := h.snapshot()
	if len(recs) != 4 {
		t.Fatalf("got %d records, want 4", len(recs))
	}

	want := []struct {
		msg   string
		level slog.Level
	}{
		{"d", slog.LevelDebug},
		{"i", slog.LevelInfo},
		{"w", slog.LevelWarn},
		{"e", slog.LevelError},
	}
	for i, w := range want {
		if recs[i].Message != w.msg || recs[i].Level != w.level {
			t.Errorf("record %d = (%q, %v), want (%q, %v)", i, recs[i].Message, recs[i].Level, w.msg, w.level)
		}
	}
}
