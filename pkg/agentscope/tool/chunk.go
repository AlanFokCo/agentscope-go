package tool

import (
	"context"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

// ToolChunk represents a piece of streaming output from a tool execution.
type ToolChunk struct {
	// Content contains the content blocks for this chunk.
	Content []message.ContentBlock

	// Metadata holds per-chunk metadata (e.g. progress info).
	Metadata map[string]any

	// IsFinal marks this as the last chunk in the stream.
	IsFinal bool

	// State indicates the tool result state (set on the final chunk).
	State message.ToolResultState
}

// StreamingTool is an optional interface that tools can implement to support
// streaming responses. Tools that implement this interface can yield partial
// results as they become available.
type StreamingTool interface {
	Tool

	// ExecuteStream starts a streaming execution and returns a channel of
	// ToolChunks. The channel is closed when the execution completes.
	// The final chunk should have IsFinal=true and State set.
	ExecuteStream(ctx context.Context, input map[string]any) (<-chan ToolChunk, error)
}

// WrapNonStreamingTool adapts a non-streaming Tool into a StreamingTool by
// calling Execute once and returning the result as a single final chunk.
func WrapNonStreamingTool(t Tool) StreamingTool {
	return &nonStreamingAdapter{Tool: t}
}

type nonStreamingAdapter struct {
	Tool
}

func (a *nonStreamingAdapter) ExecuteStream(ctx context.Context, input map[string]any) (<-chan ToolChunk, error) {
	ch := make(chan ToolChunk, 1)
	go func() {
		defer close(ch)
		resp, err := a.Tool.Execute(ctx, input)
		if err != nil {
			ch <- ToolChunk{
				Content: []message.ContentBlock{message.TextBlock{
					Type: "text",
					Text: err.Error(),
				}},
				IsFinal: true,
				State:   message.ToolResultError,
			}
			return
		}
		ch <- ToolChunk{
			Content:  resp.Content,
			Metadata: resp.Metadata,
			IsFinal:  true,
			State:    resp.State,
		}
	}()
	return ch, nil
}

// IsStreamingTool reports whether a Tool also implements StreamingTool.
func IsStreamingTool(t Tool) bool {
	_, ok := t.(StreamingTool)
	return ok
}

// CollectStream reads all chunks from a streaming tool execution and assembles
// them into a single ToolResponse. Useful when callers don't need streaming.
func CollectStream(ch <-chan ToolChunk) *ToolResponse {
	var allContent []message.ContentBlock
	var lastMeta map[string]any
	var lastState message.ToolResultState

	for chunk := range ch {
		allContent = append(allContent, chunk.Content...)
		if chunk.Metadata != nil {
			lastMeta = chunk.Metadata
		}
		if chunk.IsFinal {
			lastState = chunk.State
		}
	}

	if lastState == "" {
		lastState = message.ToolResultSuccess
	}

	return &ToolResponse{
		Content:  allContent,
		State:    lastState,
		Metadata: lastMeta,
	}
}
