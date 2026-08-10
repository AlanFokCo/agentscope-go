package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	agenterrors "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/errors"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/internal/jsonx"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/permission"
)

// Tool is the interface all tools must implement.
type Tool interface {
	permission.Checker

	Description() string
	InputSchema() json.RawMessage
	Execute(ctx context.Context, input map[string]any) (*ToolResponse, error)
	IsConcurrencySafe() bool
	IsReadOnly() bool
	IsExternalTool() bool
}

// ToolResponse is the result of executing a tool.
type ToolResponse struct {
	Content  []message.ContentBlock
	State    message.ToolResultState
	Metadata map[string]any
}

// NewTextResponse creates a ToolResponse with a single text block.
func NewTextResponse(text string) *ToolResponse {
	return &ToolResponse{
		Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: text}},
		State:   message.ToolResultSuccess,
	}
}

// NewErrorResponse creates a ToolResponse representing an error.
func NewErrorResponse(err error) *ToolResponse {
	return &ToolResponse{
		Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: err.Error()}},
		State:   message.ToolResultError,
	}
}

// ToolMiddleware wraps individual tool executions in an onion-chain pattern.
type ToolMiddleware interface {
	Wrap(ctx context.Context, name string, input map[string]any, next ToolHandler) (any, error)
}

// ToolHandler is the next function in a tool middleware chain.
type ToolHandler func(ctx context.Context, name string, input map[string]any) (any, error)

// BaseTool provides embeddable defaults for the Tool interface.
// Embed this in concrete tool structs and override methods as needed.
type BaseTool struct {
	ToolName        string
	ToolDescription string
	ToolSchema      json.RawMessage
	ConcurrencySafe bool
	ReadOnly        bool
	StateInjected   bool
	Middlewares     []ToolMiddleware
}

func (b *BaseTool) Name() string                 { return b.ToolName }
func (b *BaseTool) Description() string          { return b.ToolDescription }
func (b *BaseTool) InputSchema() json.RawMessage { return b.ToolSchema }
func (b *BaseTool) IsConcurrencySafe() bool      { return b.ConcurrencySafe }
func (b *BaseTool) IsReadOnly() bool             { return b.ReadOnly }
func (b *BaseTool) IsExternalTool() bool         { return false }

// Execute must be overridden by embedding structs.
func (b *BaseTool) Execute(ctx context.Context, input map[string]any) (*ToolResponse, error) {
	return nil, fmt.Errorf("tool %q: Execute not implemented", b.ToolName)
}

// CheckPermissions returns Passthrough by default, deferring to the engine.
func (b *BaseTool) CheckPermissions(input map[string]any, ctx *permission.Context) permission.Decision {
	return permission.Decision{Behavior: permission.BehaviorPassthrough}
}

// CheckReadOnly returns the tool's static ReadOnly flag.
func (b *BaseTool) CheckReadOnly(input map[string]any) bool {
	return b.ReadOnly
}

// MatchRule returns true only for empty ruleContent (tool-name-level rules).
// Concrete tools override this for fine-grained matching.
func (b *BaseTool) MatchRule(ruleContent string, input map[string]any) bool {
	return ruleContent == ""
}

// GenerateSuggestions returns a single tool-name-level allow rule.
func (b *BaseTool) GenerateSuggestions(input map[string]any) []permission.Rule {
	return []permission.Rule{{
		ToolName: b.ToolName,
		Behavior: permission.BehaviorAllow,
		Source:   "suggested",
	}}
}

// FunctionTool wraps a plain function into the Tool interface.
type FunctionTool struct {
	BaseTool
	Fn func(ctx context.Context, input map[string]any) (any, error)
}

// NewFunctionTool creates a Tool from a function, name, description, and JSON Schema.
func NewFunctionTool(name, description string, schema json.RawMessage, fn func(ctx context.Context, input map[string]any) (any, error)) *FunctionTool {
	return &FunctionTool{
		BaseTool: BaseTool{
			ToolName:        name,
			ToolDescription: description,
			ToolSchema:      schema,
		},
		Fn: fn,
	}
}

func (f *FunctionTool) Execute(ctx context.Context, input map[string]any) (*ToolResponse, error) {
	result, err := f.Fn(ctx, input)
	if err != nil {
		return NewErrorResponse(err), nil
	}
	switch v := result.(type) {
	case string:
		return NewTextResponse(v), nil
	case *ToolResponse:
		return v, nil
	default:
		b, err := json.Marshal(result)
		if err != nil {
			return NewErrorResponse(fmt.Errorf("marshal result: %w", err)), nil
		}
		return NewTextResponse(string(b)), nil
	}
}

// --- ToolGroup ---

