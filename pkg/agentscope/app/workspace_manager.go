package app

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/workspace"
)

// WorkspaceManager manages per-session workspace lifecycle.
type WorkspaceManager struct {
	mu         sync.RWMutex
	workspaces map[string]workspace.Workspace // sessionID -> workspace
	baseDir    string
	backend    string // "local", "docker", "e2b"
}

// NewWorkspaceManager creates a workspace manager with the given base directory.
func NewWorkspaceManager(baseDir, backend string) *WorkspaceManager {
	if backend == "" {
		backend = "local"
	}
	return &WorkspaceManager{
		workspaces: make(map[string]workspace.Workspace),
		baseDir:    baseDir,
		backend:    backend,
	}
}

// GetOrCreate returns the workspace for a session, creating it if needed.
func (m *WorkspaceManager) GetOrCreate(sessionID string) (workspace.Workspace, error) {
	m.mu.RLock()
	ws, ok := m.workspaces[sessionID]
	m.mu.RUnlock()
	if ok {
		return ws, nil
	}

	sessionDir := filepath.Join(m.baseDir, sessionID)
	switch m.backend {
	case "local":
		lw, err := workspace.NewLocalWorkspace(workspace.LocalConfig{BasePath: sessionDir})
		if err != nil {
			return nil, fmt.Errorf("create workspace for session %s: %w", sessionID, err)
		}
		ws = lw
	default:
		return nil, fmt.Errorf("unsupported workspace backend: %s", m.backend)
	}

	m.mu.Lock()
	m.workspaces[sessionID] = ws
	m.mu.Unlock()
	return ws, nil
}

// Get returns the workspace for a session, or nil if not created.
func (m *WorkspaceManager) Get(sessionID string) workspace.Workspace {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.workspaces[sessionID]
}

// Remove removes and cleans up the workspace for a session.
func (m *WorkspaceManager) Remove(sessionID string) {
	m.mu.Lock()
	delete(m.workspaces, sessionID)
	m.mu.Unlock()
}

// List returns info about all managed workspaces.
func (m *WorkspaceManager) List() []WorkspaceInfoResponse {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]WorkspaceInfoResponse, 0, len(m.workspaces))
	for id, ws := range m.workspaces {
		result = append(result, WorkspaceInfoResponse{
			SessionID: id,
			BasePath:  ws.BasePath(),
			Type:      m.backend,
		})
	}
	return result
}
