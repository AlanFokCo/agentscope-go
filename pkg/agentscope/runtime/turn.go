package runtime

import (
	"context"

	agentscope "github.com/alanfokco/agentscope-go/pkg/agentscope"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/loop"
)

// TurnConfig holds the dependencies for a single Turn execution.
type TurnConfig struct {
	Loop   *loop.Loop
	Hooks  *SessionHookManager
	Budget *BudgetTracker
}

// Turn represents a single agent turn: one user input producing one stream of
// events via the underlying Loop. Each Turn has a unique ID and runs
// pre/post hooks and budget checks around the loop execution.
type Turn struct {
	id  string
	cfg TurnConfig
}

// NewTurn creates a new Turn with a unique ID and the given configuration.
func NewTurn(cfg TurnConfig) *Turn {
	return &Turn{
		id:  agentscope.GenerateID(),
		cfg: cfg,
	}
}

// ID returns the unique identifier for this turn.
func (t *Turn) ID() string { return t.id }

// Run starts the turn in a background goroutine and returns an event channel.
// The channel is closed when the turn finishes. Callers should range over the
// channel to receive all events.
func (t *Turn) Run(ctx context.Context, input string) <-chan event.Event {
	out := make(chan event.Event, 64)
	go t.run(ctx, input, out)
	return out
}

func (t *Turn) run(ctx context.Context, input string, out chan<- event.Event) {
	defer close(out)

	// Fire pre-turn hook.
	if t.cfg.Hooks != nil {
		if err := t.cfg.Hooks.Fire(HookPreTurn, map[string]any{"turn_id": t.id, "input": input}); err != nil {
			emitEvent(ctx, out, event.NewCustomEvent("", "turn.hook_error", map[string]any{"error": err, "hook": "pre_turn"}))
			return
		}
	}

	// Check budget.
	if t.cfg.Budget != nil {
		if err := t.cfg.Budget.AddTurn(); err != nil {
			emitEvent(ctx, out, event.NewCustomEvent("", "turn.budget_exceeded", map[string]any{"error": err}))
			return
		}
	}

	// Guard against nil loop.
	if t.cfg.Loop == nil {
		emitEvent(ctx, out, event.NewCustomEvent("", "turn.error", map[string]any{"error": "loop is nil"}))
		return
	}

	// Forward all events from the loop.
	for ev := range t.cfg.Loop.Run(ctx, input) {
		emitEvent(ctx, out, ev)
	}

	// Fire post-turn hook.
	if t.cfg.Hooks != nil {
		if err := t.cfg.Hooks.Fire(HookPostTurn, map[string]any{"turn_id": t.id}); err != nil {
			emitEvent(ctx, out, event.NewCustomEvent("", "turn.hook_error", map[string]any{"error": err, "hook": "post_turn"}))
		}
	}
}
