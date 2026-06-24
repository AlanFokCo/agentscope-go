package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

	content := string(data)

	if !strings.Contains(content, oldStr) {
		return NewErrorResponse(fmt.Errorf("old_string not found in file")), nil
	}

	var count int
	if replaceAll {
		count = strings.Count(content, oldStr)
		content = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		count = strings.Count(content, oldStr)
		if count > 1 {
			return NewErrorResponse(fmt.Errorf("old_string is not unique in file (%d occurrences); use replace_all=true or provide more context", count)), nil
		}
		content = strings.Replace(content, oldStr, newStr, 1)
		count = 1
	}

	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return NewErrorResponse(fmt.Errorf("write file: %w", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("Replaced %d occurrence(s) in %s", count, abs)), nil
}

// EditTool returns a tool that performs search-and-replace edits on files.
func EditTool() Tool {
	return &editTool{
		BaseTool: BaseTool{
			ToolName:        "edit",
			ToolDescription: "Edit a file by replacing exact text matches. By default requires old_string to be unique; use replace_all=true for multiple occurrences.",
			ToolSchema:      editSchema,
		},
	}
}
