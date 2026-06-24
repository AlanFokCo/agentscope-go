package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
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

	// Detect context cancellation (user interrupt)
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return &ToolResponse{
				Content: []message.ContentBlock{message.TextBlock{
					Type: "text",
					Text: fmt.Sprintf("Command interrupted.\nPartial output:\n%s", string(out)),
				}},
				State: message.ToolResultInterrupted,
			}, nil
		}
	}

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
