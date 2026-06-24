package jsonx

import (
	"testing"
)

func TestRepairAndUnmarshal_ValidJSON(t *testing.T) {
	var v map[string]any
	if err := RepairAndUnmarshal([]byte(`{"key": "value"}`), &v); err != nil {
		t.Fatalf("valid JSON failed: %v", err)
	}
	if v["key"] != "value" {
		t.Errorf("got %v, want value", v["key"])
	}
}

func TestRepairAndUnmarshal_MarkdownFence(t *testing.T) {
	input := "Here is the result:\n```json\n{\"tool\": \"bash\", \"args\": {\"cmd\": \"ls\"}}\n```"
	var v map[string]any
	if err := RepairAndUnmarshal([]byte(input), &v); err != nil {
		t.Fatalf("markdown fence failed: %v", err)
	}
	if v["tool"] != "bash" {
		t.Errorf("got %v, want bash", v["tool"])
	}
}

func TestRepairAndUnmarshal_TrailingComma(t *testing.T) {
	input := `{"a": 1, "b": 2,}`
	var v map[string]any
	if err := RepairAndUnmarshal([]byte(input), &v); err != nil {
		t.Fatalf("trailing comma failed: %v", err)
	}
}

func TestRepairAndUnmarshal_SingleQuotes(t *testing.T) {
	input := `{'name': 'test', 'value': 42}`
	var v map[string]any
	if err := RepairAndUnmarshal([]byte(input), &v); err != nil {
		t.Fatalf("single quotes failed: %v", err)
	}
	if v["name"] != "test" {
		t.Errorf("got %v, want test", v["name"])
	}
}

func TestRepairAndUnmarshal_UnclosedBrackets(t *testing.T) {
	input := `{"key": "value", "nested": {"a": 1}`
	var v map[string]any
	if err := RepairAndUnmarshal([]byte(input), &v); err != nil {
		t.Fatalf("unclosed brackets failed: %v", err)
	}
}

func TestRepairAndUnmarshal_PrefixText(t *testing.T) {
	input := `Sure, here is the JSON output: {"tool": "grep", "args": {"pattern": "foo"}}`
	var v map[string]any
	if err := RepairAndUnmarshal([]byte(input), &v); err != nil {
		t.Fatalf("prefix text failed: %v", err)
	}
	if v["tool"] != "grep" {
		t.Errorf("got %v, want grep", v["tool"])
	}
}

func TestRepairAndUnmarshal_Array(t *testing.T) {
	input := `The result is [1, 2, 3]`
	var v []int
	if err := RepairAndUnmarshal([]byte(input), &v); err != nil {
		t.Fatalf("array extraction failed: %v", err)
	}
	if len(v) != 3 {
		t.Errorf("got %d elements, want 3", len(v))
	}
}

func TestRepairAndUnmarshal_CombinedFixes(t *testing.T) {
	input := "```\n{'key': 'val', 'list': [1, 2,]}\n```"
	var v map[string]any
	if err := RepairAndUnmarshal([]byte(input), &v); err != nil {
		t.Fatalf("combined fixes failed: %v", err)
	}
}

func TestRepairAndUnmarshal_Irreparable(t *testing.T) {
	input := "this is not json at all"
	var v map[string]any
	if err := RepairAndUnmarshal([]byte(input), &v); err == nil {
		t.Fatal("expected error for irreparable input")
	}
}
