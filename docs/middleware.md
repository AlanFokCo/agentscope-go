# Middleware

## Overview

The middleware system uses an onion-chain pattern with 5 hooks. Each middleware wraps the next in the chain, enabling logging, tracing, budget control, memory injection, and more.

## Hooks

| Hook | Scope | Pattern |
|------|-------|---------|
| `OnReply` | Wraps the entire agent reply lifecycle | Onion |
| `OnModelCall` | Wraps each LLM API call | Onion |
| `OnActing` | Wraps each tool execution | Onion |
| `OnSystemPrompt` | Transforms the system prompt | Pipeline |
| `OnCompressContext` | Wraps context compression | Onion |

Additionally, `ListTools()` lets middleware provide extra tools to the agent.

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
    agent.WithMiddlewares(middleware.NewReplyBudgetControlMiddleware(5000)),
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
store := memory.NewInMemoryMemoryStore()
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

Backends: `InMemoryMemoryStore` (substring search), `VectorMemoryStore` (cosine similarity with embedding model), `Mem0Store` (mem0 REST API with LLM-based extraction).
