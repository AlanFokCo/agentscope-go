package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
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
			"description": "1-based line number to start reading from (default 1, i.e. the first line)"
		},
		"limit": {
			"type": "integer",
			"description": "Maximum number of lines to read (default 2000)"
		}
	},
	"required": ["file_path"]
}`)

const (
	defaultReadLimit    = 2000
	maxLineLengthChars  = 2000
	lineTruncatedSuffix = " [truncated]"
)

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
		// User provides 1-based offset; convert to 0-based internally.
		offset = toInt(v) - 1
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
		defer f.Close() //nolint:errcheck

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
		line := rawLines[i]
		// Truncate very long lines to prevent output explosion
		if len(line) > maxLineLengthChars {
			line = line[:maxLineLengthChars] + lineTruncatedSuffix
		}
		lines = append(lines, fmt.Sprintf("%d\t%s", i+1, line))
	}

	content := strings.Join(lines, "\n")
	return NewTextResponse(content), nil
}

// CheckPermissions returns passthrough for read operations (reads are generally safe).
func (t *readTool) CheckPermissions(input map[string]any, ctx *permission.Context) permission.Decision {
	return permission.Decision{Behavior: permission.BehaviorPassthrough}
}

// MatchRule checks whether a permission rule's glob pattern matches the file path.
func (t *readTool) MatchRule(ruleContent string, input map[string]any) bool {
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
	// Also try matching against just the base name
	matched, _ = filepath.Match(ruleContent, filepath.Base(abs))
	return matched
}

// ReadTool returns a tool that reads text files with optional line offset and limit.
func ReadTool() Tool {
	return &readTool{
		BaseTool: BaseTool{
			ToolName:        "Read",
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
