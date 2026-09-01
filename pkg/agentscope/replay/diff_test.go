package replay

import (
	"strings"
	"testing"
)

func TestParseRunLog_RoundTrip(t *testing.T) {
	log := strings.Join([]string{
		`{"type":"reply_input","ts":"2026-09-01T00:00:00Z","data":{"agent":"a"}}`,
		`{"type":"event","ts":"2026-09-01T00:00:01Z","data":{"event_type":"reply_start"}}`,
		`garbage line skipped`,
		`{"type":"model_call","ts":"2026-09-01T00:00:02Z","data":{"model":"m"}}`,
	}, "\n")
	events := ParseRunLog([]byte(log))
	if len(events) != 3 {
		t.Fatalf("parsed %d lines, want 3", len(events))
	}
}

func TestDiffRunLogs_AlignsDivergedStreams(t *testing.T) {
	a := []RunEvent{
		{Type: "event", Data: []byte(`{"event_type":"reply_start"}`)},
		{Type: "event", Data: []byte(`{"event_type":"tool_call_start"}`)},
		{Type: "event", Data: []byte(`{"event_type":"tool_result_start"}`)},
		{Type: "event", Data: []byte(`{"event_type":"reply_end"}`)},
	}
	b := []RunEvent{
		{Type: "event", Data: []byte(`{"event_type":"reply_start"}`)},
		{Type: "event", Data: []byte(`{"event_type":"reply_end"}`)},
	}
	lines := DiffRunLogs(a, b)
	text := FormatRunDiff(lines)
	if !strings.Contains(text, "- tool_call_start") || !strings.Contains(text, "- tool_result_start") {
		t.Errorf("diff missing removed events:\n%s", text)
	}

	identical := DiffRunLogs(a, a)
	if got := FormatRunDiff(identical); got != "runs are structurally identical" {
		t.Errorf("identical runs diffed as: %s", got)
	}
}
