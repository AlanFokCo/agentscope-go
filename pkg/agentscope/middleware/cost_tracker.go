package middleware

import (
	"context"
	"sync"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
)

// ModelPrice defines the per-million-token pricing for a model. All fields are
// in USD per one million tokens.
type ModelPrice struct {
	InputPerMillion  float64
	OutputPerMillion float64
	CacheReadPerM    float64
	CacheWritePerM   float64
}

// ModelCost is the accumulated usage and cost attributed to a single model.
type ModelCost struct {
	InputTokens  int
	OutputTokens int
	CacheTokens  int
	CostUSD      float64
}

// TurnCost is the accumulated usage and cost for a single logical turn.
type TurnCost struct {
	Turn         int
	InputTokens  int
	OutputTokens int
	CostUSD      float64
}

// SessionCost is a snapshot of all usage and cost tracked across a session.
type SessionCost struct {
	TotalInputTokens  int
	TotalOutputTokens int
	TotalCacheTokens  int
	TotalCostUSD      float64
	ByModel           map[string]ModelCost
	ByTurn            []TurnCost
}

// CostTrackerMiddleware accumulates token usage and cost across every model call
// for the lifetime of the middleware instance (i.e. across turns), in contrast
// to ReplyBudgetControlMiddleware which resets per reply. It is safe for
// concurrent model calls.
type CostTrackerMiddleware struct {
	BaseMiddleware

	mu          sync.Mutex
	prices      map[string]ModelPrice
	totalInput  int
	totalOutput int
	totalCache  int
	totalCost   float64
	byModel     map[string]*ModelCost
	byTurn      []*TurnCost
	currentTurn *TurnCost
}

// NewCostTrackerMiddleware creates a cost tracker with the given per-model
// pricing table, keyed by model name. Model calls whose model name is absent
// from the table still contribute to token totals but incur zero cost.
func NewCostTrackerMiddleware(prices map[string]ModelPrice) *CostTrackerMiddleware {
	p := make(map[string]ModelPrice, len(prices))
	for k, v := range prices {
		p[k] = v
	}
	return &CostTrackerMiddleware{
		BaseMiddleware: BaseMiddleware{MiddlewareKey: "cost_tracker"},
		prices:         p,
		byModel:        make(map[string]*ModelCost),
	}
}

// NewTurn starts a new turn for per-turn tracking. Subsequent model calls are
// attributed to this turn until NewTurn is called again.
func (m *CostTrackerMiddleware) NewTurn() {
	m.mu.Lock()
	defer m.mu.Unlock()
	turn := &TurnCost{Turn: len(m.byTurn) + 1}
	m.byTurn = append(m.byTurn, turn)
	m.currentTurn = turn
}

// OnModelCall records the token usage of each model call and accumulates cost.
func (m *CostTrackerMiddleware) OnModelCall(
	ctx context.Context,
	input *ModelCallInput,
	next ModelCallHandler,
) (*model.ChatResponse, error) {
	resp, err := next(ctx, input)
	if err != nil {
		return resp, err
	}
	if resp == nil || resp.Usage == nil {
		return resp, nil
	}

	modelName := resp.ModelName
	if modelName == "" {
		modelName = input.ModelName
	}

	m.record(modelName, resp.Usage)
	return resp, nil
}

func (m *CostTrackerMiddleware) record(modelName string, usage *model.ChatUsage) {
	cacheTokens := usage.CacheInputTokens + usage.CacheCreationInputTokens

	price := m.prices[modelName]
	cost := float64(usage.InputTokens)*price.InputPerMillion/1e6 +
		float64(usage.OutputTokens)*price.OutputPerMillion/1e6 +
		float64(usage.CacheInputTokens)*price.CacheReadPerM/1e6 +
		float64(usage.CacheCreationInputTokens)*price.CacheWritePerM/1e6

	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalInput += usage.InputTokens
	m.totalOutput += usage.OutputTokens
	m.totalCache += cacheTokens
	m.totalCost += cost

	mc := m.byModel[modelName]
	if mc == nil {
		mc = &ModelCost{}
		m.byModel[modelName] = mc
	}
	mc.InputTokens += usage.InputTokens
	mc.OutputTokens += usage.OutputTokens
	mc.CacheTokens += cacheTokens
	mc.CostUSD += cost

	if m.currentTurn != nil {
		m.currentTurn.InputTokens += usage.InputTokens
		m.currentTurn.OutputTokens += usage.OutputTokens
		m.currentTurn.CostUSD += cost
	}
}

// GetSessionCost returns a snapshot of all accumulated usage and cost. The
// returned maps and slices are copies, safe to read without holding the lock.
func (m *CostTrackerMiddleware) GetSessionCost() SessionCost {
	m.mu.Lock()
	defer m.mu.Unlock()

	byModel := make(map[string]ModelCost, len(m.byModel))
	for name, c := range m.byModel {
		byModel[name] = *c
	}

	byTurn := make([]TurnCost, len(m.byTurn))
	for i, t := range m.byTurn {
		byTurn[i] = *t
	}

	return SessionCost{
		TotalInputTokens:  m.totalInput,
		TotalOutputTokens: m.totalOutput,
		TotalCacheTokens:  m.totalCache,
		TotalCostUSD:      m.totalCost,
		ByModel:           byModel,
		ByTurn:            byTurn,
	}
}
