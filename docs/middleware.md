# Middleware

## Overview

The middleware system uses an onion-chain pattern with 7 hooks. Each middleware wraps the next in the chain, enabling logging, tracing, budget control, memory injection, permission interception, and more.

## Hooks

| Hook | Scope | Pattern |
|------|-------|---------|
| `OnReply` | Wraps the entire agent reply lifecycle | Onion |
| `OnReasoning` | Wraps each reasoning step (ReAct iteration) | Onion |
| `OnModelCall` | Wraps each LLM API call | Onion |
| `OnActing` | Wraps each tool execution | Onion |
| `OnSystemPrompt` | Transforms the system prompt | Pipeline |
| `OnCompressContext` | Wraps context compression | Onion |
| `OnCheckPermission` | Wraps the permission check for a tool call | Onion |

Additionally, `ListTools()` lets middleware provide extra tools to the agent.

### OnCheckPermission

Defines a wrapper for permission checks. The current UnifiedAgent and loop bridge call the permission engine directly, so adding this middleware with `WithMiddlewares` alone does not intercept their checks. An executor integration must explicitly invoke `middleware.BuildCheckPermissionChain`. The wrapper below is suitable for that integration:

```go
type AuditPermissionMiddleware struct {
    middleware.BaseMiddleware
    logger *log.Logger
}

func (m *AuditPermissionMiddleware) OnCheckPermission(
    ctx context.Context,
    input *middleware.CheckPermissionInput,
    next middleware.CheckPermissionHandler,
) (*permission.Decision, error) {
    m.logger.Printf("Permission check: agent=%s tool=%s", input.AgentName, input.ToolCall.Name)
    decision, err := next(ctx, input)
    m.logger.Printf("Permission result: %v", decision)
    return decision, err
}
```

**Signature:**

```go
OnCheckPermission(ctx context.Context, input *CheckPermissionInput, next CheckPermissionHandler) (*permission.Decision, error)
```

**`CheckPermissionInput` fields:**

| Field | Type | Description |
|-------|------|-------------|
| `AgentName` | `string` | Name of the agent requesting permission |
| `ToolCall` | `message.ToolCallBlock` | The tool call being checked |
| `ToolInput` | `map[string]any` | Parsed input arguments |

### OnReasoning

Wraps each reasoning step within the ReAct loop. Use this for per-iteration observability, logging, or to inject guardrails at the iteration level:

```go
type ReasoningGuardMiddleware struct {
    middleware.BaseMiddleware
    maxComplexity int
}

func (m *ReasoningGuardMiddleware) OnReasoning(
    ctx context.Context,
    input middleware.ReasoningInput,
    next middleware.ReasoningHandler,
) <-chan event.Event {
    if input.Iteration > m.maxComplexity {
        ch := make(chan event.Event, 1)
        // Emit warning event
        close(ch)
        return ch
    }
    return next(ctx, input)
}
```

**Signature:**

```go
OnReasoning(ctx context.Context, input ReasoningInput, next ReasoningHandler) <-chan event.Event
```

**`ReasoningInput` fields:**

| Field | Type | Description |
|-------|------|-------------|
| `AgentName` | `string` | Name of the agent |
| `Messages` | `[]*message.Msg` | Current conversation history |
| `Iteration` | `int` | Current ReAct loop iteration (0-indexed) |

## Writing Custom Middleware

Embed `middleware.BaseMiddleware` and override the hooks you need:

```go
type TimingMiddleware struct {
    middleware.BaseMiddleware
}

func (m *TimingMiddleware) OnModelCall(
    ctx context.Context,
    input *middleware.ModelCallInput,
    next middleware.ModelCallHandler,
) (*model.ChatResponse, error) {
    start := time.Now()
    resp, err := next(ctx, input)
    log.Printf("[%s] model call took %v", input.AgentName, time.Since(start))
    return resp, err
}
```

Attach to an agent:

```go
a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithMiddlewares(&TimingMiddleware{
        BaseMiddleware: middleware.BaseMiddleware{MiddlewareKey: "timing"},
    }),
)
```

## Built-in Middleware

### TracingMiddleware

