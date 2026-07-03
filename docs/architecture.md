# Architecture

## Overview

agentscope-go is organized as a single Go module at `github.com/alanfokco/agentscope-go`. All library code lives under `pkg/agentscope/`, runnable demos under `examples/`.

## Package Structure

```
pkg/agentscope/
├── config.go              # Global Init(), logging, ID factory
│
├── agent/                 # Agent abstractions
│   ├── agent.go           # Agent interface + AgentBase (identity, hooks, subscribers)
│   ├── unified_agent.go   # UnifiedAgent — native tool calling, streaming, middleware
│   ├── loop_bridge.go     # UnifiedAgentRunner — bridges UnifiedAgent to loop.Loop
│   ├── user_agent.go      # UserAgent — human input via InputProvider
│   ├── a2a_agent.go       # A2AAgent — remote agent proxy via HTTP
│   ├── compress.go        # Context compression (trigger/reserve ratios, structured summary)
│   └── agent_state.go     # Serializable session state
│
├── model/                 # LLM provider interface
│   ├── model.go           # ChatModel interface, CallOptions, ToolSchema
│   ├── openai.go          # OpenAI Chat Completions adapter
│   ├── anthropic.go       # Anthropic Messages adapter
│   ├── dashscope.go       # DashScope (Qwen) adapter + shared OpenAI-compat types
│   ├── deepseek.go        # DeepSeek adapter
│   ├── gemini.go          # Google Gemini adapter
│   ├── ollama.go          # Ollama (local) adapter
│   ├── moonshot.go        # Moonshot/Kimi adapter
│   ├── xai.go             # xAI/Grok adapter
│   ├── openai_response.go # OpenAI Responses API adapter
│   ├── fallback.go        # FallbackChatModel (primary → fallback failover)
│   ├── structured_output.go # GenerateStructuredOutput (JSON Schema via tool call)
│   ├── wav.go             # PCM-to-WAV conversion for audio streaming
│   ├── http.go            # ClientOptions, defaultHTTPClient, mergeHeaders
│   └── models/            # 51 YAML model cards (//go:embed)
│
├── tool/                  # Tool system
│   ├── tool.go            # Tool interface, BaseTool, FunctionTool, Toolkit
│   ├── builtin_bash.go    # Bash tool with AST-level safety analysis
│   ├── builtin_read.go    # File read tool
│   ├── builtin_write.go   # File write tool
│   ├── builtin_edit.go    # File edit tool (search/replace)
│   ├── builtin_glob.go    # Glob pattern matching
│   ├── builtin_grep.go    # Text search
│   ├── builtin_task.go    # Task management (create/get/list/update)
│   ├── bash_parser.go     # Command safety analysis (injection, dangerous paths)
│   └── backend.go         # Backend interface (exec shell, read/write files)
│
├── message/               # Message types
│   ├── msg.go             # Msg struct with polymorphic ContentBlock
│   ├── block.go           # TextBlock, ThinkingBlock, ToolCallBlock, ToolResultBlock, DataBlock, HintBlock
│   └── append_event.go    # Event-to-message accumulation
│
├── event/                 # Streaming event system
│   └── event.go           # 28 event types (reply, model, text, thinking, data, tool, HITL)
│
├── middleware/             # Agent middleware
│   ├── middleware.go       # Middleware interface (5 hooks), chain builders
│   ├── tracing.go         # TracingMiddleware (OpenTelemetry semantic conventions)
│   ├── tts.go             # TTSMiddleware (text → audio DataBlock injection)
│   ├── budget.go          # ReplyBudgetControlMiddleware (token budget enforcement)
│   └── memory/            # LongTermMemoryMiddleware (3 modes: static/agent/both)
│
├── formatter/             # Per-provider message formatting
├── permission/            # Permission engine (5 modes, rule matching, HITL)
├── pipeline/              # Pipeline (Then/If) + MsgHub (multi-agent routing)
├── credential/            # Provider credential types + factory
├── mcp/                   # MCP client (Stdio + HTTP) and server
├── embedding/             # Embedding models (4 providers) + cache
├── rag/                   # Index + KnowledgeBase + Qdrant
├── team/                  # Agent teams (leader/worker coordination)
├── workspace/             # Sandboxed execution (Local, Docker, E2B)
├── storage/               # State persistence (InMemory, File, Redis)
├── service/               # HTTP Agent Service + SSE + AG-UI protocol
├── tts/                   # TTS models (DashScope standard + CosyVoice realtime)
├── tracing/               # Tracer interface + OpenTelemetry + LoggerTracer
├── schedule/              # InMemoryScheduler for periodic tasks
├── messagebus/            # InMemory + Redis pub/sub + registry operations
├── session/               # Session KV store
├── skill/                 # Reusable skill system + SkillManager registry
├── memory/                # Conversation memory + compression
├── a2a/                   # Agent-to-Agent protocol types + HTTP client
├── prompt/                # Composable system prompt assembly from named sections
├── resilience/            # Circuit breaker + rate limiter wrappers for ChatModel
├── realtime/              # Realtime streaming interface + echo client
├── logging/               # Structured logging handlers and initialization
│
├── protocol/              # Shared loop types: LoopState, LoopEvent
├── loop/                  # Configurable agent loop (model → inspect → act → iterate)
├── runtime/               # SessionEngine, AgentManager, BudgetTracker, AgentPool
├── metrics/               # Counter/Histogram interfaces + InMemoryProvider + MetricsHook
├── platform/              # Cross-platform shell detection + PowerShell safety checks
├── sandbox/               # Sandbox execution policies (Allow/Deny/AskUser)
├── errors/                # Typed error hierarchy (Retriable, Throttled, PermissionDenied, Timeout)
└── internal/              # httpx (HTTP+SSE), jsonx (JSON repair)
```

