package model

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

// ChatModel is the unified interface for all chat models.
type ChatModel interface {
	// Chat generates a reply given conversation history.
	Chat(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (*ChatResponse, error)

	// ChatStream returns a channel of streaming ChatResponse chunks.
	// The final chunk has IsLast=true.
	// If a model does not support streaming, it should return (nil, ErrStreamNotSupported).
	ChatStream(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (<-chan ChatResponse, error)

	// CountTokens estimates the token count for messages and optional tool schemas.
	CountTokens(msgs []*message.Msg, tools []ToolSchema) int
}

// ChatResponse represents a model response, supporting both streaming and non-streaming.
type ChatResponse struct {
	Content   []message.ContentBlock `json:"content"`
	IsLast    bool                   `json:"is_last"`
	ID        string                 `json:"id"`
	CreatedAt string                 `json:"created_at"`
	Usage     *ChatUsage             `json:"usage,omitempty"`
	Metadata  map[string]any         `json:"metadata,omitempty"`
	ModelName string                 `json:"model_name,omitempty"`
}

// GetTextContent concatenates all TextBlock content from the response.
func (r *ChatResponse) GetTextContent() string {
	var text string
	for _, b := range r.Content {
		if tb, ok := b.(message.TextBlock); ok {
			text += tb.Text
		}
	}
	return text
}

// Deprecated: Msg field on ChatResponse. Use Content field instead.
// ToMsg converts a ChatResponse into a Msg for backward compatibility.
func (r *ChatResponse) ToMsg(name string) *message.Msg {
	msg := message.AssistantMsg(name, r.Content)
	msg.ID = r.ID
	if r.Usage != nil {
		msg.Usage = &message.Usage{
			InputTokens:              r.Usage.InputTokens,
			OutputTokens:             r.Usage.OutputTokens,
			CacheCreationInputTokens: r.Usage.CacheCreationInputTokens,
			CacheInputTokens:         r.Usage.CacheInputTokens,
		}
	}
	return msg
}

// ChatUsage tracks token consumption for a model call.
type ChatUsage struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	Time                     float64 `json:"time,omitempty"` // seconds
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens,omitempty"`
	CacheInputTokens         int     `json:"cache_input_tokens,omitempty"`
}

// ToolSchema defines a tool for model API function calling.
type ToolSchema struct {
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a callable function for the model.
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// ToolChoice controls how the model selects tools.
type ToolChoice struct {
	Mode  string   `json:"mode"`            // "auto", "none", "required", or specific tool name
	Tools []string `json:"tools,omitempty"` // optional whitelist filter
}

// ContextSizer is an optional interface that ChatModel implementations can
// provide to report the model's context window size in tokens.
// Used by context compression to determine when compression is needed.
type ContextSizer interface {
	ContextSize() int
}

// ModelNamer is an optional interface that ChatModel implementations can
// provide to report the model name used for model card lookups.
type ModelNamer interface {
	ModelName() string
}

// ResolveContextSize returns the context window size for a model.
// It checks (in order): ContextSizer interface, ModelNamer + model card lookup, default.
func ResolveContextSize(m ChatModel, fallback int) int {
	if cs, ok := m.(ContextSizer); ok {
		if s := cs.ContextSize(); s > 0 {
			return s
		}
	}
	if mn, ok := m.(ModelNamer); ok {
		if card, err := GetModelCard(mn.ModelName()); err == nil && card.ContextSize > 0 {
			return card.ContextSize
		}
	}
	return fallback
}

// ErrStreamNotSupported indicates that a model does not support streaming calls.
var ErrStreamNotSupported = fmt.Errorf("chat model: stream not supported")

// CallOptions stores model call options configured via functional options.
type CallOptions struct {
	Temperature     *float64
	MaxTokens       *int
	TopP            *float64
	Tools           []ToolSchema
	ToolChoice      *ToolChoice
	ThinkingEnable  *bool
	ThinkingBudget  *int
	ReasoningEffort *string // "low", "medium", "high"
	Voice           *string // audio output voice (e.g. "alloy"); enables audio modality
	MaxRetries      int
	RetryDelay      time.Duration
}

// CallOption mutates CallOptions.
type CallOption func(*CallOptions)

func WithTemperature(t float64) CallOption {
	return func(o *CallOptions) {
		o.Temperature = &t
	}
}

func WithMaxTokens(n int) CallOption {
	return func(o *CallOptions) {
		o.MaxTokens = &n
	}
}

func WithTopP(p float64) CallOption {
	return func(o *CallOptions) {
		o.TopP = &p
	}
}

// WithTools sets the tool schemas for native function calling.
func WithTools(tools []ToolSchema) CallOption {
	return func(o *CallOptions) {
		o.Tools = tools
	}
}

// WithToolChoice sets the tool choice mode.
func WithToolChoice(tc *ToolChoice) CallOption {
	return func(o *CallOptions) {
		o.ToolChoice = tc
	}
}

// WithThinking enables extended thinking with an optional token budget.
func WithThinking(enable bool, budget int) CallOption {
	return func(o *CallOptions) {
		o.ThinkingEnable = &enable
		if budget > 0 {
			o.ThinkingBudget = &budget
		}
	}
}

// WithReasoningEffort sets the reasoning effort level (e.g. "low", "medium", "high").
func WithReasoningEffort(effort string) CallOption {
	return func(o *CallOptions) {
		o.ReasoningEffort = &effort
	}
}

// WithVoice enables audio output with the specified voice (e.g. "alloy", "coral").
// When set, the model request includes audio modality and PCM16 format.
func WithVoice(voice string) CallOption {
	return func(o *CallOptions) {
		o.Voice = &voice
	}
}

// WithRetries configures retry behavior for transient errors.
func WithRetries(maxRetries int, delay time.Duration) CallOption {
	return func(o *CallOptions) {
		o.MaxRetries = maxRetries
		o.RetryDelay = delay
	}
}

// ValidateToolChoice validates that a tool choice references valid tools.
func ValidateToolChoice(tc *ToolChoice, tools []ToolSchema) error {
	if tc == nil {
		return nil
	}
	switch tc.Mode {
	case "auto", "none", "required", "":
		return nil
	default:
		for _, t := range tools {
			if t.Function.Name == tc.Mode {
				return nil
			}
		}
		return fmt.Errorf("model: tool_choice references unknown tool %q", tc.Mode)
	}
}

// IsRetryableError checks if an error is a transient error worth retrying.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	for _, pattern := range []string{"429", "rate limit", "timeout", "connection reset", "connection refused", "500", "502", "503", "overloaded"} {
		if contains(s, pattern) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsLower(s, substr))
}

func containsLower(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Deprecated: ChatStream interface. Use the <-chan ChatResponse return from ChatModel.ChatStream instead.
type ChatStream interface {
	Recv() (*message.Msg, error)
	Close() error
}

// countTokensByBytes estimates tokens from byte length across all block types.
func countTokensByBytes(msgs []*message.Msg, tools []ToolSchema) int {
	total := 0
	for _, m := range msgs {
		if m == nil {
			continue
		}
		for _, b := range m.Content {
			switch blk := b.(type) {
			case message.TextBlock:
				total += len(blk.Text)
			case message.ThinkingBlock:
				total += len(blk.Thinking)
			case message.ToolCallBlock:
				total += len(blk.Input)
			case message.ToolResultBlock:
				total += len(blk.GetOutputText())
			case message.HintBlock:
				total += len(blk.GetHintText())
			case message.DataBlock:
				if src, ok := blk.Source.(message.Base64Source); ok {
					total += len(src.Data) * 3 / 4 // base64 → raw bytes
				} else if src, ok := blk.Source.(message.URLSource); ok {
					total += len(src.URL)
				}
			}
		}
	}
	if len(tools) > 0 {
		b, _ := json.Marshal(tools)
		total += len(b)
	}
	return total / 4
}