Creates nested spans following OpenTelemetry semantic conventions:

```
invoke_agent (gen_ai.agent.name, agentscope.session_id)
  └── chat (gen_ai.request.model, gen_ai.usage.*)
        └── execute_tool (gen_ai.tool.name)
```

```go
tracer := tracing.LoggerTracer{Logger: as.Logger()}
tracing.SetupTracing(tracer)
a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithMiddlewares(middleware.NewTracingMiddleware(nil)),
)
```

### ReplyBudgetControlMiddleware

Enforces a per-reply token budget. When exceeded, injects a "wrap up" hint and forces `tool_choice=none`:

```go
a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithMiddlewares(middleware.NewReplyBudgetControl(5000)),
)
```

### TTSMiddleware

Intercepts text output and synthesizes audio, injecting DataBlock events:

```go
ttsModel, _ := tts.NewDashScopeTTSModel(...)
a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithMiddlewares(middleware.NewTTSMiddleware(ttsModel)),
)
```

### LongTermMemoryMiddleware

Cross-session memory with 3 modes:

| Mode | Behavior |
|------|----------|
| `static_control` | Automatic search before reply + write-back after |
| `agent_control` | Provides `search_memory` / `add_memory` tools only |
| `both` | Both automatic injection and tools |

```go
store := memory.NewInMemoryStore()
memMW := memory.New(&memory.Config{
    UserID:  "user-123",
    AgentID: "bot",
    Store:   store,
    Mode:    memory.ModeBoth,
    TopK:    5,
})
a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithMiddlewares(memMW),
)
```

Backends: `InMemoryStore` (substring search), `VectorMemoryStore` (cosine similarity with embedding model), `Mem0Store` (mem0 REST API with LLM-based extraction).

### GuardrailMiddleware

Content safety filtering on model responses. Three actions:

| Action | Behavior |
|--------|----------|
| `Block` | Rejects the response with `ErrGuardrailBlocked` |
| `Redact` | Replaces content with a safe placeholder |
| `Warn` | Allows the response but sets metadata flags |

```go
gm := middleware.NewGuardrailMiddleware(
    middleware.KeywordBlockRule("profanity", "badword1", "badword2"),
    middleware.KeywordRedactRule("pii", "[REDACTED]", "ssn", "password"),
    middleware.MaxLengthRule("max_output", 10000, middleware.GuardrailWarn),
    middleware.CustomRule("custom_check", middleware.GuardrailBlock, myCheckFunc),
)
a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithMiddlewares(gm),
)
```

### CostTrackerMiddleware

Before each model call, the tracker checks already-accounted cost. Once that
cost reaches `WithMaxCostUSD`, subsequent calls return `ErrBudgetExceeded`.
The next call's cost is not estimated or reserved: a single call or concurrent
calls can exceed the threshold. Missing usage or prices leave costs unaccounted.
Exchange rates convert tracked totals for display, not billing enforcement.

```go
ct := middleware.NewCostTrackerMiddleware(
    prices,                              // map[string]middleware.ModelPrice
    middleware.WithMaxCostUSD(5.0),
    middleware.WithExchangeRate("CNY", 7.2),
)
```

### RepetitionBreakerMiddleware

Detects identical successful tool-call spins (name + input hash; failed
calls reset the streak). At the threshold a change-strategy reminder is
injected into the system prompt; past it the typed `ErrToolRepetition`
replaces the tool result (the call itself still executes — side effects
cannot be un-run). Streaks are keyed per reply. Allowlist exempts
read-only/idempotent tools.

```go
rb := middleware.NewRepetitionBreaker(
    middleware.WithRepetitionThreshold(3),
    middleware.WithRepetitionAllowlist("read_file", "grep"),
)
```

### ReplyWatchdogMiddleware

Aborts replies that run too long (wall clock) or stall (maximum gap
between events). On expiry the reply context is canceled. Zero values
disable the respective limit.

```go
wd := middleware.NewReplyWatchdog(
    middleware.WithReplyWallClock(5*time.Minute),
    middleware.WithReplyIdleTimeout(60*time.Second),
)
```

