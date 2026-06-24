package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var writeSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"file_path": {
			"type": "string",
			"description": "Absolute or relative path to the file to write"
		},
		"content": {
			"type": "string",
			"description": "The content to write to the file (complete overwrite)"
		}
	},
	"required": ["file_path", "content"]
}`)

type writeTool struct {
	BaseTool
}

func (t *writeTool) Execute(ctx context.Context, args map[string]any) (*ToolResponse, error) {
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

	contentRaw, ok := args["content"]
	if !ok {
		return NewErrorResponse(fmt.Errorf("content is required")), nil
	}
	content, ok := contentRaw.(string)
	if !ok {
		return NewErrorResponse(fmt.Errorf("content must be a string")), nil
	}

	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return NewErrorResponse(fmt.Errorf("invalid path: %w", err)), nil
	}

	if _, statErr := os.Stat(abs); statErr == nil {
		if rc := GetReadCache(ctx); rc != nil {
			if !rc.HasBeenRead(abs) {
				return NewErrorResponse(fmt.Errorf("file exists but has not been read yet; you must read the file first before writing to it")), nil
			}
		}
	}

	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return NewErrorResponse(fmt.Errorf("create directories: %w", err)), nil
	}

	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return NewErrorResponse(fmt.Errorf("write file: %w", err)), nil
	}

	return NewTextResponse(fmt.Sprintf("Written %d bytes to %s", len(content), abs)), nil
}

// WriteTool returns a tool that writes content to a file (complete overwrite).
func WriteTool() Tool {
	return &writeTool{
		BaseTool: BaseTool{
			ToolName:        "write",
			ToolDescription: "Write content to a file, creating directories as needed. Completely overwrites existing content.",
			ToolSchema:      writeSchema,
		},
	}
}
