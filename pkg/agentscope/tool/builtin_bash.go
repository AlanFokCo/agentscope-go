package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
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
			"description": "Timeout in milliseconds (default 120000, max 600000)"
		},
		"description": {
			"type": "string",
			"description": "A short description of what this command does"
		},
		"run_in_background": {
			"type": "boolean",
			"description": "If true, run the command in the background and return immediately with a task ID"
		}
	},
	"required": ["command"]
}`)

// BashOption configures the bash tool.
type BashOption func(*bashTool)

// WithCwd sets the working directory for command execution.
func WithCwd(dir string) BashOption {
	return func(t *bashTool) {
		t.cwd = dir
	}
}

// WithBackgroundManager sets a manager for background command execution.
func WithBackgroundManager(mgr BackgroundManager) BashOption {
	return func(t *bashTool) {
		t.bgManager = mgr
	}
}

// WithMaxOutputBytes sets the maximum output size in bytes (default 1MB).
func WithMaxOutputBytes(n int) BashOption {
	return func(t *bashTool) {
		t.maxOutputBytes = n
	}
}

type bashTool struct {
	BaseTool
	cwd            string
	bgManager      BackgroundManager
	maxOutputBytes int
}

const (
	defaultTimeoutMs    = 120000
	maxTimeoutMs        = 600000
	defaultMaxOutputLen = 1024 * 1024 // 1 MB output cap
)

// BackgroundManager manages background command execution.
type BackgroundManager interface {
	Submit(name string, fn func(ctx context.Context) error) string
}

func (t *bashTool) Execute(ctx context.Context, args map[string]any) (*ToolResponse, error) {
	cmdStr, timeoutMs, _, runBg, err := t.parseArgs(args)
	if err != nil {
		return NewErrorResponse(err), nil
	}

	if runBg && t.bgManager != nil {
		taskID := t.bgManager.Submit(cmdStr, func(bgCtx context.Context) error {
			_, execErr := t.runCommand(bgCtx, cmdStr, timeoutMs)
			return execErr
		})
		return NewTextResponse(fmt.Sprintf(`{"task_id": %q, "status": "running"}`, taskID)), nil
	}

	return t.runCommand(ctx, cmdStr, timeoutMs)
}

// ExecuteStream implements StreamingTool. It streams stdout/stderr line by line.
func (t *bashTool) ExecuteStream(ctx context.Context, args map[string]any) (<-chan ToolChunk, error) {
	cmdStr, timeoutMs, _, _, err := t.parseArgs(args)
	if err != nil {
		ch := make(chan ToolChunk, 1)
		ch <- ToolChunk{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: err.Error()}},
			IsFinal: true,
			State:   message.ToolResultError,
		}
		close(ch)
		return ch, nil
	}

	ch := make(chan ToolChunk, 64)

	go func() {
		defer close(ch)

		runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
		defer cancel()

		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.CommandContext(runCtx, "cmd", "/c", cmdStr)
		} else {
			cmd = exec.CommandContext(runCtx, "/bin/sh", "-c", cmdStr)
		}
		if t.cwd != "" {
			cmd.Dir = t.cwd
		}

		stdout, pipeErr := cmd.StdoutPipe()
		if pipeErr != nil {
			ch <- ToolChunk{
				Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: pipeErr.Error()}},
				IsFinal: true,
				State:   message.ToolResultError,
			}
			return
		}
		cmd.Stderr = cmd.Stdout // merge stderr into stdout pipe

		if startErr := cmd.Start(); startErr != nil {
			ch <- ToolChunk{
				Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: startErr.Error()}},
				IsFinal: true,
				State:   message.ToolResultError,
			}
			return
		}

		var accumulated strings.Builder
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)

		for scanner.Scan() {
			line := scanner.Text() + "\n"
			accumulated.WriteString(line)
			ch <- ToolChunk{
				Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: line}},
			}
		}

		waitErr := cmd.Wait()

		exitCode := 0
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}

		interrupted := ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled)

		var finalState message.ToolResultState
		switch {
		case interrupted:
			finalState = message.ToolResultInterrupted
		case exitCode != 0:
			finalState = message.ToolResultError
		default:
			finalState = message.ToolResultSuccess
		}

		// Emit the final chunk with full accumulated output for context storage
		result := map[string]any{
			"exit_code": exitCode,
			"output":    accumulated.String(),
		}
		if waitErr != nil && !interrupted {
			result["error"] = waitErr.Error()
		}
		b, _ := json.Marshal(result)

		ch <- ToolChunk{
			Content: []message.ContentBlock{message.TextBlock{Type: "text", ID: "final", Text: string(b)}},
			IsFinal: true,
			State:   finalState,
		}
	}()

	return ch, nil
}

func (t *bashTool) parseArgs(args map[string]any) (command string, timeoutMs int, description string, runBg bool, err error) {
	raw, ok := args["command"]
	if !ok {
		return "", 0, "", false, fmt.Errorf("command is required")
	}
	command, ok = raw.(string)
	if !ok {
		return "", 0, "", false, fmt.Errorf("command must be a string")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", 0, "", false, fmt.Errorf("command cannot be empty")
	}

	timeoutMs = defaultTimeoutMs
	if v, ok := args["timeout"]; ok {
		switch n := v.(type) {
		case float64:
			timeoutMs = int(n)
		case int:
			timeoutMs = n
		}
		if timeoutMs <= 0 {
			timeoutMs = defaultTimeoutMs
		}
		if timeoutMs > maxTimeoutMs {
			timeoutMs = maxTimeoutMs
		}
	}

	if v, ok := args["description"].(string); ok {
		description = v
	}

	if v, ok := args["run_in_background"].(bool); ok {
		runBg = v
	}

	return command, timeoutMs, description, runBg, nil
}

func (t *bashTool) runCommand(ctx context.Context, cmdStr string, timeoutMs int) (*ToolResponse, error) {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, "cmd", "/c", cmdStr)
	} else {
		cmd = exec.CommandContext(runCtx, "/bin/sh", "-c", cmdStr)
	}
	if t.cwd != "" {
		cmd.Dir = t.cwd
	}

	// Use pipes for streaming-capable output capture
	stdout, pipeErr := cmd.StdoutPipe()
	if pipeErr != nil {
		return NewErrorResponse(fmt.Errorf("pipe: %w", pipeErr)), nil
	}
	cmd.Stderr = cmd.Stdout

	if startErr := cmd.Start(); startErr != nil {
		return NewErrorResponse(fmt.Errorf("start: %w", startErr)), nil
	}

	var mu sync.Mutex
	var output strings.Builder
	maxLen := t.maxOutputBytes
	if maxLen <= 0 {
		maxLen = defaultMaxOutputLen
	}
	truncated := false

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		mu.Lock()
		if output.Len() < maxLen {
			output.WriteString(scanner.Text())
			output.WriteByte('\n')
		} else if !truncated {
			truncated = true
		}
		mu.Unlock()
	}

	// Drain remaining data if scanner stopped due to token size
	if scanner.Err() != nil {
		remaining, _ := io.ReadAll(stdout)
		mu.Lock()
		if output.Len() < maxLen {
			output.Write(remaining)
		}
		mu.Unlock()
	}

	waitErr := cmd.Wait()

	if ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
		return &ToolResponse{
			Content: []message.ContentBlock{message.TextBlock{
				Type: "text",
				Text: fmt.Sprintf("Command interrupted.\nPartial output:\n%s", output.String()),
			}},
			State: message.ToolResultInterrupted,
		}, nil
	}

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	result := map[string]any{
		"exit_code": exitCode,
		"output":    output.String(),
	}
	if truncated {
		result["truncated"] = true
	}
	if waitErr != nil {
		result["error"] = waitErr.Error()
	}

	b, _ := json.Marshal(result)
	return NewTextResponse(string(b)), nil
}

// CheckPermissions implements a 7-step permission chain:
// 1. Injection risk (command substitution, eval, etc.) -> bypass-immune ASK
// 2. Read-only command -> ALLOW
// 3. Dangerous command patterns -> bypass-immune ASK
// 4. Sed constraints -> bypass-immune ASK if violated
// 5. Dangerous file paths in arguments -> bypass-immune ASK
// 6. Dangerous removal patterns -> bypass-immune ASK
// 7. AcceptEdits mode -> ALLOW; otherwise PASSTHROUGH
func (t *bashTool) CheckPermissions(input map[string]any, ctx *permission.Context) permission.Decision {
	cmdStr, _ := input["command"].(string)
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return permission.Decision{Behavior: permission.BehaviorPassthrough}
	}

	// Step 1: Injection risk
	if risky, reason := CheckInjectionRisk(cmdStr); risky {
		return permission.Decision{
			Behavior:     permission.BehaviorAsk,
			Message:      reason,
			BypassImmune: true,
		}
	}

	// Step 2: Read-only commands get immediate allow
	if IsReadOnlyCommand(cmdStr) {
		return permission.Decision{Behavior: permission.BehaviorAllow}
	}

	// Step 3: Dangerous command patterns
	if dangerous, reason := CheckDangerousCommand(cmdStr); dangerous {
		return permission.Decision{
			Behavior:     permission.BehaviorAsk,
			Message:      reason,
			BypassImmune: true,
		}
	}

	// Step 4: Sed constraints
	if violated, reason := CheckSedConstraints(cmdStr, DangerousFiles); violated {
		return permission.Decision{
			Behavior:     permission.BehaviorAsk,
			Message:      reason,
			BypassImmune: true,
		}
	}

	// Step 5: Dangerous file paths
	if dangerous, reason := CheckDangerousFilePaths(cmdStr); dangerous {
		return permission.Decision{
			Behavior:     permission.BehaviorAsk,
			Message:      reason,
			BypassImmune: true,
		}
	}

	// Step 6: Dangerous removal
	if dangerous, reason := CheckDangerousRemoval(cmdStr); dangerous {
		return permission.Decision{
			Behavior:     permission.BehaviorAsk,
			Message:      reason,
			BypassImmune: true,
		}
	}

	// Step 7: AcceptEdits mode allows remaining commands
	if ctx != nil && ctx.Mode == permission.ModeAcceptEdits {
		return permission.Decision{Behavior: permission.BehaviorAllow}
	}

	return permission.Decision{Behavior: permission.BehaviorPassthrough}
}

// CheckReadOnly checks whether this specific bash invocation is read-only.
func (t *bashTool) CheckReadOnly(input map[string]any) bool {
	cmdStr, _ := input["command"].(string)
	return IsReadOnlyCommand(strings.TrimSpace(cmdStr))
}

// MatchRule checks whether a permission rule content matches the bash command.
// Supports glob-like patterns: "git *" matches "git status", "git log", etc.
// An empty ruleContent matches all bash invocations.
func (t *bashTool) MatchRule(ruleContent string, input map[string]any) bool {
	if ruleContent == "" {
		return true
	}

	cmdStr, _ := input["command"].(string)
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return false
	}

	// Convert glob-like rule to regex
	pattern := ruleToRegex(ruleContent)
	re, err := regexp.Compile("^" + pattern + "$")
	if err != nil {
		// Fallback to substring matching
		return strings.Contains(cmdStr, ruleContent)
	}

	return re.MatchString(cmdStr)
}

// GenerateSuggestions produces permission rule suggestions based on command prefixes.
func (t *bashTool) GenerateSuggestions(input map[string]any) []permission.Rule {
	cmdStr, _ := input["command"].(string)
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return []permission.Rule{{
			ToolName: t.ToolName,
			Behavior: permission.BehaviorAllow,
			Source:   "suggested",
		}}
	}

	prefixes := ExtractCommandPrefixes(cmdStr, 3)
	var rules []permission.Rule
	for _, prefix := range prefixes {
		rules = append(rules, permission.Rule{
			ToolName:    t.ToolName,
			RuleContent: prefix,
			Behavior:    permission.BehaviorAllow,
			Source:      "suggested",
		})
	}

	if len(rules) == 0 {
		rules = append(rules, permission.Rule{
			ToolName: t.ToolName,
			Behavior: permission.BehaviorAllow,
			Source:   "suggested",
		})
	}

	return rules
}

// ruleToRegex converts a glob-like rule pattern to a regex:
// "*" -> ".*", "?" -> ".", literal chars are escaped.
func ruleToRegex(pattern string) string {
	// Handle the common "prefix:*" format
	if strings.HasSuffix(pattern, ":*") {
		prefix := pattern[:len(pattern)-2]
		return regexp.QuoteMeta(prefix) + ".*"
	}

	var b strings.Builder
	for _, c := range pattern {
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// BashTool returns a tool that executes shell commands with streaming support.
// It implements both Tool and StreamingTool interfaces.
func BashTool(opts ...BashOption) Tool {
	t := &bashTool{
		BaseTool: BaseTool{
			ToolName:        "Bash",
			ToolDescription: "Execute a shell command and return stdout/stderr. Supports streaming output for long-running commands and background execution.",
			ToolSchema:      bashSchema,
		},
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}
