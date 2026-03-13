package model

import (
	"context"
	"fmt"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

// ChatModel is the unified interface for all chat models, abstracting multi-turn conversations.
type ChatModel interface {
	// Chat generates a reply given conversation history.
	Chat(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (*ChatResponse, error)

	// ChatStream provides a streaming output interface (optional).
	// If a model does not support streaming, it should return (nil, ErrStreamNotSupported).
	ChatStream(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (ChatStream, error)
}

// ChatResponse is the Go-style counterpart to the Python ChatResponse.
type ChatResponse struct {
	Msg       *message.Msg `json:"msg"`
	Raw       any          `json:"raw,omitempty"` // Provider-specific raw payload (e.g. JSON map).
	ModelName string       `json:"model_name,omitempty"`

	// Additional fields like usage or latency can be added as needed.
	CreatedAt time.Time `json:"created_at"`
}

// ChatStream represents a streaming model response.
type ChatStream interface {
	Recv() (*message.Msg, error)
	Close() error
}

// ErrStreamNotSupported indicates that a model does not support streaming calls.
var ErrStreamNotSupported = fmt.Errorf("chat model: stream not supported")

// CallOptions stores model call options configured via functional options.
type CallOptions struct {
	Temperature *float32
	MaxTokens   *int
	TopP        *float32
	// Reserved: tool calling, system prompt, etc.
}

// CallOption mutates CallOptions.
type CallOption func(*CallOptions)

func WithTemperature(t float32) CallOption {
	return func(o *CallOptions) {
		o.Temperature = &t
	}
}

func WithMaxTokens(n int) CallOption {
	return func(o *CallOptions) {
		o.MaxTokens = &n
	}
}

func WithTopP(p float32) CallOption {
	return func(o *CallOptions) {
		o.TopP = &p
	}
}

