package middleware

import (
	"context"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

// ReplyHandler processes a reply request and returns an event stream.
type ReplyHandler func(ctx context.Context, input ReplyInput) <-chan event.Event

// ModelCallHandler calls the model and returns a response.
type ModelCallHandler func(ctx context.Context, input ModelCallInput) (*model.ChatResponse, error)

// ActingHandler executes a tool call and returns the result.
type ActingHandler func(ctx context.Context, input ActingInput) (*tool.ToolResponse, error)

// CompressHandler performs context compression.
type CompressHandler func(ctx context.Context, input CompressInput) error

// ReplyInput is passed to OnReply hooks.
type ReplyInput struct {
	AgentName string
	UserInput string
	Messages  []*message.Msg
}

// ModelCallInput is passed to OnModelCall hooks.
type ModelCallInput struct {
	AgentName  string
	Messages   []*message.Msg
	Tools      []model.ToolSchema
	ToolChoice *model.ToolChoice
}

// ActingInput is passed to OnActing hooks.
type ActingInput struct {
	AgentName string
	ToolCall  message.ToolCallBlock
}

// CompressInput is passed to OnCompressContext hooks.
type CompressInput struct {
	AgentName    string
	TriggerRatio float64
	ReserveRatio float64
}

// Middleware defines the hooks that can intercept agent behavior.
// Implement only the hooks you need; embed BaseMiddleware for pass-through defaults.
type Middleware interface {
	// OnReply wraps the entire reply lifecycle (outermost hook).
	OnReply(ctx context.Context, input ReplyInput, next ReplyHandler) <-chan event.Event

	// OnModelCall wraps each model API call within the ReAct loop.
	OnModelCall(ctx context.Context, input ModelCallInput, next ModelCallHandler) (*model.ChatResponse, error)

	// OnActing wraps each tool execution.
	OnActing(ctx context.Context, input ActingInput, next ActingHandler) (*tool.ToolResponse, error)

	// OnSystemPrompt transforms the system prompt (pipeline mode, not onion).
	// Each middleware receives the output of the previous one.
	OnSystemPrompt(ctx context.Context, agentName string, currentPrompt string) string

	// OnCompressContext wraps context compression.
	OnCompressContext(ctx context.Context, input CompressInput, next CompressHandler) error

	// Key returns a unique identifier for this middleware instance.
	// Used to namespace middleware state in MiddleContext.
	Key() string
}

// BaseMiddleware provides pass-through implementations for all hooks.
// Embed this in concrete middleware structs and override only the hooks you need.
type BaseMiddleware struct {
	MiddlewareKey string
}

func (b *BaseMiddleware) Key() string { return b.MiddlewareKey }

func (b *BaseMiddleware) OnReply(ctx context.Context, input ReplyInput, next ReplyHandler) <-chan event.Event {
	return next(ctx, input)
}

func (b *BaseMiddleware) OnModelCall(ctx context.Context, input ModelCallInput, next ModelCallHandler) (*model.ChatResponse, error) {
	return next(ctx, input)
}

func (b *BaseMiddleware) OnActing(ctx context.Context, input ActingInput, next ActingHandler) (*tool.ToolResponse, error) {
	return next(ctx, input)
}

func (b *BaseMiddleware) OnSystemPrompt(_ context.Context, _ string, currentPrompt string) string {
	return currentPrompt
}

func (b *BaseMiddleware) OnCompressContext(ctx context.Context, input CompressInput, next CompressHandler) error {
	return next(ctx, input)
}
