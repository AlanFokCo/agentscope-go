# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`agentscope-go` is a Go port of the Python [AgentScope](https://github.com/agentscope-ai/agentscope) multi-agent LLM framework. The module path is `github.com/alanfokco/agentscope-go`. All library code lives under `pkg/agentscope/`; runnable demos live under `examples/`.

Python reference code is at `/Users/alanfokco/Github/agentscope/` (main branch). When adding features, check the Python implementation first for design consistency.

`go.mod` declares `go 1.25.0`, but `CONTRIBUTING.md` asks contributors to keep the code compatible with **Go 1.22+**. Don't reach for `>=1.23` standard-library features without checking.

## Common commands

```bash
# Build everything (library + every example main).
go build ./...
go build ./examples/...

# Static checks and tests (matches CI in .github/workflows/ci.yml).
go vet ./...
go test ./...

# Run a single package's tests, or a single test:
go test ./pkg/agentscope/pipeline -run TestName -v

# Run any example (each has its own main package).
go run ./examples/simple
go run ./examples/agent_v2
go run ./examples/streaming
go run ./examples/middleware
go run ./examples/react_tool
go run ./examples/react_builtin_tools
go run ./examples/multi_provider
go run ./examples/structured_output
go run ./examples/pipeline_multi_agent
go run ./examples/permission
go run ./examples/agent_team
go run ./examples/mcp
go run ./examples/embedding
go run ./examples/long_term_memory
go run ./examples/rag_react
go run ./examples/a2a_http
go run ./examples/realtime_echo
go run ./examples/agent_service
go run ./examples/scheduled_task
go run ./examples/model_call
go run ./examples/multimodal
go run ./examples/multiagent
```

