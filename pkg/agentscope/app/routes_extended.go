package app

import (
	"encoding/json"
	"net/http"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/storage"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tts"
	"github.com/google/uuid"
)

// registerExtendedRoutes adds CRUD routes for agents, credentials, schedules,
// sessions (update/messages/stream), workspace MCP/skill, and TTS models.
func (a *App) registerExtendedRoutes() {
	// Agent CRUD
	a.mux.HandleFunc("GET /api/agent", a.handleListAgents)
	a.mux.HandleFunc("POST /api/agent", a.handleCreateAgent)
	a.mux.HandleFunc("GET /api/agent/{id}", a.handleGetAgent)
	a.mux.HandleFunc("DELETE /api/agent/{id}", a.handleDeleteAgent)

	// Credential CRUD
	a.mux.HandleFunc("GET /api/credential", a.handleListCredentials)
	a.mux.HandleFunc("POST /api/credential", a.handleCreateCredential)
	a.mux.HandleFunc("DELETE /api/credential/{id}", a.handleDeleteCredential)

	// Schedule CRUD
	a.mux.HandleFunc("GET /api/schedule", a.handleListSchedules)
	a.mux.HandleFunc("POST /api/schedule", a.handleCreateSchedule)
	a.mux.HandleFunc("DELETE /api/schedule/{id}", a.handleDeleteSchedule)

	// Session extended
	a.mux.HandleFunc("PATCH /api/session/{id}", a.handleUpdateSession)
	a.mux.HandleFunc("GET /api/session/{id}/messages", a.handleListMessages)

	// Workspace MCP/Skill management
	a.mux.HandleFunc("GET /api/workspace/mcp", a.handleListWorkspaceMCPs)
	a.mux.HandleFunc("POST /api/workspace/mcp", a.handleAddWorkspaceMCP)
	a.mux.HandleFunc("DELETE /api/workspace/mcp/{name}", a.handleRemoveWorkspaceMCP)
	a.mux.HandleFunc("GET /api/workspace/skill", a.handleListWorkspaceSkills)
	a.mux.HandleFunc("POST /api/workspace/skill", a.handleAddWorkspaceSkill)
	a.mux.HandleFunc("DELETE /api/workspace/skill/{name}", a.handleRemoveWorkspaceSkill)

	// TTS models
	a.mux.HandleFunc("GET /api/tts-model", a.handleListTTSModels)
}

// --- Agent CRUD ---

func (a *App) handleListAgents(w http.ResponseWriter, r *http.Request) {
	fs := a.getFullStorage()
	if fs == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	userID := r.Header.Get("X-User-ID")
	agents, err := fs.ListAgents(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, agents)
}

func (a *App) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	fs := a.getFullStorage()
	if fs == nil {
		http.Error(w, "storage not configured", http.StatusNotImplemented)
		return
	}
	var req CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	record := &storage.AgentRecord{
		ID:           uuid.NewString(),
		UserID:       r.Header.Get("X-User-ID"),
		Name:         req.Name,
		SystemPrompt: req.SystemPrompt,
		ModelName:    req.Type,
	}
	if err := fs.SaveAgent(r.Context(), record); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (a *App) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	fs := a.getFullStorage()
	if fs == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	record, err := fs.LoadAgent(r.Context(), r.Header.Get("X-User-ID"), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (a *App) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	fs := a.getFullStorage()
	if fs == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = fs.DeleteAgent(r.Context(), r.Header.Get("X-User-ID"), r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// --- Credential CRUD ---

func (a *App) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	fs := a.getFullStorage()
	if fs == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	creds, err := fs.ListCredentials(r.Context(), r.Header.Get("X-User-ID"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, creds)
}

func (a *App) handleCreateCredential(w http.ResponseWriter, r *http.Request) {
	fs := a.getFullStorage()
	if fs == nil {
		http.Error(w, "storage not configured", http.StatusNotImplemented)
		return
	}
	var req CreateCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	record := &storage.CredentialRecord{
		ID:       uuid.NewString(),
		UserID:   r.Header.Get("X-User-ID"),
		Provider: req.Provider,
		Data:     req.Config,
	}
	if err := fs.SaveCredential(r.Context(), record); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, CredentialResponse{ID: record.ID, Provider: record.Provider})
}

func (a *App) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	fs := a.getFullStorage()
	if fs == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = fs.DeleteCredential(r.Context(), r.Header.Get("X-User-ID"), r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// --- Schedule CRUD ---

func (a *App) handleListSchedules(w http.ResponseWriter, _ *http.Request) {
	if a.schedulerMgr == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, a.schedulerMgr.List())
}

func (a *App) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	if a.schedulerMgr == nil {
		http.Error(w, "scheduler not configured", http.StatusNotImplemented)
		return
	}
	var req CreateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	record, err := a.schedulerMgr.Create(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func (a *App) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	if a.schedulerMgr == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	_ = a.schedulerMgr.Cancel(r.Context(), r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// --- Session extended ---

func (a *App) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	session, ok := a.sessionSvc.Get(id)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	var req struct {
		SystemPrompt *string `json:"system_prompt,omitempty"`
		ModelName    *string `json:"model_name,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.SystemPrompt != nil {
		session.SystemPrompt = *req.SystemPrompt
	}
	if req.ModelName != nil {
		session.ModelName = *req.ModelName
	}
	writeJSON(w, http.StatusOK, session.ToResponse())
}

func (a *App) handleListMessages(w http.ResponseWriter, r *http.Request) {
	fs := a.getFullStorage()
	if fs == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	sessionID := r.PathValue("id")
	msgs, err := fs.ListMessages(r.Context(), sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, msgs)
}

// --- Workspace MCP/Skill ---

func (a *App) handleListWorkspaceMCPs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

func (a *App) handleAddWorkspaceMCP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string   `json:"name"`
		Command string   `json:"command"`
		Args    []string `json:"args,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name, "status": "added"})
}

func (a *App) handleRemoveWorkspaceMCP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleListWorkspaceSkills(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

func (a *App) handleAddWorkspaceSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string `json:"name"`
		Instructions string `json:"instructions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name, "status": "added"})
}

func (a *App) handleRemoveWorkspaceSkill(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// --- TTS models ---

func (a *App) handleListTTSModels(w http.ResponseWriter, _ *http.Request) {
	cards := tts.ListTTSModels()
	writeJSON(w, http.StatusOK, cards)
}

// --- Model listing ---

func (a *App) handleListModelsFull(w http.ResponseWriter, _ *http.Request) {
	cards := model.ListModels()
	writeJSON(w, http.StatusOK, cards)
}

// getFullStorage attempts to cast the StateSaver to FullStorage.
func (a *App) getFullStorage() storage.FullStorage {
	if fs, ok := a.cfg.Storage.(storage.FullStorage); ok {
		return fs
	}
	return nil
}
