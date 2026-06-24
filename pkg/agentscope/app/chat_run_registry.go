package app

import "sync"

// ChatRunRegistry tracks active chat runs to prevent duplicate concurrent
// chats for the same session. Each session can have at most one active chat.
type ChatRunRegistry struct {
	mu      sync.Mutex
	running map[string]bool // sessionID -> true if chat is active
}

// NewChatRunRegistry creates a new chat run registry.
func NewChatRunRegistry() *ChatRunRegistry {
	return &ChatRunRegistry{
		running: make(map[string]bool),
	}
}

// TryAcquire attempts to start a chat run for a session.
// Returns true if acquired, false if a chat is already running.
func (r *ChatRunRegistry) TryAcquire(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running[sessionID] {
		return false
	}
	r.running[sessionID] = true
	return true
}

// Release ends the chat run for a session.
func (r *ChatRunRegistry) Release(sessionID string) {
	r.mu.Lock()
	delete(r.running, sessionID)
	r.mu.Unlock()
}

// IsRunning returns whether a chat is active for a session.
func (r *ChatRunRegistry) IsRunning(sessionID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running[sessionID]
}
