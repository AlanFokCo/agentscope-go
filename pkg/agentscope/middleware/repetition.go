package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	agenterrors "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/errors"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
)

// RepetitionBreakerMiddleware stops an agent from spinning on identical
// tool calls (HARNESS_DESIGN E1) — a common cost blowout shape. The call
// signature key includes the tool name and input; only successful calls
// count (a failed call legitimately may be retried and resets the streak).
// At the threshold a system-prompt hint is injected asking the model to
// change strategy; the identical call past the threshold still executes
// (middleware cannot un-run side effects) but its result is discarded and
// the reply ends with the typed ErrToolRepetition.
//
// Known limitation: spins whose inputs vary by timestamps/random values are
// not detected (hash-based matching).
//
// Streak state lives on the middleware instance guarded by a mutex because
// OnActing runs concurrently for parallel tool batches — MiddleContext is
// not synchronized and must not be touched from concurrent hooks.
type RepetitionBreakerMiddleware struct {
	BaseMiddleware
	threshold int
	hint      string
	allowlist map[string]bool

	mu     sync.Mutex
	streak map[string]repetitionStreak // per agent name
}

type repetitionStreak struct {
	lastKey string
	count   int
}

const defaultRepetitionHint = "<system-reminder>You have repeated the exact same tool call several times without progress. Stop retrying it and try a different approach, or report that the task cannot be completed.</system-reminder>"

// RepetitionBreakerOption configures NewRepetitionBreaker.
type RepetitionBreakerOption func(*RepetitionBreakerMiddleware)

// WithRepetitionThreshold sets how many consecutive identical successful
// calls trigger the hint (default 3). The call after the hint is aborted.
func WithRepetitionThreshold(n int) RepetitionBreakerOption {
	return func(m *RepetitionBreakerMiddleware) {
		if n > 0 {
			m.threshold = n
		}
	}
}

// WithRepetitionHint overrides the injected reminder text.
func WithRepetitionHint(hint string) RepetitionBreakerOption {
	return func(m *RepetitionBreakerMiddleware) {
		if hint != "" {
			m.hint = hint
		}
	}
}

// WithRepetitionAllowlist exempts tools (by name) from repetition breaking —
// use for read-only/idempotent tools that legitimately repeat.
func WithRepetitionAllowlist(names ...string) RepetitionBreakerOption {
	return func(m *RepetitionBreakerMiddleware) {
		for _, n := range names {
			m.allowlist[n] = true
		}
	}
}

// NewRepetitionBreaker creates the middleware with defaults (threshold 3).
func NewRepetitionBreaker(opts ...RepetitionBreakerOption) *RepetitionBreakerMiddleware {
	m := &RepetitionBreakerMiddleware{
		BaseMiddleware: BaseMiddleware{MiddlewareKey: "repetition-breaker"},
		threshold:      3,
		hint:           defaultRepetitionHint,
		allowlist:      map[string]bool{},
		streak:         map[string]repetitionStreak{},
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// OnReply resets per-reply streak state.
func (m *RepetitionBreakerMiddleware) OnReply(ctx context.Context, input ReplyInput, next ReplyHandler) <-chan event.Event {
	m.mu.Lock()
	m.streak[input.AgentName] = repetitionStreak{}
	m.mu.Unlock()
	return next(ctx, input)
}

// OnActing tracks identical successful calls and breaks the loop.
func (m *RepetitionBreakerMiddleware) OnActing(ctx context.Context, input *ActingInput, next ActingHandler) (*tool.ToolResponse, error) {
	resp, err := next(ctx, input)

	if m.allowlist[input.ToolCall.Name] {
		return resp, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.streak[input.AgentName]

	if err != nil {
		// Failed calls may legitimately be retried; reset the streak.
		m.streak[input.AgentName] = repetitionStreak{}
		return resp, err
	}

	key := repetitionKey(input.ToolCall.Name, input.ToolCall.Input)
	if st.lastKey == key {
		st.count++
	} else {
		st = repetitionStreak{lastKey: key, count: 1}
	}
	m.streak[input.AgentName] = st

	if st.count > m.threshold {
		return nil, agenterrors.ErrToolRepetition
	}
	return resp, nil
}

// OnSystemPrompt injects the strategy-change hint once the streak reaches
// the threshold.
func (m *RepetitionBreakerMiddleware) OnSystemPrompt(ctx context.Context, agentName string, currentPrompt string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if st, ok := m.streak[agentName]; ok && st.count >= m.threshold {
		return currentPrompt + "\n\n" + m.hint
	}
	return currentPrompt
}

func repetitionKey(name, input string) string {
	sum := sha256.Sum256([]byte(name + "\x00" + input))
	return hex.EncodeToString(sum[:16])
}