### CostLedger + CostTrackingMiddleware

Cross-session cost aggregation: `NewCostLedger()` (bounded raw-entry
retention, default 100k) + `NewCostTracking(ledger, sessionID, agentName,
prices)` records successful model calls that report usage and have a known price; query with
`ledger.Summary(filter)`. Use low-cardinality label values only when
exporting to metrics.

```go
ledger := middleware.NewCostLedger()
track := middleware.NewCostTracking(ledger, "sess-1", "bot", prices)
// ...
sum := ledger.Summary(middleware.CostFilter{SessionID: "sess-1"})
```

### ReplyCostBudgetMiddleware

Per-reply cost budget: at the warn ratio (default 80%) a budget hint is
injected into the system prompt; when the estimate exceeds the cap the
reply stops with `ErrBudgetExceeded`.

```go
cb := middleware.NewReplyCostBudget(1.0, // USD per reply
    middleware.WithCostBudgetWarnRatio(0.8),
)
```

### RunJSONL

Writes the full event stream plus middleware-routed model-call records of a
reply as JSONL — the input for `replay.ParseRunLog` / `replay.DiffRunLogs`
and `examples/replayview`. Optional redaction hook.

```go
runlog := middleware.NewRunJSONL(os.Stdout,
    middleware.WithRunLogRedactor(func(s string) string { return s }),
)
```

### StreamValidator

Development-only guard: buffers a reply's event stream and validates the
event-stream invariants (reply/block/tool-call/tool-result pairing, no
orphan deltas — the same rules `event/streamcheck` and `agenttest` use)
when the reply ends, logging any violations. Off in production: it holds
the whole reply's events in memory.

```go
sv := middleware.NewStreamValidator("bot")
```

### Replay Middleware

Records or replays model calls for deterministic testing. See the `replay` package for details.

**Recording mode** — captures every model call request/response pair into a `Tape`:

```go
recorder := replay.NewRecorder()
a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithMiddlewares(recorder),
)

// After running the agent:
tape := recorder.Tape()  // serialize to JSON for storage
```

**Replay mode** — returns recorded responses; a non-nil model is still required by the constructor and token counter. Import `pkg/agentscope/agenttest` for an offline placeholder:

```go
replayer := replay.NewReplayer(tape)
placeholder := agenttest.NewMockModel()
a := agent.NewUnifiedAgent("bot", "...", placeholder,
    agent.WithMiddlewares(replayer),
)
```

The replay middleware uses the `OnModelCall` hook to intercept calls. In record mode, it passes through to the real model and captures the response. In replay mode, it returns the next tape entry without invoking the model API. It does not compare prompts or intercept tools; use mock or isolated tools if the test must avoid network calls or other side effects.

See [Go Runtime Features](go-exclusive.md) for full replay workflow documentation.

## Hook Execution Order

When a chain is built, middleware enter in configuration order and unwind in reverse. The permission-chain portion below applies only when an integration explicitly calls `BuildCheckPermissionChain`; it is not part of the default UnifiedAgent or loop bridge execution path:

```
MW1.OnReply →
  MW2.OnReply →
    MW1.OnReasoning →
      MW2.OnReasoning →
        MW1.OnModelCall →
          MW2.OnModelCall →
            [actual model call]
          MW2.OnModelCall ←
        MW1.OnModelCall ←
        MW1.OnCheckPermission →
          MW2.OnCheckPermission →
            [actual permission check]
          MW2.OnCheckPermission ←
        MW1.OnCheckPermission ←
        MW1.OnActing →
          MW2.OnActing →
            [actual tool execution]
          MW2.OnActing ←
        MW1.OnActing ←
      MW2.OnReasoning ←
    MW1.OnReasoning ←
  MW2.OnReply ←
MW1.OnReply ←
```

`OnSystemPrompt` is the exception — it runs as a pipeline (each middleware transforms the output of the previous one, not an onion).

## See Also

- [Architecture](architecture.md) — Middleware in the broader system design
- [Go Runtime Features](go-exclusive.md) — Replay middleware for CI/CD
- [Tools](tools.md) — Tool-level middleware
