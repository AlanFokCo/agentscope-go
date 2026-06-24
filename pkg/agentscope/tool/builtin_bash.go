package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

var bashSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"command": {
			"type": "string",
			"description": "The shell command to execute"
		},
		"timeout": {
			"type": "number",
			"description": "Timeout in seconds (default 30, max 300)"
		}
	},
	"required": ["command"]
}`)

type bashTool struct {
	BaseTool
}

func (t *bashTool) Execute(ctx context.Context, args map[string]any) (*ToolResponse, error) {
	raw, ok := args["command"]
	if !ok {
		return NewErrorResponse(fmt.Errorf("command is required")), nil
	}
	cmdStr, ok := raw.(string)
	if !ok {
		return NewErrorResponse(fmt.Errorf("command must be a string")), nil
	}
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return NewErrorResponse(fmt.Errorf("command cannot be empty")), nil
	}

	if dangerous, reason := CheckDangerousCommand(cmdStr); dangerous {
		return NewErrorResponse(fmt.Errorf("blocked: %s — this command requires explicit user approval", reason)), nil
	}

	timeoutSec := DefaultShellTimeout
	if v, ok := args["timeout"]; ok {
		switch n := v.(type) {
		case float64:
			timeoutSec = int(n)
		case int:
			timeoutSec = n
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

	result := map[string]any{
		"exit_code": exitCode,
		"output":    string(out),
	}
	if err != nil {
		result["error"] = err.Error()
	}

	b, _ := json.Marshal(result)
	return NewTextResponse(string(b)), nil
}

// BashTool returns a tool that executes shell commands.
// This is the enhanced replacement for execute_shell_command.
func BashTool() Tool {
	return &bashTool{
		BaseTool: BaseTool{
			ToolName:        "bash",
			ToolDescription: "Execute a shell command and return stdout/stderr. Use for running programs, scripts, git commands, etc.",
			ToolSchema:      bashSchema,
		},
	}
}
