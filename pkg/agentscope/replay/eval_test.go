package replay

import (
	"context"
	"encoding/json"
	"testing"
)

func TestEvalTape_ExactMatch(t *testing.T) {
	expected := &Tape{
		Version: "1.0",
		Entries: []Entry{
			{Index: 0, Response: json.RawMessage(`{"text":"hello"}`)},
			{Index: 1, Response: json.RawMessage(`{"text":"world"}`)},
		},
	}
	recorded := &Tape{
		Version: "1.0",
		Entries: []Entry{
			{Index: 0, Response: json.RawMessage(`{"text":"hello"}`)},
			{Index: 1, Response: json.RawMessage(`{"text":"world"}`)},
		},
	}

	report, err := EvalTape(context.Background(), expected, recorded, ExactMatchScorer, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed != 2 || report.Failed != 0 {
		t.Errorf("expected 2 passed, got passed=%d failed=%d", report.Passed, report.Failed)
	}
	if report.MeanScore != 1.0 {
		t.Errorf("expected mean 1.0, got %.3f", report.MeanScore)
	}
}

func TestEvalTape_Mismatch(t *testing.T) {
	expected := &Tape{
		Version: "1.0",
		Entries: []Entry{
			{Index: 0, Response: json.RawMessage(`{"text":"hello"}`)},
		},
	}
	recorded := &Tape{
		Version: "1.0",
		Entries: []Entry{
			{Index: 0, Response: json.RawMessage(`{"text":"goodbye"}`)},
		},
	}

	report, err := EvalTape(context.Background(), expected, recorded, ExactMatchScorer, 1.0)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed != 0 || report.Failed != 1 {
		t.Errorf("expected 0 passed 1 failed, got passed=%d failed=%d", report.Passed, report.Failed)
	}
}

func TestEvalTape_LengthMismatch(t *testing.T) {
	expected := &Tape{Version: "1.0", Entries: []Entry{{}, {}}}
	recorded := &Tape{Version: "1.0", Entries: []Entry{{}}}

	_, err := EvalTape(context.Background(), expected, recorded, ExactMatchScorer, 1.0)
	if err == nil {
		t.Fatal("expected error for entry count mismatch")
	}
}

func TestContainsScorer(t *testing.T) {
	s := ContainsScorer("revenue")
	score, err := s.Score(context.Background(), nil, json.RawMessage(`"The revenue grew by 15%."`))
	if err != nil {
		t.Fatal(err)
	}
	if score != 1.0 {
		t.Errorf("expected 1.0, got %.3f", score)
	}

	score, _ = s.Score(context.Background(), nil, json.RawMessage(`"No relevant data."`))
	if score != 0.0 {
		t.Errorf("expected 0.0, got %.3f", score)
	}
}

func TestJSONFieldScorer(t *testing.T) {
	s := JSONFieldScorer("status")
	score, _ := s.Score(context.Background(),
		json.RawMessage(`{"status":"ok","data":1}`),
		json.RawMessage(`{"status":"ok","data":99}`))
	if score != 1.0 {
		t.Errorf("expected 1.0 (field match), got %.3f", score)
	}

	score, _ = s.Score(context.Background(),
		json.RawMessage(`{"status":"ok"}`),
		json.RawMessage(`{"status":"fail"}`))
	if score != 0.0 {
		t.Errorf("expected 0.0 (field mismatch), got %.3f", score)
	}
}

func TestTextContainsScorer(t *testing.T) {
	s := TextContainsScorer("Go", "concurrency")
	score, _ := s.Score(context.Background(), nil, json.RawMessage(`"Go has great concurrency support."`))
	if score != 1.0 {
		t.Errorf("expected 1.0, got %.3f", score)
	}

	score, _ = s.Score(context.Background(), nil, json.RawMessage(`"Python is great for ML."`))
	if score != 0.0 {
		t.Errorf("expected 0.0, got %.3f", score)
	}
}

func TestCompositeScorer(t *testing.T) {
	s := CompositeScorer(ExactMatchScorer, ContainsScorer("hello"))
	score, _ := s.Score(context.Background(),
		json.RawMessage(`"hello world"`),
		json.RawMessage(`"hello world"`))
	if score != 1.0 {
		t.Errorf("expected 1.0, got %.3f", score)
	}

	// Exact mismatch (0) + contains match (1) = 0.5
	score, _ = s.Score(context.Background(),
		json.RawMessage(`"hello world"`),
		json.RawMessage(`"hello earth"`))
	if score != 0.5 {
		t.Errorf("expected 0.5, got %.3f", score)
	}
}

func TestAssertTape_PassesOnMatch(t *testing.T) {
	tape := &Tape{
		Version: "1.0",
		Entries: []Entry{{Response: json.RawMessage(`"ok"`)}},
	}
	// AssertTape should not fail for a perfect match.
	// We can't easily test that it *doesn't* call t.Errorf, but we can
	// at least run it and confirm no panic.
	AssertTape(t, tape, tape, ExactMatchScorer, 1.0)
}
