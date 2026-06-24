package app

import (
	"encoding/json"
	"sync"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/messagebus"
)

// SessionProjection mirrors UI cards from one session onto another session's
// event stream. This enables features like team HITL where a worker session's
// permission prompt needs to be visible to the leader session.
type SessionProjection struct {
	bus messagebus.MessageBus
}

// NewSessionProjection creates a new session projection.
func NewSessionProjection(bus messagebus.MessageBus) *SessionProjection {
	return &SessionProjection{bus: bus}
}

// ProjectedEntry is a UI card projected from a source session onto a target.
type ProjectedEntry struct {
	EntryID         string `json:"entry_id"`
	Kind            string `json:"kind"` // e.g., "hitl", "progress", "error"
	SourceSessionID string `json:"source_session_id"`
	Payload         any    `json:"payload"`
}

// Publish projects an entry onto the target session.
func (p *SessionProjection) Publish(targetSessionID string, entry ProjectedEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	evtData, err2 := json.Marshal(map[string]any{
		"type": "custom",
		"name": "session_projection." + entry.Kind,
		"data": json.RawMessage(data),
	})
	if err2 != nil {
		return err2
	}
	return p.bus.Publish(nil, "session:"+targetSessionID+":events", evtData)
}

// Set stores a projected entry in the target session's projection feed.
func (p *SessionProjection) Set(targetSessionID, kind, entryID string, payload any) error {
	entry := ProjectedEntry{
		EntryID: entryID,
		Kind:    kind,
		Payload: payload,
	}
	return p.Publish(targetSessionID, entry)
}

// Remove removes a projected entry from the target session's feed.
func (p *SessionProjection) Remove(targetSessionID, kind, entryID string) error {
	entry := ProjectedEntry{
		EntryID: entryID,
		Kind:    kind,
		Payload: nil, // nil payload signals removal
	}
	return p.Publish(targetSessionID, entry)
}

// SubagentHitlProjector projects HITL permission requests from sub-agent
// (worker) sessions onto the parent (leader) session. This allows a client
// watching the leader session to see and respond to worker permission prompts.
type SubagentHitlProjector struct {
	projection *SessionProjection
	mu         sync.RWMutex
	mappings   map[string]string // workerSessionID -> leaderSessionID
}

// NewSubagentHitlProjector creates a projector.
func NewSubagentHitlProjector(projection *SessionProjection) *SubagentHitlProjector {
	return &SubagentHitlProjector{
		projection: projection,
		mappings:   make(map[string]string),
	}
}

// Register maps a worker session to its leader session.
func (p *SubagentHitlProjector) Register(workerSessionID, leaderSessionID string) {
	p.mu.Lock()
	p.mappings[workerSessionID] = leaderSessionID
	p.mu.Unlock()
}

// Unregister removes a worker-to-leader mapping.
func (p *SubagentHitlProjector) Unregister(workerSessionID string) {
	p.mu.Lock()
	delete(p.mappings, workerSessionID)
	p.mu.Unlock()
}

// ProjectConfirmRequest projects a RequireUserConfirmEvent from a worker
// onto the leader session so the leader's client can respond.
func (p *SubagentHitlProjector) ProjectConfirmRequest(workerSessionID string, evt event.RequireUserConfirmEvent) error {
	p.mu.RLock()
	leaderID, ok := p.mappings[workerSessionID]
	p.mu.RUnlock()
	if !ok {
		return nil // no mapping — worker is standalone
	}

	return p.projection.Set(leaderID, "hitl", evt.GetEventID(), map[string]any{
		"type":              "require_user_confirm",
		"worker_session_id": workerSessionID,
		"reply_id":          evt.ReplyID,
		"tool_calls":        evt.ToolCalls,
	})
}

// ResolveConfirmRequest removes a projected HITL card after it's resolved.
func (p *SubagentHitlProjector) ResolveConfirmRequest(workerSessionID, eventID string) error {
	p.mu.RLock()
	leaderID, ok := p.mappings[workerSessionID]
	p.mu.RUnlock()
	if !ok {
		return nil
	}
	return p.projection.Remove(leaderID, "hitl", eventID)
}
