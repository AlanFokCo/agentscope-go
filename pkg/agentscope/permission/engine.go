package permission

import "fmt"

// Engine evaluates tool execution requests against configured permission rules.
// Each PermissionMode has its own check method so mode policies are
// self-contained and readable in isolation.
type Engine struct {
	Context *Context
}

// NewEngine creates a permission engine with the given context.
func NewEngine(ctx *Context) *Engine {
	return &Engine{Context: ctx}
}

// AddRule adds a permission rule to the engine's context.
func (e *Engine) AddRule(rule Rule) {
	switch rule.Behavior {
	case BehaviorAllow:
		e.Context.AllowRules[rule.ToolName] = append(e.Context.AllowRules[rule.ToolName], rule)
	case BehaviorDeny:
		e.Context.DenyRules[rule.ToolName] = append(e.Context.DenyRules[rule.ToolName], rule)
	case BehaviorAsk:
		e.Context.AskRules[rule.ToolName] = append(e.Context.AskRules[rule.ToolName], rule)
	}
}

// CheckPermission evaluates a tool execution request and returns a Decision.
func (e *Engine) CheckPermission(tool Checker, input map[string]any) (Decision, error) {
	switch e.Context.Mode {
	case ModeDefault:
		return e.checkDefault(tool, input), nil
	case ModeExplore:
		return e.checkExplore(tool, input), nil
	case ModeAcceptEdits:
		return e.checkAcceptEdits(tool, input), nil
	case ModeBypass:
		return e.checkBypass(tool, input), nil
	case ModeDontAsk:
		return e.checkDontAsk(tool, input), nil
	default:
		return Decision{}, fmt.Errorf("permission: unknown mode %q", e.Context.Mode)
	}
}

// checkDefault implements ModeDefault:
//  1. Deny rules -> DENY
//  2. Ask rules -> ASK (with suggestions)
//  3. tool.CheckPermissions: ALLOW/DENY returned as-is; safety ASK (bypass-immune)
//     returned with suggestions; non-safety ASK/PASSTHROUGH -> continue
//  4. Allow rules -> ALLOW
//  5. Default -> ASK (with suggestions)
func (e *Engine) checkDefault(tool Checker, input map[string]any) Decision {
	if d := e.checkDenyRules(tool, input); d != nil {
		return *d
	}
	if d := e.checkAskRules(tool, input); d != nil {
		d.SuggestedRules = e.generateSuggestions(tool, input)
		return *d
	}

	td := tool.CheckPermissions(input, e.Context)
	if td.Behavior == BehaviorAllow || td.Behavior == BehaviorDeny {
		return td
	}
	if isSafetyAsk(td) {
		td.SuggestedRules = e.generateSuggestions(tool, input)
		return td
	}

	if d := e.checkAllowRules(tool, input); d != nil {
		return *d
	}

	return Decision{
		Behavior:       BehaviorAsk,
		Message:        fmt.Sprintf("Permission required for %s", tool.Name()),
		DecisionReason: fmt.Sprintf("Mode: %s", e.Context.Mode),
		SuggestedRules: e.generateSuggestions(tool, input),
	}
}

// checkExplore implements ModeExplore (read-only):
//  1. Deny rules -> DENY
//  2. Ask rules -> ASK (with suggestions)
//  3. tool.CheckReadOnly: true -> ALLOW, false -> DENY
func (e *Engine) checkExplore(tool Checker, input map[string]any) Decision {
	if d := e.checkDenyRules(tool, input); d != nil {
		return *d
	}
	if d := e.checkAskRules(tool, input); d != nil {
		d.SuggestedRules = e.generateSuggestions(tool, input)
		return *d
	}

	if tool.CheckReadOnly(input) {
		return Decision{
			Behavior:       BehaviorAllow,
			Message:        fmt.Sprintf("Permission granted for %s (explore mode - read-only invocation)", tool.Name()),
			DecisionReason: "Explore mode allows read-only operations",
		}
	}
	return Decision{
		Behavior:       BehaviorDeny,
		Message:        fmt.Sprintf("Permission denied for %s (explore mode is read-only)", tool.Name()),
		DecisionReason: "Explore mode does not allow modifications",
	}
}

// checkAcceptEdits implements ModeAcceptEdits:
//  1. Deny rules -> DENY
//  2. Ask rules -> ASK (with suggestions)
//  3. tool.CheckReadOnly -> true -> ALLOW (fast path)
//  4. tool.CheckPermissions: ALLOW/DENY as-is; safety ASK -> with suggestions;
//     non-safety ASK/PASSTHROUGH -> continue
//  5. Allow rules -> ALLOW
//  6. Default -> ASK (with suggestions)
func (e *Engine) checkAcceptEdits(tool Checker, input map[string]any) Decision {
	if d := e.checkDenyRules(tool, input); d != nil {
		return *d
	}
	if d := e.checkAskRules(tool, input); d != nil {
		d.SuggestedRules = e.generateSuggestions(tool, input)
		return *d
	}

	if tool.CheckReadOnly(input) {
		return Decision{
			Behavior:       BehaviorAllow,
			Message:        fmt.Sprintf("Permission granted for %s (accept edits mode - read-only invocation)", tool.Name()),
			DecisionReason: "Accept edits mode allows read-only operations",
		}
	}

	td := tool.CheckPermissions(input, e.Context)
	if td.Behavior == BehaviorAllow || td.Behavior == BehaviorDeny {
		return td
	}
	if isSafetyAsk(td) {
		td.SuggestedRules = e.generateSuggestions(tool, input)
		return td
	}

	if d := e.checkAllowRules(tool, input); d != nil {
		return *d
	}

	return Decision{
		Behavior:       BehaviorAsk,
		Message:        fmt.Sprintf("Permission required for %s", tool.Name()),
		DecisionReason: fmt.Sprintf("Mode: %s", e.Context.Mode),
		SuggestedRules: e.generateSuggestions(tool, input),
	}
}

