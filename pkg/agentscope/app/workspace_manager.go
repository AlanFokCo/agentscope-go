package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/workspace"
)

// WorkspaceManager manages workspace lifecycle with session↔workspace
// bindings (Python #1951 sharing semantics). Every session binds to exactly
// one workspace; a workspace's refcount is the number of sessions bound to
// it. By default each session gets a private workspace named after itself;
// Share rebinds a session onto a named workspace other sessions can join.
// When the last session unbinds, the workspace is released from memory
// (files stay on disk; the next access recreates it over the same dir).
type WorkspaceManager struct {
	mu         sync.RWMutex
	workspaces map[string]workspace.Workspace // workspaceID -> workspace
	refs       map[string]int                 // workspaceID -> bound session count
	bindings   map[string]string              // sessionID -> workspaceID
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
		refs:       make(map[string]int),
		bindings:   make(map[string]string),
		baseDir:    baseDir,
		backend:    backend,
	}
}

// errInvalidWorkspaceID distinguishes bad input from workspace creation
// failures (the routes map it to 400 vs 500).
var errInvalidWorkspaceID = errors.New("invalid workspace id")

// validWorkspaceID mirrors the skill partition guard: leading dots collide
// with hidden entries and ./.., separators are the escape.
func validWorkspaceID(id string) bool {
	if id == "" || strings.HasPrefix(id, ".") {
		return false
	}
	return !strings.Contains(id, "/") && !strings.Contains(id, "\\")
}

// GetOrCreate returns the workspace a session is bound to, binding it to a
// private workspace named after itself on first sight.
func (m *WorkspaceManager) GetOrCreate(sessionID string) (workspace.Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id, bound := m.bindings[sessionID]; bound {
		if ws, ok := m.workspaces[id]; ok {
			return ws, nil
		}
	}
	ws, err := m.createLocked(sessionID)
	if err != nil {
		return nil, err
	}
	m.bindings[sessionID] = sessionID
	m.refs[sessionID]++
	return ws, nil
}

// Share binds a session to a named workspace that multiple sessions can
// share, creating it if needed. The session's previous binding (if any) is
// released first. Files of a shared workspace live at <baseDir>/<workspaceID>.
func (m *WorkspaceManager) Share(sessionID, workspaceID string) (workspace.Workspace, error) {
	if !validWorkspaceID(workspaceID) {
		return nil, fmt.Errorf("%w %q", errInvalidWorkspaceID, workspaceID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unbindLocked(sessionID)
	ws, ok := m.workspaces[workspaceID]
	if !ok {
		var err error
		ws, err = m.createLocked(workspaceID)
		if err != nil {
			return nil, err
		}
	}
	m.bindings[sessionID] = workspaceID
	m.refs[workspaceID]++
	return ws, nil
}

// BoundWorkspaceID returns the workspace a session is bound to, if any.
func (m *WorkspaceManager) BoundWorkspaceID(sessionID string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.bindings[sessionID]
	return id, ok
}

// GetByID returns a live workspace by its ID, or nil when it is not
// currently held (never referenced, or released after its last session
// unbound).
func (m *WorkspaceManager) GetByID(workspaceID string) workspace.Workspace {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.workspaces[workspaceID]
}

// RefCount returns how many sessions are bound to a live workspace.
func (m *WorkspaceManager) RefCount(workspaceID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.refs[workspaceID]
}

// Get returns the workspace for a session, or nil if not created.
func (m *WorkspaceManager) Get(sessionID string) workspace.Workspace {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if id, bound := m.bindings[sessionID]; bound {
		return m.workspaces[id]
	}
	return nil
}

// Remove unbinds a session from its workspace, releasing the workspace
// from memory when no session is bound to it anymore.
func (m *WorkspaceManager) Remove(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unbindLocked(sessionID)
}

func (m *WorkspaceManager) unbindLocked(sessionID string) {
	id, bound := m.bindings[sessionID]
	if !bound {
		return
	}
	delete(m.bindings, sessionID)
	m.refs[id]--
	if m.refs[id] <= 0 {
		delete(m.refs, id)
		delete(m.workspaces, id)
	}
}

// createLocked builds (or returns) the workspace for a workspace ID and
// stores it. Callers hold m.mu.
func (m *WorkspaceManager) createLocked(workspaceID string) (workspace.Workspace, error) {
	if ws, ok := m.workspaces[workspaceID]; ok {
		return ws, nil
	}
	wsDir := filepath.Join(m.baseDir, workspaceID)
	switch m.backend {
	case "local":
		lw, err := workspace.NewLocalWorkspace(workspace.LocalConfig{BasePath: wsDir})
		if err != nil {
			return nil, fmt.Errorf("create workspace %s: %w", workspaceID, err)
		}
		m.workspaces[workspaceID] = lw
		return lw, nil
	default:
		return nil, fmt.Errorf("unsupported workspace backend: %s", m.backend)
	}
}

// List returns info about all managed workspaces.
func (m *WorkspaceManager) List() []WorkspaceInfoResponse {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]WorkspaceInfoResponse, 0, len(m.workspaces))
	for id, ws := range m.workspaces {
		result = append(result, WorkspaceInfoResponse{
			SessionID: id, // the workspace ID (private workspaces are named after their session)
			BasePath:  ws.BasePath(),
			Type:      m.backend,
		})
	}
	return result
}