`vendor/` is checked into the working tree (and listed in `.gitignore` — i.e. it's a local convenience, not the source of truth). If you change dependencies, run `go mod tidy` and don't commit a stale `vendor/`.

LLM-backed examples need one of: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, or `DASHSCOPE_API_KEY` (+ optional `DASHSCOPE_BASE_URL`). The `loadChatModelFromEnv` helpers inside the examples pick a backend in the order Anthropic → DashScope → OpenAI.

## Deployment

Code must be rsync'd to `root@builder:/opt/Projects/agentscope-go` for commit and push. Do NOT commit or push locally — always rsync first, then `git add/commit/push` on builder.

## Architecture

The package layout intentionally mirrors the Python project (see `docs/migration_from_python.md` for the full mapping). Each subpackage exposes a small interface plus one or more concrete implementations:

### Core

- **`config.go`** — global `Init(opts ...Option)` sets up a process-wide `Config`. `agentscope.Log()` (logrus) is the canonical logger.
- **`message`** — `Msg` with typed `ContentBlock` variants: `TextBlock`, `ThinkingBlock` (with `Extra` for provider-specific fields like Anthropic `signature`), `ToolCallBlock` (with `Extra` for OpenAI Response `call_id`), `ToolResultBlock` (with `Metadata`), `DataBlock` (Base64Source/URLSource for images/audio/video), `HintBlock` (polymorphic `Hint` — `string` or `[]ContentBlock`). `NewMsg` panics on invalid content type by design.
- **`event`** — Full event lifecycle: ReplyStart/End, ModelCallStart/End, TextBlock/ThinkingBlock/DataBlock Start/Delta/End, ToolCall Start/Delta/End, ToolResult Start/TextDelta/DataDelta/End, HintBlock, HITL events (RequireUserConfirm, UserConfirmResult, RequireExternalExecution, ExternalExecutionResult), ExceedMaxIters, Custom.

### Agent

- **`agent`** — `Agent` interface (`ID`, `Reply`, `Observe`, `Interrupt`, `SetConsoleOutputEnabled`) and `AgentBase` (UUID identity, console printing, msghub subscriptions, hooks). Two agent generations:
  - **`UnifiedAgent`** (v2) — aligns with Python's single `Agent` class. Native tool calling, streaming via `ReplyStream()` returning `<-chan event.Event`, middleware chain, permission engine, context compression, skill instructions injection, audio block filtering. Options: `WithToolkit`, `WithMiddlewares`, `WithContextConfig`, `WithPermissionContext`, `WithSkills`, `WithReadCache`.
  - **`UnifiedAgentRunner`** — bridges `UnifiedAgent` to `loop.Loop` via `modelCallerAdapter` and `toolExecutorAdapter`. `LoopOptions()` returns `[]loop.Option` for `loop.New()`. Supports `WithLoopHooks` for metrics/tracing hook injection.
  - **`ReActAgent`** (v1, deprecated) — JSON-based tool calling protocol. Supports RAG via `WithKnowledge(...)` and basic compression via `WithCompression`.
  - **`A2AAgent`** — remote agent proxy via `a2a.Client`.
  - **`UserAgent`** — human input agent with pluggable `InputProvider`.

### Model

- **`model`** — `ChatModel` interface: `Chat`, `ChatStream` (`<-chan ChatResponse`), `CountTokens`. 9 provider adapters: `openai.go`, `anthropic.go`, `dashscope.go`, `deepseek.go`, `gemini.go`, `moonshot.go`, `ollama.go`, `xai.go`, `openai_response.go`. All share `internal/httpx` for HTTP calls.
  - **Call options**: `WithTemperature`, `WithMaxTokens`, `WithTools`, `WithToolChoice`, `WithThinking(enable, budget)`, `WithReasoningEffort(effort)`, `WithRetries(max, delay)`.
  - **`ChatUsage`** tracks `InputTokens`, `OutputTokens`, `CacheCreationInputTokens`, `CacheInputTokens`.
  - **`FallbackChatModel`** — automatic primary → fallback failover.
  - **`SecretStr`** — wrapper type that redacts API keys in `String()`/`MarshalJSON()`.
  - **`GenerateStructuredOutput`** — forces tool call, auto-retries with `tool_choice: "auto"` when thinking-mode conflicts.
  - **`ValidateToolChoice`** — validates tool names against available schemas.
  - **Model cards** — YAML files under `model/models/` loaded via `//go:embed`. `GetModelCard(name)`, `ListModels()`.
  - Token counting traverses all block types including DataBlock base64 estimation.

### Formatter

- **`formatter`** — `Formatter` / `MultiAgentFormatter` interfaces. Per-provider implementations with multimodal DataBlock support:
  - `OpenAIFormatter` — image_url, input_audio formats. `SupportedInputMediaTypes` with glob matching.
  - `AnthropicFormatter` — Anthropic content blocks, image source, ThinkingBlock with signature, tool_use/tool_result.
  - `DashScopeFormatter` — extends OpenAI with video_url, input_audio, reasoning_content.
  - `OpenAIResponseFormatter` — input_text, input_image, function_call/function_call_output, reasoning items.
  - `GeminiFormatter` — Gemini native parts format, inlineData/fileData for media.
  - Shared helpers: `ConvertToolResultToString`, `GroupMessages`, `SupportsMediaType`, `FormatDataBlockForOpenAI`.

### Middleware

- **`middleware`** — Onion-chain hooks on `Middleware` interface:
  - `OnReply` — wraps entire reply lifecycle
  - `OnReasoning` — wraps each reasoning step in the ReAct loop
  - `OnModelCall` — wraps each model API call
  - `OnActing` — wraps each tool execution
  - `OnSystemPrompt` — pipeline transformer for system prompt
  - `OnCompressContext` — wraps context compression
  - `ListTools() []tool.Tool` — middleware can provide additional tools
  - Chain builders: `BuildReplyChain`, `BuildReasoningChain`, `BuildModelCallChain`, `BuildActingChain`, `BuildCompressChain`, `ApplySystemPromptPipeline`.
  - Built-in: budget control, TTS, tracing (with `SpanAttribute`/`AttributedTracer`), long-term memory (3 modes: static/agent/both with vector store).

### Tool

- **`tool`** — `Tool` interface embedding `permission.Checker`. `BaseTool` provides defaults. `FunctionTool` wraps plain Go functions.
  - **Built-in tools**: bash, read, write, edit, glob, grep, reset_tools, task_create/get/list/update. `NewEnhancedToolkit()` provides the full set.
  - **Bash safety**: `bash_parser.go` uses `mvdan.cc/sh/v3/syntax` for AST-level analysis: `IsReadOnlyCommand`, `CheckInjectionRisk`, `CheckDangerousRemoval`, `ExtractFilePaths`, `CheckSedConstraints`, `ExtractCommandPrefixes`. On Windows, regex-based patterns for PowerShell/Cmd replace AST analysis (`isPowerShellReadOnly`, injection patterns, dangerous removal patterns).
  - **Per-tool permission chains**: bash has a 7-step chain (injection → PowerShell dangerous [Windows] → read-only → dangerous cmd → sed → dangerous paths → dangerous removal → ACCEPT_EDITS → passthrough). File tools use `filepath.Match` for glob rules.
  - **Tool streaming**: `ToolChunk` struct + `StreamingTool` optional interface with `ExecuteStream`.
  - **Backend abstraction**: `Backend` interface (`ExecShell`, `ReadFile`, `WriteFile`, `FileExists`). `LocalBackend` default. Context helpers `WithBackend`/`GetBackend`.
  - **Diff generation**: write/edit tools produce unified diff in `ToolResponse.Metadata["diff"]`.
  - **Input validation**: `ValidateInput(schema, input)` does basic JSON Schema type checking before execution.
  - **Task dependencies**: bidirectional updates for blocks/blocked_by.
  - **Read line truncation**: lines > 2000 chars get `[truncated]`.

### Infrastructure

- **`credential`** — Per-provider credential structs + `Factory` with `Register`, `FromMap`, `ListSchemas`.
- **`embedding`** — `Embedder` interface, `FileEmbeddingCache`, model cards via `//go:embed` YAML.
- **`tts`** — `TTSModel` + `RealtimeTTSModel` interfaces. DashScope + CosyVoice implementations. Model cards via `//go:embed` YAML.
- **`mcp`** — MCP client (Stdio + HTTP). Name validation (`^[a-zA-Z0-9_-]+$`), execution timeout wiring.
- **`workspace`** — `Workspace` interface + `LocalWorkspace`, `DockerWorkspace`, `E2BWorkspace`. `ManagedWorkspace` extends with MCP/Skill management (`.mcp.json` persistence, `skills/` directory). `Offloader` interface.
- **`permission`** — `Engine` with 5 modes (default, accept_edits, explore, bypass, dont_ask). `Checker` interface embedded by `Tool`. `Decision` with bypass-immune safety checks.
- **`storage`** — `InMemoryStorage`, `FileStorage`, `RedisStorage` for agent state persistence.
- **`pipeline`** — `Pipeline` with `Then`/`If` combinators. `MsgHub` for agent message routing.
- **`tracing`** — `Tracer` interface + `AttributedTracer` optional extension with `SpanAttribute`. `NoopTracer`, `LoggerTracer`.
- **`skill`** — `Skill` struct with `Category` field, `LocalSkillLoader`, `FormatSkillInstructions`. `SkillManager` registry with `Register`, `Get`, `List`, `ListByCategory`, `LoadFromDir`, `FormatInstructions`.
- **`schedule`** — `InMemoryScheduler` for periodic agent task execution.

### v3 Infrastructure

- **`protocol`** — Shared types for the agent loop: `LoopState` (Idle/Thinking/Acting/Done/Error), `LoopEvent` (state transitions, model results, tool results, errors), `ModelCallResult`, `ToolCallResult`. Used by `loop/`, `runtime/`, and `agent/`.
- **`errors`** — Typed error hierarchy: `AgentScopeError` (base with Code/Retriable), `RetriableError`, `ThrottledError` (with RetryAfter), `PermissionDeniedError`, `TimeoutError`, `ValidationError`, `ModelError`, `ToolError`. `Is`/`As` compatible.
- **`loop`** — `Loop` struct configured via `WithModelCaller`, `WithToolExecutor`, `WithSchemaProvider`, `WithMaxIters`, `WithSystemPrompt`, `WithHooks`. `RunSync` executes the full reasoning-acting cycle. `Hook` interface: `OnLoopStart/End`, `OnModelCallStart/End`, `OnToolExecStart/End`, `OnIteration`.
- **`runtime`** — `SessionEngine` (single-session lifecycle with state machine) and `Harness` (multi-session manager with `Start`/`Stop`/`GetSession`/`ListSessions`). `AgentManager` (subagent lifecycle: `Spawn`/`Stop`/`List`/`WaitAll` with `BudgetTracker` concurrency limits and `SessionHookManager` notifications via `HookSubagentStart`/`HookSubagentEnd`). Uses `protocol.LoopState` for state tracking.
- **`metrics`** — `Provider` interface (`Counter`/`Histogram` factories). `InMemoryProvider` with `Snapshot()` for testing. `MetricsHook` implements `loop.Hook` to track `model_call_total`, `model_call_duration`, `tool_exec_total`, `loop_iteration_total`, `active_loops`.
- **`platform`** — `Detect()` returns cached `Shell` (Type, Path). `DeriveExecArgs(cmd)` returns platform-correct `exec.Command` args. `ShellType`: Bash, Zsh, Sh, PowerShell, Cmd. `CheckPowerShellDangerous` has 10 regex patterns for dangerous PowerShell commands.
- **`sandbox`** — `Policy` interface with `Check(op Operation) Decision`. `AllowAll`, `DenyAll`, `AskUser` policies. `Operation` struct (Type, Target, Details).

### App Layer

- **`app`** — `CreateApp(cfg)` factory wiring session management, chat (sync + SSE streaming), credentials, models, background tasks. HTTP routes: `/api/session`, `/api/chat/{id}`, `/api/chat/{id}/stream`, `/api/credential/schemas`, `/api/model`, `/api/task`. `BackgroundTaskManager`, `CancelDispatcher`.
- **`service`** — Lower-level HTTP service with `SSEWriter`. AG-UI protocol constants. Service middleware (inbox, state change, tool offload).

### Context Compression

- **`agent/compress.go`** — `ContextConfig` (trigger/reserve ratios, compression prompt, summary schema/template, tool result limit). `compressContext` runs through middleware chain, generates structured summary via `GenerateStructuredOutput`, replaces old context with summary.
  - Block-level splitting: `splitMessageAtBlock` for finer granularity.
  - Smart truncation: `TruncateToolResultBlocks` handles per-block truncation including base64 replacement.
  - Read cache cleanup: `cleanReadCacheForReserved` drops stale file caches after compression.

## Conventions

- Always pass `context.Context` as the first argument and return `(T, error)` rather than panicking. The one explicit exception is `message.NewMsg`, which panics on an invalid content type by design.
- Log through `agentscope.Log()` (logrus). `Debug` for noisy details, `Info` for lifecycle, `Warn` for retryable/degraded conditions, `Error` only for terminal failures. The `httpx` helper already logs at the right levels — don't double-log around it.
- **Interfaces + embeddable defaults**: Define interfaces, provide `BaseXxx` structs with pass-through defaults (e.g. `BaseTool`, `BaseMiddleware`).
- **Functional options**: Constructors take `opts ...XxxOption` (e.g. `NewUnifiedAgent(name, prompt, model, opts...)`).
- **Streaming**: Use `<-chan T` pattern. Goroutine writes deltas (`IsLast=false`) then final accumulated response (`IsLast=true`), defers `close(ch)`.
- **Content block polymorphism**: `ContentBlock` interface with type switch in formatters and response parsers. JSON uses `type` field discriminator.
- **Provider-specific extensions**: Use `Extra map[string]any` on ThinkingBlock/ToolCallBlock for provider-specific fields rather than adding provider-specific struct fields.
- New vector backends should mirror the Qdrant pair: a low-level `Index` that takes pre-computed vectors plus a higher-level "text index" that takes an `Embedder`.
- When adding a new example, give it its own `examples/<name>/main.go` and add it to the list in `README.md`. CI builds every example via `go build ./examples/...`, so an example that doesn't compile breaks the whole build.
- When adding a new model provider, follow the pattern: embed `OpenAIFormatter` if OpenAI-compatible, create `XxxConfig` struct, implement `Chat`/`ChatStream`/`CountTokens`, add retry logic via `IsRetryableError`, extract cache tokens from usage response.
