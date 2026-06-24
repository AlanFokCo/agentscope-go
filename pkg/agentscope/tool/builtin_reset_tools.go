package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

var resetToolsSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"activate": {
			"type": "array",
			"items": {"type": "string"},
			"description": "Tool group names to activate"
		},
		"deactivate": {
			"type": "array",
			"items": {"type": "string"},
			"description": "Tool group names to deactivate"
		}
	}
}`)

type resetToolsTool struct {
	BaseTool
	toolkit *Toolkit
}

func (t *resetToolsTool) Execute(ctx context.Context, args map[string]any) (*ToolResponse, error) {
	if t.toolkit == nil {
		return NewErrorResponse(fmt.Errorf("no toolkit reference")), nil
	}

	var activated, deactivated []string

	if v, ok := args["activate"]; ok {
		names := toStringSlice(v)
		for _, name := range names {
			t.toolkit.ActivateGroup(name)
			activated = append(activated, name)
		}
	}

	if v, ok := args["deactivate"]; ok {
		names := toStringSlice(v)
		for _, name := range names {
			t.toolkit.DeactivateGroup(name)
			deactivated = append(deactivated, name)
		}
	}

	var parts []string
	if len(activated) > 0 {
		parts = append(parts, fmt.Sprintf("Activated: %s", strings.Join(activated, ", ")))
	}
	if len(deactivated) > 0 {
		parts = append(parts, fmt.Sprintf("Deactivated: %s", strings.Join(deactivated, ", ")))
	}
	if len(parts) == 0 {
		return NewTextResponse("No changes made."), nil
	}
	return NewTextResponse(strings.Join(parts, ". ")), nil
}

func toStringSlice(v any) []string {
	switch arr := v.(type) {
	case []any:
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return arr
	default:
		return nil
	}
}

// ResetToolsTool returns a meta-tool that dynamically activates/deactivates tool groups.
// It needs a reference to the Toolkit it manages.
func ResetToolsTool(tk *Toolkit) Tool {
	return &resetToolsTool{
		BaseTool: BaseTool{
			ToolName:        "ResetTools",
			ToolDescription: "Activate or deactivate tool groups to change available tools. Use when you need different capabilities.",
			ToolSchema:      resetToolsSchema,
		},
		toolkit: tk,
	}
}
