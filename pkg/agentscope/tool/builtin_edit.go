package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
)

var editSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"file_path": {
			"type": "string",
			"description": "Path to the file to edit"
		},
		"old_string": {
			"type": "string",
			"description": "The exact text to find and replace"
		},
		"new_string": {
			"type": "string",
			"description": "The replacement text"
		},
		"replace_all": {
			"type": "boolean",
			"description": "If true, replace all occurrences; otherwise replace only the first unique match (default false)"
		}
	},
	"required": ["file_path", "old_string", "new_string"]
}`)

type editTool struct {
	BaseTool
}

func (t *editTool) Execute(ctx context.Context, args map[string]any) (*ToolResponse, error) {
	raw, ok := args["file_path"]
	if !ok {
		return NewErrorResponse(fmt.Errorf("file_path is required")), nil
	}
	path, ok := raw.(string)
	if !ok {
		return NewErrorResponse(fmt.Errorf("file_path must be a string")), nil
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return NewErrorResponse(fmt.Errorf("file_path cannot be empty")), nil
	}

	oldStr, ok := args["old_string"].(string)
	if !ok {
		return NewErrorResponse(fmt.Errorf("old_string is required and must be a string")), nil
	}
	newStr, _ := args["new_string"].(string)

	if oldStr == newStr {
		return NewErrorResponse(fmt.Errorf("old_string and new_string are identical")), nil
	}

	replaceAll := false
	if v, ok := args["replace_all"]; ok {
		if b, ok := v.(bool); ok {
			replaceAll = b
		}
	}

	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return NewErrorResponse(fmt.Errorf("invalid path: %w", err)), nil
	}

	if rc := GetReadCache(ctx); rc != nil {
		if !rc.HasBeenRead(abs) {
			return NewErrorResponse(fmt.Errorf("you must read the file first before editing it")), nil
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return NewErrorResponse(fmt.Errorf("file not found: %s", path)), nil
		}
		return NewErrorResponse(fmt.Errorf("read file: %w", err)), nil
	}

	oldContent := string(data)

	if !strings.Contains(oldContent, oldStr) {
		return NewErrorResponse(fmt.Errorf("old_string not found in file")), nil
	}

	var count int
	var newContent string
	if replaceAll {
		count = strings.Count(oldContent, oldStr)
		newContent = strings.ReplaceAll(oldContent, oldStr, newStr)
	} else {
		count = strings.Count(oldContent, oldStr)
		if count > 1 {
			return NewErrorResponse(fmt.Errorf("old_string is not unique in file (%d occurrences); use replace_all=true or provide more context", count)), nil
		}
		newContent = strings.Replace(oldContent, oldStr, newStr, 1)
		count = 1
	}

	if err := os.WriteFile(abs, []byte(newContent), 0o644); err != nil {
		return NewErrorResponse(fmt.Errorf("write file: %w", err)), nil
	}

	resp := NewTextResponse(fmt.Sprintf("Replaced %d occurrence(s) in %s", count, abs))

	// Generate diff metadata
	diff := generateUnifiedDiff(abs, oldContent, newContent)
	if diff != "" {
		if resp.Metadata == nil {
			resp.Metadata = make(map[string]any)
		}
		resp.Metadata["diff"] = diff
	}

	return resp, nil
}

// CheckPermissions checks for dangerous paths and defers to AcceptEdits mode.
func (t *editTool) CheckPermissions(input map[string]any, ctx *permission.Context) permission.Decision {
	path, _ := input["file_path"].(string)
	if path == "" {
		return permission.Decision{Behavior: permission.BehaviorPassthrough}
	}

	// Dangerous path -> bypass-immune ASK
	if dangerous, reason := CheckDangerousPath(path); dangerous {
		return permission.Decision{
			Behavior:     permission.BehaviorAsk,
			Message:      reason,
			BypassImmune: true,
		}
	}

	// AcceptEdits mode allows edits
	if ctx != nil && ctx.Mode == permission.ModeAcceptEdits {
		return permission.Decision{Behavior: permission.BehaviorAllow}
	}

	return permission.Decision{Behavior: permission.BehaviorPassthrough}
}

// MatchRule checks whether a permission rule's glob pattern matches the file path.
func (t *editTool) MatchRule(ruleContent string, input map[string]any) bool {
	if ruleContent == "" {
		return true
	}
	path, _ := input["file_path"].(string)
	if path == "" {
		return false
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return false
	}
	matched, _ := filepath.Match(ruleContent, abs)
	if matched {
		return true
	}
	matched, _ = filepath.Match(ruleContent, filepath.Base(abs))
	return matched
}

// GenerateSuggestions produces a permission rule for the parent directory.
func (t *editTool) GenerateSuggestions(input map[string]any) []permission.Rule {
	path, _ := input["file_path"].(string)
	if path == "" {
		return []permission.Rule{{
			ToolName: t.ToolName,
			Behavior: permission.BehaviorAllow,
			Source:   "suggested",
		}}
	}

	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return []permission.Rule{{
			ToolName: t.ToolName,
			Behavior: permission.BehaviorAllow,
			Source:   "suggested",
		}}
	}

	parentDir := filepath.Dir(abs)
	return []permission.Rule{{
		ToolName:    t.ToolName,
		RuleContent: filepath.Join(parentDir, "**"),
		Behavior:    permission.BehaviorAllow,
		Source:      "suggested",
	}}
}

// EditTool returns a tool that performs search-and-replace edits on files.
func EditTool() Tool {
	return &editTool{
		BaseTool: BaseTool{
			ToolName:        "Edit",
			ToolDescription: "Edit a file by replacing exact text matches. By default requires old_string to be unique; use replace_all=true for multiple occurrences.",
			ToolSchema:      editSchema,
		},
	}
}
