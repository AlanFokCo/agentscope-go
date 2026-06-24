package model

import (
	"context"
	"encoding/json"
	"fmt"

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
			InputTokens:  r.Usage.InputTokens,
			OutputTokens: r.Usage.OutputTokens,
		}
	}
	return msg
}

// ChatUsage tracks token consumption for a model call.
type ChatUsage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Time         float64 `json:"time,omitempty"` // seconds
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
	Temperature *float64
	MaxTokens   *int
	TopP        *float64
	Tools       []ToolSchema
	ToolChoice  *ToolChoice
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

// Deprecated: ChatStream interface. Use the <-chan ChatResponse return from ChatModel.ChatStream instead.
type ChatStream interface {
	Recv() (*message.Msg, error)
	Close() error
}

// countTokensByBytes estimates tokens from byte length (len/4).
func countTokensByBytes(msgs []*message.Msg, tools []ToolSchema) int {
	total := 0
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if t := m.GetTextContent("\n"); t != nil {
			total += len(*t)
		}
	}
	if len(tools) > 0 {
		b, _ := json.Marshal(tools)
		total += len(b)
	}
	return total / 4
}