// ToolGroup is a named collection of tools that can be activated/deactivated.
type ToolGroup struct {
	GroupName    string
	Description  string
	Instructions string
	Skills       []string
	MCPs         []string
	Tools        []Tool
	Active       bool
}

// --- Toolkit ---

// Toolkit manages tool groups and provides schema generation and invocation.
type Toolkit struct {
	groups map[string]*ToolGroup
	mu     sync.RWMutex
}

// NewToolkit creates a Toolkit. If tools are provided, they are placed in the "basic" group (always active).
func NewToolkit(tools ...Tool) *Toolkit {
	tk := &Toolkit{groups: make(map[string]*ToolGroup)}
	if len(tools) > 0 {
		tk.groups["basic"] = &ToolGroup{
			GroupName: "basic",
			Tools:     tools,
			Active:    true,
		}
	}
	return tk
}

// AddGroup adds a named tool group. The group starts active.
func (tk *Toolkit) AddGroup(name string, tools ...Tool) {
	tk.mu.Lock()
	defer tk.mu.Unlock()
	tk.groups[name] = &ToolGroup{
		GroupName: name,
		Tools:     tools,
		Active:    true,
	}
}

// ActivateGroup activates a tool group by name.
func (tk *Toolkit) ActivateGroup(name string) {
	tk.mu.Lock()
	defer tk.mu.Unlock()
	if g, ok := tk.groups[name]; ok {
		g.Active = true
	}
}

// DeactivateGroup deactivates a tool group (it won't appear in schemas or be callable).
func (tk *Toolkit) DeactivateGroup(name string) {
	tk.mu.Lock()
	defer tk.mu.Unlock()
	if g, ok := tk.groups[name]; ok {
		g.Active = false
	}
}

