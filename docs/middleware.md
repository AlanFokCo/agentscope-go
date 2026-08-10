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

Intercepts permission checks before tool execution. Use this to implement custom authorization logic, audit logging, or dynamic permission policies:

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

**Replay mode** — returns pre-recorded responses, no model needed:

```go
replayer := replay.NewReplayer(tape)
a := agent.NewUnifiedAgent("bot", "...", nil,  // no model!
    agent.WithMiddlewares(replayer),
)
```

The replay middleware uses the `OnModelCall` hook to intercept calls. In record mode, it passes through to the real model and captures the response. In replay mode, it returns the next entry from the tape without calling any model.

See [Go-Exclusive Features](go-exclusive.md) for full replay workflow documentation.

## Hook Execution Order

When multiple middleware are chained, hooks execute in onion order:

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
- [Go-Exclusive Features](go-exclusive.md) — Replay middleware for CI/CD
- [Tools](tools.md) — Tool-level middleware
