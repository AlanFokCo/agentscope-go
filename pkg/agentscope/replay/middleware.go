package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/middleware"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
)

// Middleware records or replays model calls.
type Middleware struct {
	middleware.BaseMiddleware
	mode   Mode
	tape   *Tape
	mu     sync.Mutex
	cursor int // current position in replay mode
}

// NewRecorder creates a middleware that records all model calls to a tape.
func NewRecorder() *Middleware {
	return &Middleware{
		BaseMiddleware: middleware.BaseMiddleware{MiddlewareKey: "replay-recorder"},
		mode:           ModeRecord,
		tape:           NewTape(),
	}
}

// NewReplayer creates a middleware that replays responses from a pre-recorded tape.
func NewReplayer(tape *Tape) *Middleware {
	return &Middleware{
		BaseMiddleware: middleware.BaseMiddleware{MiddlewareKey: "replay-replayer"},
		mode:           ModeReplay,
		tape:           tape,
	}
}

// Tape returns the underlying tape (useful for retrieving recorded data).
func (m *Middleware) Tape() *Tape {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tape
}

// OnModelCall intercepts model calls for recording or replaying.
func (m *Middleware) OnModelCall(ctx context.Context, input *middleware.ModelCallInput, next middleware.ModelCallHandler) (*model.ChatResponse, error) {
	switch m.mode {
	case ModeRecord:
		return m.record(ctx, input, next)
	case ModeReplay:
		return m.replay(ctx, input)
	default:
		return next(ctx, input)
	}
}

func (m *Middleware) record(ctx context.Context, input *middleware.ModelCallInput, next middleware.ModelCallHandler) (*model.ChatResponse, error) {
	// Serialize request data
	messagesJSON, _ := json.Marshal(input.Messages)
	var toolsJSON json.RawMessage
	if len(input.Tools) > 0 {
		toolsJSON, _ = json.Marshal(input.Tools)
	}

	start := time.Now()
	resp, err := next(ctx, input)
	duration := time.Since(start)

	// Build entry
	entry := Entry{
		AgentName:  input.AgentName,
		ModelName:  input.ModelName,
		Messages:   messagesJSON,
		Tools:      toolsJSON,
		Timestamp:  start,
		DurationMs: duration.Milliseconds(),
	}

	if err != nil {
		entry.Error = err.Error()
	}
	if resp != nil {
		entry.Response, _ = json.Marshal(resp)
	}

	// Append to tape
	m.mu.Lock()
	entry.Index = len(m.tape.Entries)
	m.tape.Entries = append(m.tape.Entries, entry)
	m.mu.Unlock()

	return resp, err
}

func (m *Middleware) replay(_ context.Context, _ *middleware.ModelCallInput) (*model.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cursor >= len(m.tape.Entries) {
		return nil, fmt.Errorf("replay: tape exhausted at index %d (tape has %d entries)", m.cursor, len(m.tape.Entries))
	}

	entry := m.tape.Entries[m.cursor]
	m.cursor++

	// Replay error if recorded
	if entry.Error != "" {
		if len(entry.Response) > 0 {
			resp, unmarshalErr := unmarshalChatResponse(entry.Response)
			if unmarshalErr == nil {
				return resp, fmt.Errorf("%s", entry.Error)
			}
		}
		return nil, fmt.Errorf("%s", entry.Error)
	}

	// Deserialize response
	if len(entry.Response) == 0 {
		return nil, nil
	}
	resp, err := unmarshalChatResponse(entry.Response)
	if err != nil {
		return nil, fmt.Errorf("replay: unmarshal response at index %d: %w", entry.Index, err)
	}
	return resp, nil
}

// unmarshalChatResponse handles the polymorphic Content field in ChatResponse.
func unmarshalChatResponse(data json.RawMessage) (*model.ChatResponse, error) {
	// First unmarshal everything except Content
	var raw struct {
		Content    json.RawMessage  `json:"content"`
		IsLast     bool             `json:"is_last"`
		ID         string           `json:"id"`
		CreatedAt  string           `json:"created_at"`
		Usage      *model.ChatUsage `json:"usage,omitempty"`
		Metadata   map[string]any   `json:"metadata,omitempty"`
		ModelName  string           `json:"model_name,omitempty"`
		StopReason string           `json:"stop_reason,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	resp := &model.ChatResponse{
		IsLast:     raw.IsLast,
		ID:         raw.ID,
		CreatedAt:  raw.CreatedAt,
		Usage:      raw.Usage,
		Metadata:   raw.Metadata,
		ModelName:  raw.ModelName,
		StopReason: raw.StopReason,
	}

	// Unmarshal content blocks using the message package helper
	if len(raw.Content) > 0 && string(raw.Content) != "null" && string(raw.Content) != "[]" {
		blocks, err := message.UnmarshalContentBlocks(raw.Content)
		if err != nil {
			return nil, fmt.Errorf("unmarshal content blocks: %w", err)
		}
		resp.Content = blocks
	}

	return resp, nil
}
