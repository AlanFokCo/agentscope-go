# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`agentscope-go` is a Go port of the Python [AgentScope](https://github.com/agentscope-ai/agentscope) multi-agent LLM framework. The module path is **`github.com/alanfokco/agentscope-go/v2`** (v2+ line): internal and consumer imports use `github.com/alanfokco/agentscope-go/v2/pkg/agentscope/...`. Latest release tag: **`v2.0.9`**. All library code lives under `pkg/agentscope/`; runnable demos live under `examples/`.

See `STABILITY.md` for the API-stability policy, stability tiers, and the production-hardening status (what's done, what's open) — read it before large changes.

Python reference code is at `/Users/alanfokco/Github/agentscope/` (main branch). When adding features, check the Python implementation first for design consistency.

`go.mod` declares `go 1.25.0`. Keep code compatible with Go 1.25+ (the minimum version in `go.mod`).

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

## Deployment / commit workflow (important)

Git lives on **builder** (`root@builder:/opt/Projects/agentscope-go`), not locally. Do NOT commit or push from the local checkout — edit locally, `rsync` to builder, then `git add/commit/push` **on builder**. Hard-won gotchas:

- **The local git HEAD is behind builder** — do not use local `git diff`/`git status` to figure out "what I changed" (it over-reports). Instead: rsync only the files you actually edited, then run `git status` **on builder** — it shows exactly your changes against the real HEAD.
- **Check the tree is clean before every rsync.** The maintainer sometimes works directly on builder (feature branches, uncommitted edits). Run `ssh root@builder 'cd .../agentscope-go && git branch --show-current && git status --short'` first; if there are uncommitted changes, don't rsync over them — coordinate. Also note **which branch** is checked out (commits land on whatever is checked out).
- **`vendor/` is `.gitignore`d** (local convenience, not committed). Adding a dependency: on builder run `go get <pkg> && go mod tidy && go mod vendor`, then commit **`go.mod` + `go.sum`** (vendor is not committed; CI restores deps from the module proxy).
- **Tag pushes need `--no-verify`.** The builder pre-push hook runs an AK-leak scanner that errors on tag refs (`invalid local oid`). For `git push origin <tag>` / tag deletes, use `git push --no-verify ...`. The commit content was already scanned on the `main` push, so this is safe.
- **Commit messages must not mention Claude/AI** or include `Co-Authored-By`. Prefer committing the message via `git commit -F <file>` (rsync a message file) to avoid ssh quoting issues.
- Verify on builder before committing: `go build ./... && go build ./examples/... && go vet ./... && go test -race -count=1 ./...` and `golangci-lint run ./...` (install via `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`). CI is a 3-OS matrix (ubuntu/macos/**windows**) — Windows exercises the PowerShell/Cmd code paths, so shell-specific Unix tests use `if runtime.GOOS == "windows" { t.Skip("requires Unix shell") }`, and sandbox/workspace-relative paths use `path` (forward slash), not `filepath`.

## Architecture

The package layout intentionally mirrors the Python project. Each subpackage exposes a small interface plus one or more concrete implementations:

### Core

- **`config.go`** — global `Init(opts ...Option)` sets up a process-wide `Config`. `agentscope.Log()` (logrus) is the canonical logger.
- **`message`** — `Msg` with typed `ContentBlock` variants: `TextBlock`, `ThinkingBlock` (with `Extra` for provider-specific fields like Anthropic `signature`), `ToolCallBlock` (with `Extra` for OpenAI Response `call_id`), `ToolResultBlock` (with `Metadata`), `DataBlock` (Base64Source/URLSource for images/audio/video), `HintBlock` (polymorphic `Hint` — `string` or `[]ContentBlock`). `NewMsg` panics on invalid content type by design.
- **`event`** — Full event lifecycle: ReplyStart/End, ModelCallStart/End, TextBlock/ThinkingBlock/DataBlock Start/Delta/End, ToolCall Start/Delta/End, ToolResult Start/TextDelta/DataDelta/End, HintBlock, HITL events (RequireUserConfirm, UserConfirmResult, RequireExternalExecution, ExternalExecutionResult), **ToolExecStart/End, ToolPolicyDenied** (sandbox execution visibility), ExceedMaxIters, Custom.

### Agent

- **`agent`** — `Agent` interface (`ID`, `Reply`, `Observe`, `Interrupt`, `SetConsoleOutputEnabled`) and `AgentBase` (UUID identity, console printing, msghub subscriptions, hooks). Two agent generations:
  - **`UnifiedAgent`** (v2) — aligns with Python's single `Agent` class. Native tool calling, streaming via `ReplyStream()` returning `<-chan event.Event`, middleware chain, permission engine, context compression, skill instructions injection, audio block filtering. Options: `WithToolkit`, `WithMiddlewares`, `WithContextConfig`, `WithPermissionContext`, `WithSkills`, `WithReadCache`, `WithState` (restore from checkpoint), `WithStateSaver` (auto-checkpoint at tool-batch boundaries and park points). Crash recovery: `LoadCheckpoint` loads resumable state (schema-version guarded); resumed replies re-emit pending confirm/external events, confirmed calls execute inline, and batched answers are stashed per call ID.
  - **`UnifiedAgentRunner`** — bridges `UnifiedAgent` to `loop.Loop` via `modelCallerAdapter` and `toolExecutorAdapter`. `LoopOptions()` returns `[]loop.Option` for `loop.New()`. Supports `WithLoopHooks` for metrics/tracing hook injection.
  - **`ReActAgent`** (v1, deprecated) — JSON-based tool calling protocol. Supports RAG via `WithKnowledge(...)` and basic compression via `WithCompression`.
  - **`A2AAgent`** — remote agent proxy via `a2a.Client`.
  - **`UserAgent`** — human input agent with pluggable `InputProvider`.

### Model

- **`model`** — `ChatModel` interface: `Chat`, `ChatStream` (`<-chan ChatResponse`), `CountTokens`. 9 provider adapters: `openai.go`, `anthropic.go`, `dashscope.go`, `deepseek.go`, `gemini.go`, `moonshot.go`, `ollama.go`, `xai.go`, `openai_response.go`. All share `internal/httpx` for HTTP calls.
  - **Call options**: `WithTemperature`, `WithMaxTokens`, `WithTools`, `WithToolChoice`, `WithThinking(enable, budget)`, `WithReasoningEffort(effort)`, `WithRetries(max, delay)`.
  - **`ChatResponse`** carries `Error` (terminal streaming failure — consumers MUST check it per chunk) and `StopReason` (normalized `stop`/`length`/`tool_calls`/`content_filter`, via `normalizeStopReason`). Stream consumers emit `ChatResponse{Error: ...}` on scanner/transport failure instead of ending silently (see `stream_error_test.go`).
  - **`ChatUsage`** tracks `InputTokens`, `OutputTokens`, `CacheCreationInputTokens`, `CacheInputTokens`; loop + budget account all dimensions.
  - **Retry**: `internal/httpx` is the transport-level retry authority — retries 429 + 5xx, honors `Retry-After`, full-jitter exponential backoff, ctx-aware (no sleeping through a cancelled context). `IsRetryableError` also honors typed `errors.AgentError.Retryable`.
  - **`FallbackChatModel`** — `NewFallbackChatModel(primary, fallback)` or `NewFallbackChain(models...)` (ordered failover chain), ctx-aware backoff.
  - **`SecretStr`** — wrapper type that redacts API keys in `String()`/`MarshalJSON()`/`MarshalText()`. `UnmarshalJSON` for config file loading. `ResolveAPIKey(plain, secret)` helper for the dual-field migration pattern. All 22 config structs carry `SecretAPIKey SecretStr` alongside deprecated `APIKey string`.
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
   - Built-in: budget control, TTS, tracing (with `SpanAttribute`/`AttributedTracer`), long-term memory (3 modes: static/agent/both with vector store), **cost tracker** (`WithMaxCostUSD` hard cap + `WithExchangeRate` for multi-currency display), **guardrails** (Block/Redact/Warn content filtering on model responses with `KeywordBlockRule`/`KeywordRedactRule`/`MaxLengthRule`/`CustomRule`), **repetition breaker** (`NewRepetitionBreaker`: identical tool-call spin detection, hint at threshold, `ErrToolRepetition` result past it; per-reply streaks), **reply watchdog** (`NewReplyWatchdog`: wall-clock + idle timeouts), **cost ledger** (`NewCostLedger`/`NewCostTracking`: cross-session aggregation with retention bound; `NewReplyCostBudget`: per-reply soft warning + hard `ErrBudgetExceeded` stop), **run logger** (`NewRunJSONL`: full event stream + model-call records as JSONL, with redactor hook), **stream validator** (`NewStreamValidator`: opt-in runtime event-stream invariant checks for development).

### Tool

- **`tool`** — `Tool` interface embedding `permission.Checker`. `BaseTool` provides defaults. `FunctionTool` wraps plain Go functions.
  - **Built-in tools**: bash, read, write, edit, **MultiEdit** (atomic multi-edit of one file, `applyStringEdit`), **ApplyPatch** (apply a unified diff atomically, `applyUnifiedDiff`), glob, grep, webfetch (SSRF-guarded), reset_tools, task_create/get/list/update. `NewEnhancedToolkit()` provides the full set. TodoWrite is intentionally absent — the `task_*` tools cover it (bidirectional block/blocked_by deps).
  - **Bash safety**: `bash_parser.go` uses `mvdan.cc/sh/v3/syntax` for AST-level analysis: `IsReadOnlyCommand`, `CheckInjectionRisk`, `CheckDangerousRemoval`, `CheckInterpreterAttack` (detects `python -c`/`node -e`/`perl -e` etc. with dangerous API calls), `ExtractFilePaths`, `CheckSedConstraints`, `ExtractCommandPrefixes`. On Windows, regex-based patterns for PowerShell/Cmd replace AST analysis (`isPowerShellReadOnly`, injection patterns, dangerous removal patterns).
  - **Process-group isolation**: `proc_unix.go` / `proc_windows.go` — bash commands run in a dedicated process group (`Setpgid`); on timeout the entire group is killed via `SIGKILL`, preventing orphan child processes and fork bombs. `WaitDelay` forces orphaned pipes closed.
  - **Bash redirect safety**: `IsReadOnlyCommand` rejects output redirects (`cat x > /etc/passwd` is NOT read-only); `CheckDangerousRedirect` routes redirect targets through the dotfile + system-path checks (bypass-immune). curl/wget are NOT on the read-only allowlist (network egress). See `redirect_safety_test.go`.
  - **Per-tool permission chains**: bash chain (injection → PowerShell dangerous [Windows] → read-only → dangerous cmd → sed → dangerous paths → dangerous removal → **dangerous redirect** → ACCEPT_EDITS → passthrough). File tools use `filepath.Match` for glob rules.
  - **Workspace jail**: `WithWorkspaceRoot(ctx, root)` + `resolvePath` confine read/write/edit to a root (symlink-aware) when set; unset = unconfined (default, backward-compatible).
  - **Tool streaming**: `ToolChunk` struct + `StreamingTool` optional interface with `ExecuteStream`.
  - **Backend abstraction / sandbox execution**: `Backend` interface (`ExecShell`, `ReadFile`, `WriteFile`, `FileExists`, `ListDir`, `Glob`). `LocalBackend` default; `getBackendIfSet(ctx)` detects an explicitly-configured backend. When one is set, bash + read/write/edit/multiedit/apply_patch route through it using **workspace-relative paths** (so a `workspace.ToolBackend` over a Docker/E2B workspace gives real isolation); otherwise the rich local path (streaming/cwd/read-cache/jail) is used. Wire via `tool.WithBackend(ctx, workspace.NewToolBackend(ws))`.
  - **Orchestrator limits**: `OrchestratorConfig.DefaultToolTimeout` (per-tool `context.WithTimeout`) and `MaxToolResultBytes` (result cap).
  - **Orchestrator sandbox policy**: `OrchestratorConfig.Policy` (`*sandbox.Policy`) — the orchestrator runs `enforceSandboxPolicy` before every tool call: FSReadOnly blocks writes, AllowExec=false blocks bash, NetDisabled blocks WebFetch, DenyPaths blocks file access. Policy is injected into context via `sandbox.WithPolicy`.
  - **Orchestrator audit**: `OrchestratorConfig.AuditLogger` (`audit.Logger`) — records every tool execution, permission denial, and sandbox policy decision as structured `audit.Entry`.
  - **Diff generation**: write/edit/multiedit/apply_patch produce a unified diff in `ToolResponse.Metadata["diff"]`.
  - **Input validation**: `ValidateInput(schema, input)` is a real recursive JSON-Schema validator (type, required, `enum`, string `minLength/maxLength/pattern`, number `minimum/maximum`, nested `properties`, array `items`). Fuzzed (`safety_fuzz_test.go`).
  - **Task dependencies**: bidirectional updates for blocks/blocked_by.
  - **Read line truncation**: lines > 2000 chars get `[truncated]`.

### Infrastructure

- **`credential`** — Per-provider credential structs + `Factory` with `Register`, `FromMap`, `ListSchemas`.
- **`embedding`** — `Embedder` interface, `FileEmbeddingCache`, model cards via `//go:embed` YAML.
- **`agenttest/faults`** — deterministic model-error / tool-failure / latency injection for resilience chaos testing.
- **`model/pricing`** — embedded default price card (`default.yaml`) backing `model.ResolvePrice` (overlay pricing for cost governance/eval; `SetPrice` overrides win).
- **`tts`** — `TTSModel` + `RealtimeTTSModel` interfaces. DashScope + CosyVoice implementations. Model cards via `//go:embed` YAML.
- **`mcp`** — MCP client (Stdio + HTTP). Name validation (`^[a-zA-Z0-9_-]+$`), execution timeout wiring.
- **`workspace`** — `Workspace` interface + 8 backends: `LocalWorkspace`, `DockerWorkspace`, `E2BWorkspace`, `K8sWorkspace`, `OpenSandboxWorkspace`, `DaytonaWorkspace`, `AppleContainerWorkspace`, `BubblewrapWorkspace`. `ManagedWorkspace` extends with MCP/Skill management (`.mcp.json` persistence, `skills/` directory). `ToolBackend` adapts any Workspace into a `tool.Backend`.
  - **K8s workspace hardening** (`workspace/k8s.go`): `PodSecurityContext` (RunAsNonRoot, RunAsUser, RunAsGroup, FSGroup), `ResourceRequirements` (CPU/Memory limits+requests), `ServiceAccountName`, Labels/Annotations for discovery, `PodTTLSeconds` (anti-leak via `activeDeadlineSeconds`), `ImagePullPolicy` (default `IfNotPresent`), `DisableServiceAccount` (`automountServiceAccountToken: false`), `SecretToken` (SecretStr). `buildPodManifest()` extracted for testability. Timeout fix: respects parent ctx deadline.
  - **K8s cluster tools** (`workspace/k8s_tools.go`): `NewKubectlGetTool(kubeconfig)` — read-only query for 15 resource types (secrets explicitly BLOCKED); `NewKubectlLogTool(kubeconfig)` — pod log retrieval with tail/since/container. Both: 30s timeout, no cluster mutation, kubectl shell-out (no client-go dep).
- **`permission`** — `Engine` with 5 modes (default, accept_edits, explore, bypass, dont_ask). `Checker` interface embedded by `Tool`. `Decision` with bypass-immune safety checks.
- **`storage`** — `InMemoryStorage`, `FileStorage`, `RedisStorage` for agent state persistence; **`RedisFullStorage`** implements the 28-method `FullStorage` interface over Redis (credentials, agents, sessions, schedules, messages, teams) with reverse-index message lookup and mutex-protected append. All file-backed writes go through **`internal/fsutil.WriteFileAtomic`** (temp + fsync + rename) so a crash mid-write cannot corrupt state.
- **`internal/fsutil`** — `WriteFileAtomic`. **`internal/httpsec`** — `Harden(*http.Server)` (ReadHeaderTimeout/IdleTimeout/MaxHeaderBytes) + `LimitBody` (MaxBytesReader) for the HTTP servers.
- **`pipeline`** — `Pipeline` with `Then`/`If` combinators. `MsgHub` for agent message routing.
- **`tracing`** — `Tracer` interface + `AttributedTracer` optional extension with `SpanAttribute`. `NoopTracer`, `LoggerTracer`.
- **`replay`** — Deterministic record/replay of LLM calls. `Tape` (versioned sequence of `Entry` carrying `reply_id`/`usage`), `Recorder`/`Replayer` middleware hooking `OnModelCall` (recorder options: `WithRingLimit`, `WithRecordSizeLimit`, `WithDumpOnError` atomic flight-record dumps, `WithRedactor`). `FileStore` for tape persistence. **Eval harness**: `Scorer` interface, 5 built-in scorers (ExactMatch, Contains, JSONField, TextContains, Composite), `EvalTape()` runner producing `EvalReport`, `AssertTape(t, ...)` go-test helper for regression testing. **Run logs**: `ParseRunLog`/`DiffRunLogs` (LCS-aligned diff with truncation flag) + `FormatRunDiff`.
  - **`replay/evalkit`** — YAML task suites (`TaskSpec` with fixtures and budgets), pinned-sampling `Runner` (cost accounting via `model.ResolvePrice`), scorers (contains/json_field/text_contains/trajectory/budget/LLM judge with result caching), multi-turn tasks, Markdown `SuiteReport`, A/B `Compare` reports.
- **`providercontract`** (test-only) — per-provider harness wall asserting usage accounting, streaming lifecycle (exactly one IsLast, delta accumulation == final), truncation-error surfacing, ctx-cancel stops, error taxonomy (429 retryable / 401 not), and thinking wire formats for openai, anthropic, dashscope, gemini, deepseek, moonshot.
- **`event/streamcheck`** — single implementation of event-stream invariants (reply/block/tool-call/tool-result pairing, no orphan deltas); `agenttest` delegates to it.
- **`rag`** — `Index` + `KnowledgeBase` interfaces, 5 vector stores (InMemory, Qdrant, QdrantText, Elasticsearch, Milvus, MongoDB). **Reranker**: `Reranker` interface + `RerankedIndex` wrapper (fetches N*multiplier candidates, reranks for precision). Document parsers under `rag/parser/` (Text, PDF, Word, Excel, PPT).
- **`skill`** — `Skill` struct with `Category` field, `LocalSkillLoader`, `FormatSkillInstructions`. `SkillManager` registry with `Register`, `Get`, `List`, `ListByCategory`, `LoadFromDir`, `FormatInstructions`.
- **`schedule`** — `InMemoryScheduler` for periodic agent task execution.

### v3 Infrastructure

- **`protocol`** — Shared types for the agent loop: `LoopState` (Idle/Thinking/Acting/Done/Error), `LoopEvent` (state transitions, model results, tool results, errors), `ModelCallResult`, `ToolCallResult`. Used by `loop/`, `runtime/`, and `agent/`.
- **`errors`** — Structured `AgentError{Category, Code, Message, Cause, Retryable, RetryAfter, AgentMsg}` with `Is(target)` matching by `Code` for sentinel support and `AgentMessage()` bridging operator-facing/LLM-facing error audiences. `Category` enum: Model/Tool/Permission/Context/Config/Platform/Network/Resource. Sentinels: `ErrModelRateLimited`, `ErrModelTimeout`, `ErrModelContextLimit`, `ErrToolDenied`, `ErrToolTimeout`, `ErrSandboxDenied`, `ErrLoopInterrupted`, `ErrLoopMaxIters`, `ErrBudgetExceeded`, `ErrGuardrailBlocked`. Tool error types: `ToolNotFoundError`, `ToolInterruptedError`, `ToolJSONDecodeError`, `ToolGroupInactiveError`, `ToolExecutionError`, `ToolImplError` (migrated from former `exception` package, now deleted). Helpers: `Newf`, `Wrap`, `IsRetryable`, `NewThrottled`, `RetryAfterOf`, `IsAgentError`, `GetAgentMessage`.
- **`loop`** — `Loop` struct configured via `WithModelCaller`, `WithToolExecutor`, `WithSchemaProvider`, `WithMaxIters`, `WithSystemPrompt`, `WithHooks`. `RunSync` executes the full reasoning-acting cycle. `Hook` interface: `OnLoopStart/End`, `OnModelCallStart/End`, `OnToolExecStart/End`, `OnIteration`.
- **`runtime`** — `SessionEngine` (single-session lifecycle with state machine) and `Harness` (multi-session manager with `Start`/`Stop`/`GetSession`/`ListSessions`). `AgentManager` (subagent lifecycle: `Spawn`/`Stop`/`List`/`WaitAll` with `BudgetTracker` concurrency limits and `SessionHookManager` notifications via `HookSubagentStart`/`HookSubagentEnd`). Uses `protocol.LoopState` for state tracking.
- **`metrics`** — `MetricsProvider` interface (`Counter`/`Histogram` factories, label-aware) + `Noop`. `InMemoryProvider` (label-aware, `Snapshot()`/`ValueFor`) for testing. **`metrics/prometheus`** subpackage: a real `MetricsProvider` over `prometheus/client_golang` + `Handler()` (promhttp) — the only place that pulls the prometheus dep; wire `app.Config.MetricsHandler = provider.Handler()` to expose `GET /metrics`. `MetricsHook` implements `loop.Hook`.
- **`platform`** — `Detect()` returns cached `Shell` (Type, Path). `DeriveExecArgs(cmd)` returns platform-correct `exec.Command` args. `ShellType`: Bash, Zsh, Sh, PowerShell, Cmd. `CheckPowerShellDangerous` has 10 regex patterns for dangerous PowerShell commands.
- **`sandbox`** — `Sandbox` interface (`Execute`, `Setup`, `Teardown`), `Policy` struct (FileSystemPolicy with FSReadOnly/FSWorkspaceOnly/FSFullAccess + DenyPaths, NetworkPolicy with NetDisabled/NetAllowList/NetFullAccess, ProcessPolicy with AllowExec/MaxProcesses, ResourcePolicy with MaxMemoryMB/MaxCPUPercent/MaxDiskMB/TimeoutSec). `SandboxProvider` registry with `AutoSelect`. Context helpers: `WithPolicy`/`GetPolicy`.
- **`audit`** — structured audit logging for tool execution, permission decisions, and policy enforcement. `Logger` interface with 4 implementations: `InMemoryLogger` (thread-safe, for tests), `FileLogger` (append-only JSON Lines), `MultiLogger` (fan-out), `NopLogger` (zero-alloc default). 10 action types. Context propagation via `WithLogger`/`GetLogger`.

### App Layer

- **`app`** — `CreateApp(cfg)` factory wiring session management, chat (sync + SSE streaming), credentials, models, background tasks. HTTP routes: `/api/session`, `/api/chat/{id}`, `/api/chat/{id}/stream`, `/api/credential/schemas`, `/api/model`, `/api/task`, plus `GET /healthz`, `GET /readyz`, and optional `GET /metrics` (`Config.MetricsHandler`). Servers apply `httpsec.Harden` + `LimitBody`. `BackgroundTaskManager`, `CancelDispatcher`.
- **`service`** — Lower-level HTTP service with `SSEWriter` + `Shutdown` (graceful drain), `/healthz`, `/readyz`, hardened server. AG-UI protocol constants. Service middleware (inbox, state change, tool offload).
- **`console`** — Terminal viewing and interactive trial of agents (port of Python's `console` module). `Renderer` turns an event stream into line-based output (quiet/default/debug verbosity, `LastMsg` accumulation); `Launch` runs an interactive chat loop over any `Agent` with `ReplyStream` + `SubmitUserConfirm` (HITL confirmation y/N/a, Ctrl+C interrupts the current reply).

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

## Quality Gate: Evaluator Review (MANDATORY)

Every code change and documentation update MUST pass adversarial evaluator review before commit/push:

1. **For code**: The evaluator checks correctness (bugs, races, panics), completeness (edge cases, error handling), security (injection, leakage), and maintainability (API design). Only PASS (no HIGH findings) allows commit.

2. **For docs**: The evaluator cross-references every code example, API reference, function signature, and numeric claim against actual source code. Any inaccuracy is a blocking finding.

3. **Build gates** (all must pass):
   - `go build ./...`
   - `go vet ./...`
   - `go test -race -count=1 ./...` (or affected packages)
   - `golangci-lint run ./...` — 0 issues

This policy exists because documentation drift and untested code are the two largest sources of technical debt. The evaluator catches what automated tools cannot: semantic correctness, API contract violations, and design flaws that only manifest under adversarial conditions.