// GetToolSchemas returns ToolSchema for all active tools, suitable for passing to model.WithTools.
func (tk *Toolkit) GetToolSchemas() []model.ToolSchema {
	tk.mu.RLock()
	defer tk.mu.RUnlock()

	var schemas []model.ToolSchema
	for _, g := range tk.groups {
		if !g.Active {
			continue
		}
		for _, t := range g.Tools {
			schema := t.InputSchema()
			if schema == nil {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			schemas = append(schemas, model.ToolSchema{
				Type: "function",
				Function: model.ToolFunction{
					Name:        t.Name(),
					Description: t.Description(),
					Parameters:  schema,
				},
			})
		}
	}
	return schemas
}

// CallTool executes a tool by name with the given input.
// If the tool has a non-empty InputSchema, ValidateInput is called first.
// If the tool's BaseTool has Middlewares, they are applied as an onion chain.
func (tk *Toolkit) CallTool(ctx context.Context, name string, input map[string]any) (*ToolResponse, error) {
	tk.mu.RLock()
	t, groupName := tk.findToolWithGroup(name)
	tk.mu.RUnlock()

	if t == nil {
		if groupName != "" {
			return nil, &agenterrors.ToolGroupInactiveError{ToolName: name, GroupName: groupName}
		}
		return nil, &agenterrors.ToolNotFoundError{ToolName: name}
	}

	// Validate input against schema before execution
	if schema := t.InputSchema(); len(schema) > 0 {
		if err := ValidateInput(schema, input); err != nil {
			return NewErrorResponse(fmt.Errorf("input validation: %w", err)), nil
		}
	}

	mws := getToolMiddlewares(t)
	if len(mws) == 0 {
		return t.Execute(ctx, input)
	}

	// Build onion chain: mws[0] wraps mws[1] wraps ... wraps t.Execute
	var handler ToolHandler = func(ctx context.Context, n string, in map[string]any) (any, error) {
		return t.Execute(ctx, in)
	}
	for i := len(mws) - 1; i >= 0; i-- {
		mw := mws[i]
		next := handler
		handler = func(ctx context.Context, n string, in map[string]any) (any, error) {
			return mw.Wrap(ctx, n, in, next)
		}
	}

	result, err := handler(ctx, name, input)
	if err != nil {
		return nil, err
	}
	if resp, ok := result.(*ToolResponse); ok {
		return resp, nil
	}
	// Wrap raw results
	switch v := result.(type) {
	case string:
		return NewTextResponse(v), nil
	default:
		b, _ := json.Marshal(v)
		return NewTextResponse(string(b)), nil
	}
}

// CallToolFromBlock executes a tool from a ToolCallBlock.
func (tk *Toolkit) CallToolFromBlock(ctx context.Context, block *message.ToolCallBlock) (*ToolResponse, error) {
	input, err := block.ParseInput()
	if err != nil {
		// Try JSON repair before giving up
		var repaired map[string]any
		if repairErr := jsonx.RepairAndUnmarshal([]byte(block.Input), &repaired); repairErr == nil {
			input = repaired
		} else {
			return NewErrorResponse(&agenterrors.ToolJSONDecodeError{
				ToolName: block.Name,
				Input:    block.Input,
				Err:      err,
			}), nil
		}
	}
	return tk.CallTool(ctx, block.Name, input)
}

// Get returns a tool by name from active groups, or nil if not found.
func (tk *Toolkit) Get(name string) Tool {
	tk.mu.RLock()
	defer tk.mu.RUnlock()
	return tk.findTool(name)
}

func (tk *Toolkit) findTool(name string) Tool {
	t, _ := tk.findToolWithGroup(name)
	return t
}

// findToolWithGroup returns the tool and, if the tool exists but its group is
// inactive, the group name (so the caller can produce a specific error).
// It tries an exact name match first, then falls back to case-insensitive
// matching (comparing lowercased names) so that callers using the old
// lowercase names (e.g. "bash") still find the renamed tools (e.g. "Bash").
func (tk *Toolkit) findToolWithGroup(name string) (Tool, string) {
	inactiveGroup := ""
	for _, g := range tk.groups {
		for _, t := range g.Tools {
			if t.Name() == name {
				if g.Active {
					return t, ""
				}
				inactiveGroup = g.GroupName
			}
		}
	}
	if inactiveGroup != "" {
		return nil, inactiveGroup
	}

	// Case-insensitive fallback: compare lowercased names
	lowerName := strings.ToLower(name)
	for _, g := range tk.groups {
		for _, t := range g.Tools {
			if strings.ToLower(t.Name()) == lowerName {
				if g.Active {
					return t, ""
				}
				inactiveGroup = g.GroupName
			}
		}
	}
	return nil, inactiveGroup
}

// CheckToolAvailable returns true if the named tool exists and its group is active.
func (tk *Toolkit) CheckToolAvailable(name string) bool {
	tk.mu.RLock()
	defer tk.mu.RUnlock()
	t, _ := tk.findToolWithGroup(name)
	return t != nil
}

// Clear removes all groups and tools from the toolkit.
func (tk *Toolkit) Clear() {
	tk.mu.Lock()
	defer tk.mu.Unlock()
	tk.groups = make(map[string]*ToolGroup)
}

// GetToolSchemasForGroups returns ToolSchemas only for the specified active groups.
func (tk *Toolkit) GetToolSchemasForGroups(groups ...string) []model.ToolSchema {
	tk.mu.RLock()
	defer tk.mu.RUnlock()

	want := make(map[string]bool, len(groups))
	for _, g := range groups {
		want[g] = true
	}

	var schemas []model.ToolSchema
	for _, g := range tk.groups {
		if !g.Active || !want[g.GroupName] {
			continue
		}
		for _, t := range g.Tools {
			schema := t.InputSchema()
			if schema == nil {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			schemas = append(schemas, model.ToolSchema{
				Type: "function",
				Function: model.ToolFunction{
					Name:        t.Name(),
					Description: t.Description(),
					Parameters:  schema,
				},
			})
		}
	}
	return schemas
}

// AddMiddleware appends a tool-level middleware to the BaseTool.
func (b *BaseTool) AddMiddleware(mw ToolMiddleware) {
	b.Middlewares = append(b.Middlewares, mw)
}

// toolMiddlewarer is implemented by tools that carry their own middleware chain.
type toolMiddlewarer interface {
	GetMiddlewares() []ToolMiddleware
}

func (b *BaseTool) GetMiddlewares() []ToolMiddleware { return b.Middlewares }

func getToolMiddlewares(t Tool) []ToolMiddleware {
	if tm, ok := t.(toolMiddlewarer); ok {
		return tm.GetMiddlewares()
	}
	return nil
}

// --- Global Registry ---

var (
	mu       sync.RWMutex
	registry = map[string]Tool{}
)

// Register registers a tool in the global registry.
func Register(t Tool) error {
	if t == nil || t.Name() == "" {
		return fmt.Errorf("tool: invalid tool")
	}
	mu.Lock()
	defer mu.Unlock()
	registry[t.Name()] = t
	return nil
}

// GetRegistered returns a globally registered tool by name.
func GetRegistered(name string) Tool {
	mu.RLock()
	defer mu.RUnlock()
	return registry[name]
}

// ListRegistered returns the names of all globally registered tools.
func ListRegistered() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

// ValidateInput performs basic type checking of input values against a JSON
// Schema. This is not a full JSON Schema validator; it checks:
// - required fields are present
// - field types match ("string", "number", "integer", "boolean", "array", "object")
//
// Unknown properties are allowed (additionalProperties is not enforced).
// Returns nil if validation passes or the schema cannot be parsed.
func ValidateInput(schema json.RawMessage, input map[string]any) error {
	if len(schema) == 0 || input == nil {
		return nil
	}
	return validateAgainstSchema("input", input, schema)
}

// jsonSchemaNode is the subset of JSON Schema the validator enforces.
type jsonSchemaNode struct {
	Type       string                     `json:"type"`
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
	Items      json.RawMessage            `json:"items"`
	Enum       []any                      `json:"enum"`
	Minimum    *float64                   `json:"minimum"`
	Maximum    *float64                   `json:"maximum"`
	MinLength  *int                       `json:"minLength"`
	MaxLength  *int                       `json:"maxLength"`
	Pattern    string                     `json:"pattern"`
}

// validateAgainstSchema recursively validates val against a JSON Schema node.
// It enforces type, required, enum, string length/pattern, number bounds, nested
// object properties, and array item schemas. An unparseable schema node is
// skipped (permissive), but any explicit constraint that is violated errors.
func validateAgainstSchema(name string, val any, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var s jsonSchemaNode
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil // unparseable schema node: skip
	}
	if val == nil {
		return nil
	}

	if err := checkType(name, val, s.Type); err != nil {
		return err
	}

	if len(s.Enum) > 0 && !enumContains(s.Enum, val) {
		return fmt.Errorf("field %q: value %v is not one of the allowed values", name, val)
	}

	if str, ok := val.(string); ok {
		n := len([]rune(str))
		if s.MinLength != nil && n < *s.MinLength {
			return fmt.Errorf("field %q: length %d is below minLength %d", name, n, *s.MinLength)
		}
		if s.MaxLength != nil && n > *s.MaxLength {
			return fmt.Errorf("field %q: length %d exceeds maxLength %d", name, n, *s.MaxLength)
		}
		if s.Pattern != "" {
			if re, err := regexp.Compile(s.Pattern); err == nil && !re.MatchString(str) {
				return fmt.Errorf("field %q: value does not match pattern %q", name, s.Pattern)
			}
		}
	}

	if f, ok := toFloat(val); ok {
		if s.Minimum != nil && f < *s.Minimum {
			return fmt.Errorf("field %q: value %v is below minimum %v", name, f, *s.Minimum)
		}
		if s.Maximum != nil && f > *s.Maximum {
			return fmt.Errorf("field %q: value %v exceeds maximum %v", name, f, *s.Maximum)
		}
	}

	if obj, ok := val.(map[string]any); ok {
		for _, req := range s.Required {
			if _, present := obj[req]; !present {
				return fmt.Errorf("field %q: missing required field %q", name, req)
			}
		}
		for propName, propSchema := range s.Properties {
			if pv, present := obj[propName]; present {
				if err := validateAgainstSchema(propName, pv, propSchema); err != nil {
					return err
				}
			}
		}
	}

	if len(s.Items) > 0 {
		if arr, ok := val.([]any); ok {
			for i, elem := range arr {
				if err := validateAgainstSchema(fmt.Sprintf("%s[%d]", name, i), elem, s.Items); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// enumContains reports whether val equals one of the enum members (string, bool,
// or numeric equality).
func enumContains(enum []any, val any) bool {
	for _, e := range enum {
		switch ev := e.(type) {
		case string:
			if vs, ok := val.(string); ok && vs == ev {
				return true
			}
		case bool:
			if vb, ok := val.(bool); ok && vb == ev {
				return true
			}
		default:
			if ef, ok := toFloat(e); ok {
				if vf, ok2 := toFloat(val); ok2 && ef == vf {
					return true
				}
			}
		}
	}
	return false
}

// toFloat converts a numeric value (from JSON or Go) to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// checkType validates that val matches the expected JSON Schema type.
func checkType(name string, val any, expectedType string) error {
	if val == nil || expectedType == "" {
		return nil
	}

	switch expectedType {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("field %q: expected string, got %T", name, val)
		}
	case "number":
		switch val.(type) {
		case float64, int, int64, float32:
			// ok
		case json.Number:
			// ok
		default:
			return fmt.Errorf("field %q: expected number, got %T", name, val)
		}
	case "integer":
		switch v := val.(type) {
		case float64:
			if v != float64(int64(v)) {
				return fmt.Errorf("field %q: expected integer, got float %v", name, v)
			}
		case int, int64:
			// ok
		case json.Number:
			if _, err := v.Int64(); err != nil {
				return fmt.Errorf("field %q: expected integer, got %v", name, v)
			}
		default:
			return fmt.Errorf("field %q: expected integer, got %T", name, val)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("field %q: expected boolean, got %T", name, val)
		}
	case "array":
		switch val.(type) {
		case []any, []string, []float64, []int:
			// ok
		default:
			return fmt.Errorf("field %q: expected array, got %T", name, val)
		}
	case "object":
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("field %q: expected object, got %T", name, val)
		}
	}

	return nil
}