// checkBypass implements ModeBypass:
//  1. Deny rules -> DENY
//  2. Ask rules -> ASK (honors explicit user intent)
//  3. tool.CheckPermissions: ALLOW/DENY as-is; any ASK (including bypass-immune) falls through
//  4. Allow rules -> ALLOW
//  5. Fallback -> ALLOW
func (e *Engine) checkBypass(tool Checker, input map[string]any) Decision {
	if d := e.checkDenyRules(tool, input); d != nil {
		return *d
	}
	if d := e.checkAskRules(tool, input); d != nil {
		d.SuggestedRules = e.generateSuggestions(tool, input)
		return *d
	}

	td := tool.CheckPermissions(input, e.Context)
	if td.Behavior == BehaviorAllow || td.Behavior == BehaviorDeny {
		return td
	}

	if d := e.checkAllowRules(tool, input); d != nil {
		return *d
	}

	return Decision{
		Behavior:       BehaviorAllow,
		Message:        fmt.Sprintf("Permission granted for %s (bypass mode)", tool.Name()),
		DecisionReason: "Bypass mode allows all operations",
	}
}

// checkDontAsk implements ModeDontAsk:
//  1. Deny rules -> DENY
//  2. Ask rules -> DENY (converted)
//  3. tool.CheckPermissions: ALLOW/DENY as-is; safety ASK -> DENY; non-safety ASK/PASSTHROUGH -> continue
//  4. Allow rules -> ALLOW
//  5. Default -> DENY
func (e *Engine) checkDontAsk(tool Checker, input map[string]any) Decision {
	if d := e.checkDenyRules(tool, input); d != nil {
		return *d
	}
	if d := e.checkAskRules(tool, input); d != nil {
		d.SuggestedRules = e.generateSuggestions(tool, input)
		return convertAskToDeny(tool, *d)
	}

	td := tool.CheckPermissions(input, e.Context)
	if td.Behavior == BehaviorAllow || td.Behavior == BehaviorDeny {
		return td
	}
	if isSafetyAsk(td) {
		td.SuggestedRules = e.generateSuggestions(tool, input)
		return convertAskToDeny(tool, td)
	}

	if d := e.checkAllowRules(tool, input); d != nil {
		return *d
	}

	return Decision{
		Behavior:       BehaviorDeny,
		Message:        fmt.Sprintf("Permission denied for %s (dont_ask mode - user not available)", tool.Name()),
		DecisionReason: "User is not available to answer permission prompts",
	}
}

func (e *Engine) checkDenyRules(tool Checker, input map[string]any) *Decision {
	for _, rule := range e.Context.DenyRules[tool.Name()] {
		if e.ruleMatches(tool, rule, input) {
			return &Decision{
				Behavior:       BehaviorDeny,
				Message:        fmt.Sprintf("Permission to use %s has been denied", tool.Name()),
				DecisionReason: fmt.Sprintf("Rule: %s", rule.RuleContent),
			}
		}
	}
	return nil
}

func (e *Engine) checkAskRules(tool Checker, input map[string]any) *Decision {
	for _, rule := range e.Context.AskRules[tool.Name()] {
		if e.ruleMatches(tool, rule, input) {
			return &Decision{
				Behavior:       BehaviorAsk,
				Message:        fmt.Sprintf("Permission required for %s", tool.Name()),
				DecisionReason: fmt.Sprintf("Rule: %s", rule.RuleContent),
			}
		}
	}
	return nil
}

func (e *Engine) checkAllowRules(tool Checker, input map[string]any) *Decision {
	for _, rule := range e.Context.AllowRules[tool.Name()] {
		if e.ruleMatches(tool, rule, input) {
			return &Decision{
				Behavior:    BehaviorAllow,
				Message:     fmt.Sprintf("Permission granted for %s", tool.Name()),
				UpdatedInput: input,
			}
		}
	}
	return nil
}

func (e *Engine) ruleMatches(tool Checker, rule Rule, input map[string]any) bool {
	if rule.RuleContent == "" {
		return true
	}
	return tool.MatchRule(rule.RuleContent, input)
}

func (e *Engine) generateSuggestions(tool Checker, input map[string]any) []Rule {
	return tool.GenerateSuggestions(input)
}

func isSafetyAsk(d Decision) bool {
	return d.Behavior == BehaviorAsk && d.BypassImmune
}

func convertAskToDeny(tool Checker, ask Decision) Decision {
	return Decision{
		Behavior: BehaviorDeny,
		Message: fmt.Sprintf(
			"Permission denied for %s (dont_ask mode - ASK converted to DENY, user not available)",
			tool.Name(),
		),
		DecisionReason: fmt.Sprintf("DONT_ASK mode converted ASK to DENY. Original reason: %s", ask.DecisionReason),
		SuggestedRules: ask.SuggestedRules,
	}
}