## Core Concepts

### Agent

The `Agent` interface defines five methods: `ID()`, `Reply()`, `Observe()`, `Interrupt()`, `SetConsoleOutputEnabled()`.

`UnifiedAgent` is the primary implementation. It runs a ReAct loop: reasoning (model call) → acting (tool execution) → repeat until done or max iterations. Streaming via `ReplyStream()` returns `<-chan event.Event`.

### Message

`Msg` carries typed `ContentBlock` slices. Block types: `TextBlock`, `ThinkingBlock`, `ToolCallBlock`, `ToolResultBlock`, `DataBlock` (images/audio), `HintBlock`. JSON serialization uses a `type` field discriminator.

### Model

`ChatModel` provides `Chat()`, `ChatStream()`, `CountTokens()`. Nine provider adapters share common patterns: functional options via `CallOption`, retry logic, `ClientOptions` for HTTP customization. All streaming adapters parse SSE via the shared `internal/httpx` helper.

### Tool

`Tool` embeds `permission.Checker` for fine-grained access control. `FunctionTool` wraps plain Go functions. `Toolkit` manages named `ToolGroup`s with activation/deactivation. Built-in tools include a Bash executor with AST-level command safety analysis.

### Middleware

Five-hook onion chain: `OnReply` wraps the entire reply, `OnModelCall` wraps each API call, `OnActing` wraps tool execution, `OnSystemPrompt` transforms the system prompt, `OnCompressContext` wraps compression. Middleware can also provide additional tools via `ListTools()`.

### Agent Loop (v3)

`loop.Loop` is a lower-level reasoning engine that drives the Reason → Inspect → Act state machine. It takes pluggable components:

- **`ModelCaller`** — wraps the LLM API call
- **`ToolExecutor`** — executes tool calls (single or batch)
- **`ContextManager`** — manages conversation history and compression
- **`ToolSchemaProvider`** — provides tool schemas for model calls
- **`Hook`** — lifecycle notifications (before/after model call, before/after tool exec, state transitions, loop start/end)

`Run()` returns `<-chan event.Event` for streaming; `RunSync()` blocks and returns the final `ChatResponse`. `UnifiedAgentRunner` bridges `UnifiedAgent` to `loop.Loop` via adapter types.

### Runtime (v3)

Session lifecycle management:

- **`SessionEngine`** — single-session manager with `SubmitMessage()`, interrupt support, and budget tracking
- **`Turn`** — wraps a single loop execution with hooks and budget enforcement
- **`AgentManager`** — spawns/stops subagents with concurrency limits via `BudgetTracker`
- **`AgentPool`** — worker pool pattern for parallel agent execution
- **`BudgetTracker`** — enforces limits on turns, tokens, duration, and concurrency

### Metrics (v3)

Provider-agnostic instrumentation:

- **`Counter`** / **`Histogram`** — metric interfaces (no external dependencies)
- **`InMemoryProvider`** — stores metrics in memory with `Snapshot()` for testing
- **`MetricsHook`** — implements `loop.Hook` to automatically track model calls, tool executions, loop iterations, and active loops

### Sandbox (v3)

Sandboxed command execution:

- **`Policy`** — configures filesystem, network, process, and resource restrictions
- **`Sandbox`** interface — `Execute()`, `Setup()`, `Teardown()`
- **`NoopSandbox`** — passthrough execution with no restrictions (default)
- Provider registration via `RegisterProvider()` / `AutoSelect()`

## Data Flow

```
User Input
    │
    ▼
UnifiedAgent.Reply(ctx, input)
    │
    ├── OnReply middleware chain
    │       │
    │       ▼
    │   ReAct Loop (max N iterations)
    │       │
    │       ├── OnSystemPrompt pipeline → build system prompt
    │       │
    │       ├── OnModelCall chain → ChatModel.Chat/ChatStream
    │       │       │
    │       │       └── Provider adapter → HTTP/SSE → LLM API
    │       │
    │       ├── Parse response → TextBlock / ToolCallBlock
    │       │
    │       └── If ToolCallBlock:
    │               │
    │               ├── Permission check (Engine.CheckPermission)
    │               │       │
    │               │       ├── ALLOW → execute
    │               │       ├── ASK → emit RequireUserConfirmEvent → wait
    │               │       └── DENY → skip
    │               │
    │               ├── OnActing chain → Tool.Execute
    │               │
    │               └── Append ToolResultBlock → next iteration
    │
    ▼
ChatResponse (Content: []ContentBlock, Usage, Metadata)
```

The v3 `loop.Loop` provides the same state machine at a lower level. `UnifiedAgentRunner` bridges between the two: it wraps the agent's model and toolkit into `ModelCaller` / `ToolExecutor` adapters, letting `loop.Loop` drive the cycle while `UnifiedAgent` handles middleware, permissions, and HITL.

## Design Principles

- **Interfaces + embeddable defaults**: Define small interfaces, provide `BaseXxx` structs (e.g., `BaseTool`, `BaseMiddleware`)
- **Functional options**: Constructors take `opts ...Option` (e.g., `NewUnifiedAgent(name, prompt, model, opts...)`)
- **Streaming via channels**: `<-chan T` pattern. Goroutine writes deltas then final response, defers `close(ch)`
- **Context propagation**: `context.Context` as first argument everywhere
- **Explicit errors**: Return `(T, error)` rather than panicking (exception: `message.NewMsg` panics on invalid content type)
