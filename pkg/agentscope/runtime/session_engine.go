package runtime

import (
	"context"
	"sync"
	"time"

	agentscope "github.com/alanfokco/agentscope-go/pkg/agentscope"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/loop"
)

// SessionEngineConfig holds the configuration for creating a SessionEngine.
type SessionEngineConfig struct {
	LoopOptions []loop.Option
	Budget      Budget
	Store       SessionStore
}

// SessionEngine orchestrates a multi-turn session. It wires together a Loop,
// BudgetTracker, SessionHookManager, and SessionStore to manage the full
// lifecycle of a user session.
type SessionEngine struct {
	mu         sync.RWMutex
	id         string
	loopOpts   []loop.Option
	budget     *BudgetTracker
	hooks      *SessionHookManager
	agents     *AgentManager
	store      SessionStore
	cancelFunc context.CancelFunc
	state      *SessionState
}

// NewSessionEngine creates a new SessionEngine with a unique ID and the given
// configuration. If no Store is provided, an InMemorySessionStore is used.
func NewSessionEngine(cfg SessionEngineConfig) *SessionEngine {
	id := agentscope.GenerateID()
	var store SessionStore
	if cfg.Store != nil {
		store = cfg.Store
	} else {
		store = NewInMemorySessionStore()
	}

	bt := NewBudgetTracker(cfg.Budget)
	hooks := NewSessionHookManager()

	se := &SessionEngine{
		id:       id,
		loopOpts: cfg.LoopOptions,
		budget:   bt,
		hooks:    hooks,
		agents:   NewAgentManager(bt, hooks),
		store:    store,
		state: &SessionState{
			ID:        id,
			Metadata:  make(map[string]any),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	return se
}

// ID returns the unique identifier for this session engine.
func (se *SessionEngine) ID() string { return se.id }

// Hooks returns the session hook manager for registering lifecycle hooks.
func (se *SessionEngine) Hooks() *SessionHookManager { return se.hooks }

// Budget returns the budget tracker for this session.
func (se *SessionEngine) Budget() *BudgetTracker { return se.budget }

// Agents returns the agent manager for spawning and tracking subagents.
func (se *SessionEngine) Agents() *AgentManager { return se.agents }

// SubmitMessage starts a new turn with the given input and returns an event
// channel. The channel is closed when the turn finishes. Session hooks are
// fired around the turn execution, and state is persisted afterwards.
func (se *SessionEngine) SubmitMessage(ctx context.Context, input string) <-chan event.Event {
	se.mu.Lock()
	sessionCtx, cancel := context.WithCancel(ctx)
	se.cancelFunc = cancel
	se.mu.Unlock()

	l := loop.New(se.loopOpts...)
	turn := NewTurn(TurnConfig{
		Loop:   l,
		Hooks:  se.hooks,
		Budget: se.budget,
	})

	out := make(chan event.Event, 64)
	go func() {
		defer close(out)
		defer cancel()

		_ = se.hooks.Fire(HookSessionStart, map[string]any{"session_id": se.id})

		for ev := range turn.Run(sessionCtx, input) {
			emitEvent(sessionCtx, out, ev)
		}

		se.mu.Lock()
		se.state.UpdatedAt = time.Now()
		se.mu.Unlock()

		se.saveState()

		_ = se.hooks.Fire(HookSessionEnd, map[string]any{"session_id": se.id})
	}()

	return out
}

// Interrupt cancels the currently running turn, if any.
func (se *SessionEngine) Interrupt() {
	se.mu.RLock()
	cancel := se.cancelFunc
	se.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// State returns a copy of the current session state.
func (se *SessionEngine) State() *SessionState {
	se.mu.RLock()
	defer se.mu.RUnlock()
	cp := *se.state
	return &cp
}

func (se *SessionEngine) saveState() {
	se.mu.RLock()
	state := se.state
	se.mu.RUnlock()

	if se.store != nil {
		_ = se.store.Save(se.id, state)
	}
}
