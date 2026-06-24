package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

// agentStateJSON is the JSON-friendly representation of AgentState.
type agentStateJSON struct {
	SessionID string         `json:"session_id"`
	Context   []*message.Msg `json:"context"`
	Summary   string         `json:"summary,omitempty"`
	ReplyID   string         `json:"reply_id"`
	CurIter   int            `json:"cur_iter"`
}

// MarshalJSON serializes AgentState to JSON.
func (s *AgentState) MarshalJSON() ([]byte, error) {
	return json.Marshal(agentStateJSON{
		SessionID: s.SessionID,
		Context:   s.Context,
		Summary:   s.Summary,
		ReplyID:   s.ReplyID,
		CurIter:   s.CurIter,
	})
}

// UnmarshalJSON deserializes AgentState from JSON.
func (s *AgentState) UnmarshalJSON(data []byte) error {
	var raw agentStateJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal agent state: %w", err)
	}
	s.SessionID = raw.SessionID
	s.Context = raw.Context
	s.Summary = raw.Summary
	s.ReplyID = raw.ReplyID
	s.CurIter = raw.CurIter
	return nil
}

// StateSaver persists and retrieves agent states.
// Concrete implementations (file, Redis, etc.) are planned for Phase 4.
type StateSaver interface {
	SaveState(ctx context.Context, sessionID string, state *AgentState) error
	LoadState(ctx context.Context, sessionID string) (*AgentState, error)
	ListSessions(ctx context.Context) ([]SessionInfo, error)
	DeleteSession(ctx context.Context, sessionID string) error
}

// SessionInfo summarizes a persisted session.
type SessionInfo struct {
	SessionID string `json:"session_id"`
	AgentName string `json:"agent_name,omitempty"`
	Summary   string `json:"summary,omitempty"`
}
