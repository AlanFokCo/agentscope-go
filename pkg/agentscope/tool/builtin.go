package tool

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// MaxFileSize limits view_text_file to 1MB to avoid memory issues.
	MaxFileSize = 1024 * 1024
	// DefaultShellTimeout is the default timeout for execute_shell_command (seconds).
	DefaultShellTimeout = 30
)

// ExecuteShellCommandTool returns a tool that runs shell commands.
// Args: "command" (string, required) - the command to execute.
//       "timeout" (number, optional) - timeout in seconds; default 30.
// Returns stdout and stderr combined, or an error message.
func ExecuteShellCommandTool() *Tool {
	return &Tool{
		Name:        "execute_shell_command",
		Description: "Execute a shell command. Args: command (string, required), timeout (number, optional, seconds, default 30). Returns combined stdout and stderr.",
		Execute:     executeShellCommand,
	}
}

func executeShellCommand(ctx context.Context, args map[string]any) (any, error) {
	raw, ok := args["command"]
	if !ok {
		return nil, fmt.Errorf("command is required")
	}
	cmdStr, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("command must be a string")
	}
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return nil, fmt.Errorf("command cannot be empty")
	}

	timeoutSec := DefaultShellTimeout
	if t, ok := args["timeout"]; ok {
		switch v := t.(type) {
		case float64:
			timeoutSec = int(v)
		case int:
			timeoutSec = v
		}
		if timeoutSec <= 0 {
			timeoutSec = DefaultShellTimeout
		}
		if timeoutSec > 300 {
			timeoutSec = 300
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, "cmd", "/c", cmdStr)
	} else {
		cmd = exec.CommandContext(runCtx, "/bin/sh", "-c", cmdStr)
	}
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		return map[string]any{
			"exit_code": exitCode,
			"output":    string(out),
			"error":     err.Error(),
		}, nil
	}
	return map[string]any{
		"exit_code": exitCode,
		"output":    string(out),
	}, nil
}

// ViewTextFileTool returns a tool that reads text file contents.
// Args: "path" or "file_path" (string, required) - path to the file.
// Returns file content as string, or an error message.
func ViewTextFileTool() *Tool {
	return &Tool{
		Name:        "view_text_file",
		Description: "Read the contents of a text file. Args: path or file_path (string, required). Returns file content.",
		Execute:     viewTextFile,
	}
}

func viewTextFile(ctx context.Context, args map[string]any) (any, error) {
	_ = ctx
	path := ""
	for _, k := range []string{"path", "file_path"} {
		if v, ok := args[k]; ok {
			if s, ok := v.(string); ok {
				path = strings.TrimSpace(s)
				break
			}
		}
	}
	if path == "" {
		return nil, fmt.Errorf("path or file_path is required")
	}

	clean := filepath.Clean(path)
	abs, err := filepath.Abs(clean)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		return nil, fmt.Errorf("stat: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file: %s", path)
	}
	if info.Size() > MaxFileSize {
		return nil, fmt.Errorf("file too large (max %d bytes): %s", MaxFileSize, path)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return map[string]any{
		"path":    abs,
		"content": string(data),
	}, nil
}

// NewBuiltinToolkit returns a Toolkit with execute_shell_command and view_text_file.
func NewBuiltinToolkit() *Toolkit {
	return NewToolkit(ExecuteShellCommandTool(), ViewTextFileTool())
}
