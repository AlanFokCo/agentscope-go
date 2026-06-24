package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var readSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"file_path": {
			"type": "string",
			"description": "Absolute or relative path to the file to read"
		},
		"offset": {
			"type": "integer",
			"description": "Line number to start reading from (0-based, default 0)"
		},
		"limit": {
			"type": "integer",
			"description": "Maximum number of lines to read (default 2000)"
		}
	},
	"required": ["file_path"]
}`)

const defaultReadLimit = 2000

type readTool struct {
	BaseTool
}

func (t *readTool) Execute(ctx context.Context, args map[string]any) (*ToolResponse, error) {
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

	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return NewErrorResponse(fmt.Errorf("invalid path: %w", err)), nil
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return NewErrorResponse(fmt.Errorf("file not found: %s", path)), nil
		}
		return NewErrorResponse(fmt.Errorf("stat: %w", err)), nil
	}
	if info.IsDir() {
		return NewErrorResponse(fmt.Errorf("path is a directory: %s", path)), nil
	}
	if info.Size() > MaxFileSize {
		return NewErrorResponse(fmt.Errorf("file too large (%d bytes, max %d): %s", info.Size(), MaxFileSize, path)), nil
	}

	offset := 0
	if v, ok := args["offset"]; ok {
		offset = toInt(v)
	}
	if offset < 0 {
		offset = 0
	}

	limit := defaultReadLimit
	if v, ok := args["limit"]; ok {
		n := toInt(v)
		if n > 0 {
			limit = n
		}
	}

	var rawLines []string
	rc := GetReadCache(ctx)

	if rc != nil {
		if cached := rc.GetCache(abs); cached != nil {
			rawLines = cached.Lines
		}
	}

	if rawLines == nil {
		f, err := os.Open(abs)
		if err != nil {
			return NewErrorResponse(fmt.Errorf("open: %w", err)), nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			rawLines = append(rawLines, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return NewErrorResponse(fmt.Errorf("read: %w", err)), nil
		}

		if rc != nil {
			rc.CacheFile(abs, rawLines)
		}
	}

	end := offset + limit
	if end > len(rawLines) {
		end = len(rawLines)
	}

	var lines []string
	for i := offset; i < end; i++ {
		lines = append(lines, fmt.Sprintf("%d\t%s", i+1, rawLines[i]))
	}

	content := strings.Join(lines, "\n")
	return NewTextResponse(content), nil
}

// ReadTool returns a tool that reads text files with optional line offset and limit.
func ReadTool() Tool {
	return &readTool{
		BaseTool: BaseTool{
			ToolName:        "read",
			ToolDescription: "Read a text file's contents with line numbers. Supports offset and limit for large files (max 1MB).",
			ToolSchema:      readSchema,
			ReadOnly:        true,
			ConcurrencySafe: true,
		},
	}
}

func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}
