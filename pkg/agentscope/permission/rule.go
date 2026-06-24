package permission

// Rule defines a permission policy for a specific tool or tool operation.
//
// The RuleContent field has tool-specific semantics:
//   - For "Bash": substring pattern matched against the command
//   - For "Write"/"Read"/"Edit": glob pattern matched against file paths
//   - For other tools: tool-specific filter, or empty to match all invocations
type Rule struct {
	ToolName    string             `json:"tool_name"`
	RuleContent string             `json:"rule_content,omitempty"`
	Behavior    PermissionBehavior `json:"behavior"`
	Source      string             `json:"source"`
}
