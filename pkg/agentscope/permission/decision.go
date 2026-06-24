package permission

// Decision is the result of a permission check.
type Decision struct {
	Behavior       PermissionBehavior `json:"behavior"`
	Message        string             `json:"message"`
	DecisionReason string             `json:"decision_reason,omitempty"`
	UpdatedInput   map[string]any     `json:"updated_input,omitempty"`
	SuggestedRules []Rule             `json:"suggested_rules,omitempty"`

	// BypassImmune marks a safety Ask that cannot be overridden by allow rules
	// in Default/AcceptEdits modes. In Bypass mode this field is ignored; in
	// DontAsk mode the Ask is converted to Deny.
	BypassImmune bool `json:"bypass_immune,omitempty"`
}
