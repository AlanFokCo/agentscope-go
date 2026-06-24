package model

import (
	"context"
	"errors"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/sirupsen/logrus"
)

// FallbackChatModel wraps a primary ChatModel with automatic failover to a
// fallback model.  If the primary model fails (for non-cancellation errors),
// the call is retried on the fallback.
type FallbackChatModel struct {
	Primary    ChatModel
	Fallback   ChatModel
	MaxRetries int // retries on primary before trying fallback; 0 means try once
}

// NewFallbackChatModel creates a FallbackChatModel.
func NewFallbackChatModel(primary, fallback ChatModel) *FallbackChatModel {
	return &FallbackChatModel{
		Primary:  primary,
		Fallback: fallback,
	}
}

func (f *FallbackChatModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (*ChatResponse, error) {
	retries := f.MaxRetries + 1
	var lastErr error
	for i := 0; i < retries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * time.Second)
		}
		resp, err := f.Primary.Chat(ctx, msgs, opts...)
		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		lastErr = err
	}

	logrus.WithError(lastErr).Warn("primary model failed, falling back")
	return f.Fallback.Chat(ctx, msgs, opts...)
}

func (f *FallbackChatModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (<-chan ChatResponse, error) {
	ch, err := f.Primary.ChatStream(ctx, msgs, opts...)
	if err == nil {
		return ch, nil
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	logrus.WithError(err).Warn("primary model stream failed, falling back")
	return f.Fallback.ChatStream(ctx, msgs, opts...)
}

func (f *FallbackChatModel) CountTokens(msgs []*message.Msg, tools []ToolSchema) int {
	return f.Primary.CountTokens(msgs, tools)
}

// ContextSize delegates to the primary model if it implements ContextSizer.
func (f *FallbackChatModel) ContextSize() int {
	if cs, ok := f.Primary.(ContextSizer); ok {
		return cs.ContextSize()
	}
	return 0
}

// ModelName delegates to the primary model if it implements ModelNamer.
func (f *FallbackChatModel) ModelName() string {
	if mn, ok := f.Primary.(ModelNamer); ok {
		return mn.ModelName()
	}
	return ""
}
