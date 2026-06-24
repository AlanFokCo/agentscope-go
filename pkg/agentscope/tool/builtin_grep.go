package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var grepSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"pattern": {
			"type": "string",
			"description": "Search pattern (regex supported)"
		},
		"path": {
			"type": "string",
			"description": "File or directory to search in (default: current directory)"
		},
		"include": {
			"type": "string",
			"description": "File glob filter (e.g. '*.go', '*.ts')"
		}
	},
	"required": ["pattern"]
}`)

const maxGrepResults = 200

type grepTool struct {
	BaseTool
}

func (t *grepTool) Execute(ctx context.Context, args map[string]any) (*ToolResponse, error) {
	raw, ok := args["pattern"]
	if !ok {
		return NewErrorResponse(fmt.Errorf("pattern is required")), nil
	}
	pattern, ok := raw.(string)
	if !ok {
		return NewErrorResponse(fmt.Errorf("pattern must be a string")), nil
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return NewErrorResponse(fmt.Errorf("pattern cannot be empty")), nil
	}

	searchPath := "."
	if v, ok := args["path"].(string); ok && v != "" {
		searchPath = v
	}
	searchPath, err := filepath.Abs(searchPath)
	if err != nil {
		return NewErrorResponse(fmt.Errorf("invalid path: %w", err)), nil
	}

	include, _ := args["include"].(string)

	// Try ripgrep first (much faster for large codebases)
	if result, err := t.tryRipgrep(ctx, pattern, searchPath, include); err == nil {
		return result, nil
	}

	// Fallback to Go implementation
	return t.goGrep(pattern, searchPath, include)
}

func (t *grepTool) tryRipgrep(ctx context.Context, pattern, path, include string) (*ToolResponse, error) {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return nil, err
	}

	args := []string{
		"--line-number",
		"--no-heading",
		"--max-count", "5",
		"--max-filesize", "1M",
		"-m", fmt.Sprintf("%d", maxGrepResults),
	}
	if include != "" {
		args = append(args, "--glob", include)
	}
	args = append(args, pattern, path)

	cmd := exec.CommandContext(ctx, rgPath, args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return NewTextResponse("No matches found."), nil
		}
		return nil, fmt.Errorf("rg: %w", err)
	}

	result := string(out)
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) > maxGrepResults {
		lines = lines[:maxGrepResults]
		result = strings.Join(lines, "\n") + fmt.Sprintf("\n... (truncated at %d results)", maxGrepResults)
	}
	return NewTextResponse(strings.TrimSpace(result)), nil
}

func (t *grepTool) goGrep(pattern, path, include string) (*ToolResponse, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return NewErrorResponse(fmt.Errorf("invalid regex: %w", err)), nil
	}

	var includeRe *regexp.Regexp
	if include != "" {
		globRe := globToRegex(include)
		includeRe, _ = regexp.Compile(globRe)
	}

	var results []string
	err = filepath.Walk(path, func(fpath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if len(results) >= maxGrepResults {
			return filepath.SkipAll
		}
		if includeRe != nil && !includeRe.MatchString(info.Name()) {
			return nil
		}
		if info.Size() > MaxFileSize {
			return nil
		}

		f, err := os.Open(fpath)
		if err != nil {
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNo := 0
		matchesInFile := 0
		for scanner.Scan() {
			lineNo++
			if matchesInFile >= 5 {
				break
			}
			line := scanner.Text()
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d:%s", fpath, lineNo, line))
				matchesInFile++
				if len(results) >= maxGrepResults {
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return NewErrorResponse(fmt.Errorf("walk: %w", err)), nil
	}

	if len(results) == 0 {
		return NewTextResponse("No matches found."), nil
	}

	output := strings.Join(results, "\n")
	if len(results) >= maxGrepResults {
		output += fmt.Sprintf("\n... (truncated at %d results)", maxGrepResults)
	}
	return NewTextResponse(output), nil
}

func globToRegex(glob string) string {
	var b strings.Builder
	b.WriteString("(?i)")
	for _, c := range glob {
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '.':
			b.WriteString("\\.")
		default:
			b.WriteRune(c)
		}
	}
	b.WriteString("$")
	return b.String()
}

// GrepTool returns a tool that searches file contents using regex patterns.
// Prefers ripgrep (rg) if available, falls back to Go standard library.
func GrepTool() Tool {
	return &grepTool{
		BaseTool: BaseTool{
			ToolName:        "grep",
			ToolDescription: "Search file contents for a regex pattern. Uses ripgrep if available. Returns matching lines with file path and line number.",
			ToolSchema:      grepSchema,
			ReadOnly:        true,
			ConcurrencySafe: true,
		},
	}
}
