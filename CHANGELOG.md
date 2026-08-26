# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

Version sections below correspond to git tags (`v2.0.4` onward). Per-version
details can be verified with `git log <prev-tag>..<tag> --oneline`.

## [v2.0.9] - 2026-08-26

### Added
- **Structured output strategy ladder** (`model/`): `GenerateStructuredOutput`
  now walks `forced → auto → no_think → none`; provider request-shape
  rejections and missing structured results advance the ladder, other errors
  stop it; failures wrap the typed `errors.ErrStructuredOutput` (port of
  upstream fix #2140)
- **`model.WithThinkingDisabled()`** call option + `ThinkingDisabler`
  provider interface: DashScope sends `enable_thinking=false`;
  DeepSeek/Moonshot/Anthropic send `thinking:{"type":"disabled"}`
  (upstream #2140)
- **`gen_ai.input.messages` chat-span attribute** (`middleware/tracing`):
  bounded `role: text` serialization of what the model saw (port of upstream
  fix #2391)
- **`ToolResultDataDeltaEvent.Validate()`** (`event/`): enforces exactly one
  of Data/URL (port of upstream fix #2370)

### Changed
- **Gemini usage accounting**: tool-use prompt tokens count as input, thought
  tokens as output, cached-content tokens feed cache accounting (port of
  upstream fix #2406)
- **Audio input formats**: explicit `wav|mp3|mpeg→mp3` map for `input_audio`;
  unsupported audio subtypes produce a clear `Format` error instead of being
  passed through to the API (port of upstream fix #2301). Note: the exported
  `FormatDataBlockForOpenAI`/`FormatDataBlockForDashScope` helpers keep their
  signatures and return nil for invalid audio (dropping the block); the
  `Format` paths surface the error
- **Compression trigger ratio** `0.9` is now accepted; only values above it
  fall back to the 0.8 default (port of upstream fix #2396)

### Fixed
- **OpenAI Responses streams close deterministically**: `processStream`
  honors ctx on every send; scan errors and streams ending without
  `response.completed` surface as a final `IsLast` response instead of
  ending silently (port of upstream fix #2349)
- **`ReadCache` refreshes recency on hit**: repeatedly-read files no longer
  evict first under FIFO (port of upstream fix #1811)
- **Compression falls back to truncation** when summary generation fails —
  the context is truncated to the reserve set, the previous summary is kept
  (a truncation notice substitutes an empty one) and the dropped content is
  offloaded when an offloader is configured, instead of staying wedged above
  the threshold (port of upstream fix #2140)
- **Chat SSE handler releases the session registry via defer**: a panic
  mid-handler can no longer wedge the session slot permanently

## [v2.0.8] - 2026-08-18

### Added
- **Model cards synced with the upstream AgentScope Python v2.0.6 refresh**:
  24 cards added (claude-fable-5/opus-5/sonnet-5, qwen-flash and
  qwen3.5/3.6/3.7-flash, qwen3.8-max, gemini flash-lite family,
  gemini-3.5/3.6-flash, kimi-k2.7-code(-highspeed), gpt-5.6 luna/sol/terra,
  grok-4.20 family, grok-4.5, grok-build-0.1); 48 existing cards refreshed to
  upstream values; Go-exclusive embedding cards preserved
- **`ModelCard` lenient `deprecated_at` parsing** (`UnmarshalYAML`): accepts
  RFC3339, naive ISO, space-separated, and plain-date timestamps, matching the
  upstream Python card format (previously sunset cards were silently dropped)
- **`Toolkit.HasGroup` / `IsGroupActive` / `GroupNames`** for group inspection
- **`StdioClient.Reconnect`** (`mcp/`): reconnect a closed or failed MCP
  stdio client; `Close` is now idempotent and calls on a closed client return
  a clear error (port of upstream fix #2308). `Close`/`Reconnect` stay
  responsive even while a `CallTool` is hung on an unresponsive server — the
  subprocess is killed outside the wire mutex, which unblocks the in-flight
  read
- **`platform.Command`**: build an `exec.Cmd` through the platform-detected
  shell (bash/zsh/sh on Unix; pwsh, powershell.exe, or cmd.exe on Windows)
- **`FormatDataBlockForDashScope`** (`formatter/`): DashScope audio variant —
  base64 audio wrapped in a `data:;base64,` URL (port of upstream fix #2315).
  The mpeg→mp3 format mapping lives in the shared data-block formatter and
  applies to the OpenAI path as well (its `input_audio` API only accepts
  `wav|mp3` labels)
- Regression test locking in Moonshot/OpenAI-compatible trailing usage-only
  chunk handling (upstream #2314; verified not affected)

### Changed
- **DashScope requests now use `DashScopeFormatter`**: base64 audio sent as
  data URLs (upstream fix #2315), video DataBlocks emitted as `video_url`
  parts (previously dropped), and `reasoning_content` preserved in request
  history
- **Shell execution is platform-aware**: `LocalWorkspace.Execute`,
  `workspace.LocalBackend.ExecCommand`, `tool.LocalBackend.ExecShell`, and the
  legacy shell tool run commands through `platform.Command` instead of
  hardcoded `sh -c` — Windows workspaces execute via PowerShell/cmd
  (port of upstream feature #2132). On Unix, execution now follows the
  detected shell (bash/zsh/sh): a non-POSIX `$SHELL` (fish, tcsh, xonsh, …)
  is skipped in favor of the bash→zsh→sh chain so `-c` commands keep POSIX
  semantics

### Fixed
- **ResetTools validates all group names before changing any state**: unknown
  or `basic` group names now return an error listing the invalid names and
  available groups instead of partially resetting activations; non-string
  array elements (e.g. `activate: [1]`) are rejected instead of being
  silently dropped (port of upstream fix #2302)
- **`CollectStream` preserves ERROR state**: a trailing INTERRUPTED/DENIED
  final chunk no longer overwrites an earlier ERROR (port of upstream fix
  #2178)
- **External tool results no longer emit a duplicate `ToolResultStart`**: a
  SUBMITTED external call already emits its start event at submission, so a
  wait that ends without a matching result (canceled, timed out, or unmatched
  submission) now emits only the delta/end (upstream #2167 class)

## [v2.0.7] - 2026-08-11

### Added
- **K8s workspace hardening** (`workspace/k8s.go`): `PodSecurityContext`
  (RunAsNonRoot, RunAsUser, RunAsGroup, FSGroup), `ResourceRequirements`
  (CPU/Memory limits and requests), `ServiceAccountName`, Labels + Annotations
  on created Pods, `PodTTLSeconds` (default 3600, anti-leak cleanup via
  `activeDeadlineSeconds`), `ImagePullPolicy` (default `IfNotPresent`),
  `DisableServiceAccount` (`automountServiceAccountToken: false`), and
  `SecretToken` (`model.SecretStr`) alongside the plain `Token` field
- **Read-only Kubernetes cluster tools** (`workspace/k8s_tools.go`):
  `NewKubectlGetTool` (15 resource types; `secrets` BLOCKED) and
  `NewKubectlLogTool` (tail/since/container) — 30s timeout, no cluster mutation
- `examples/k8s_workspace` demonstrating the usage pattern

### Changed
- Extracted `buildPodManifest()` for testability

### Fixed
- Duplicate timeout in `runKubectl` (now respects the parent context deadline)
- Replaced GNU `find -printf` with a POSIX-compatible alternative

## [v2.0.6] - 2026-08-10

### Added
- **Web UI Studio** (`webui/`): embedded single-page web interface for agent
  interaction; zero-dependency SPA served via `go:embed` with streaming chat,
  thinking-block display, tool-call visualization, human-in-the-loop
  confirmation, session management, and model browser; mount with
  `service.HandlerWithWebUI` or use `webui.Handler` directly
- **Reranker interface + RerankedIndex** (`rag/`): `Reranker` interface and
  `RerankedIndex` wrapper that re-scores retrieval results for improved
  precision
- **Eval harness** (`replay/`): `Scorer` interface, `EvalTape`, and
  `AssertTape` for replay-based agent evaluation with pluggable scoring
  functions
- **CostTrackerMiddleware**: hard USD spend cap via `WithMaxCostUSD`; currency-conversion
  display support (`WithExchangeRate`, e.g. CNY)
- **GuardrailMiddleware** (`middleware/`): output content safety filtering
  with three actions — `Block` (rejects with `ErrGuardrailBlocked`), `Redact`
  (replaces with placeholder), `Warn` (allows with metadata flags); built-in
  rules: `KeywordBlockRule`, `KeywordRedactRule`, `MaxLengthRule`,
  `CustomRule`
- **RedisFullStorage** (`storage/`): 28-method `FullStorage` implementation
  backed by Redis with TTL, prefix isolation, and atomic operations
- **SecretStr**: `UnmarshalJSON` support + dual-field (`APIKey`/`SecretAPIKey`)
  across all 22 API-key configs (model, TTS, embedding, workspace, hub, and storage adapters) for safe key handling
- **`errors.AgentError.Is()` method**: sentinel matching via `errors.Is`;
  `AgentMessage()` for LLM-facing error descriptions
- Spanish README (`README.es-ES.md`) — first community PR from a human
  contributor (@webbrain-one, #3)
- Werewolves multi-agent game demo (`examples/werewolves`) plus 3 more
  examples; 35 new tests from the adversarial-review pass

### Added — Edge & Embedded Intelligence
- **ConnectivityAwareModel** (`model/connectivity.go`): wraps local + cloud
  ChatModel with internal circuit breaker; routes to cloud when online, falls
  back to local (Ollama) when offline, auto-recovers via single-probe half-open
- **PubSub interface** (`messagebus/pubsub.go`): minimal pub/sub contract for
  IoT protocols with QoS (0/1/2), retain, configurable buffer size
- **MQTT adapter** (`messagebus/mqtt/`): Eclipse Paho-based PubSub
  implementation with auto-reconnect, topic prefix, credentials; build tag
  `mqtt` to avoid bloating non-MQTT builds
- **Device framework** (`device/`): `Connector` interface + 4 pure-Go hardware
  drivers (Serial via termios, GPIO via chardev, CAN via SocketCAN, I2C via
  i2c-dev) — all `//go:build linux`, zero CGO
- **DeviceTool**: wraps any Connector as `tool.Tool`; sensors auto-allowed,
  actuators require bypass-immune ASK; integrated watchdog kick on success
- **SensorTool**: read-only sensor tool with JSON output and auto-allow
  permissions
- **SensorMiddleware**: injects live sensor readings into system prompt
  (`[SENSOR DATA]...[/SENSOR DATA]`) with configurable max-token budget to
  prevent context bloat
- **Watchdog**: timer-based safety; if `Kick()` not called within timeout,
  triggers safe-state callback (motor off, valve close, etc.)
- **CI cross-arch job**: build verification for linux/arm64, arm, mips64le,
  riscv64 with binary size check (< 18MB)
- 4 edge examples: `edge_offline`, `edge_sensor`, `edge_serial_robot`,
  `edge_fleet`
- 4 docs: edge-deployment.md, device-tools.md, offline-operation.md,
  multi-device.md

### Added — Security & Audit
- **Audit logging** (`audit/`): new package with structured `Logger` interface
  and 4 implementations (InMemory, File/JSON-Lines, Multi fan-out, Nop); 10
  action types; context propagation via `WithLogger`/`GetLogger`
- **Sandbox execution events**: 3 new event types — `tool_exec_start`,
  `tool_exec_end`, `tool_policy_denied` — providing visibility into the
  orchestrator execution layer
- **Sandbox Policy enforcement**: `sandbox.Policy` now actually controls tool
  execution; `OrchestratorConfig.Policy` blocks tool calls that violate
  filesystem/network/process policies before execution
- **Process-group isolation** (`proc_unix.go`/`proc_windows.go`): child
  processes killed as a group on timeout via `Setpgid` + `SIGKILL` to process
  group, preventing orphans and fork-bombs
- **Interpreter attack detection** (`CheckInterpreterAttack`): detects
  dangerous API calls hidden inside interpreter inline-code flags
  (`python -c`, `node -e`, `perl -e`, `ruby -e`, `lua -e`, `php -r`); checks
  for `os.system`, `subprocess`, `child_process`, etc.
- **Write hardening**: 10 MB size cap (`MaxWriteBytes`), atomic writes via
  `fsutil.WriteFileAtomic`, executable-extension detection triggers
  bypass-immune ASK for .sh/.py/.exe/.dll etc.
- **Expanded dangerous paths**: added 20+ credential files (.kube/config,
  .aws/credentials, .docker/config.json, SSH private keys, .gnupg/*) and 4
  directories (.kube, .aws, .docker, .gnupg) to the safety layer
- **awk removed from read-only allowlist**: `awk` can execute arbitrary
  commands via `system()` and write files

### Changed
- **`exception` package removed**: all types consolidated into `errors/`; no
  more split hierarchy

### Fixed
- Data race in `a2a/grpc` `Server.Close` vs the Listen accept loop
- Watchdog timer race in device tests
- `TextBlock` JSON serialization in the werewolves example
- `/static/` prefix path resolution for CSS/JS assets in webui

## [v2.0.5] - 2026-08-04

### Added
- **Ported 25 features from AgentScope Python**, including:
  - **5 workspace backends**: Kubernetes (`workspace/k8s.go`), OpenSandbox,
    Daytona, Apple Container, Bubblewrap
  - **Gemini TTS** (`tts/gemini.go`): text-to-speech via the Gemini
    generateContent API with audio response modality; default model
    `gemini-2.5-flash-preview-tts`
  - **Document parsers** (`rag/parser/`): Parser interface with 5
    implementations — `TextParser`, `PDFParser`, `WordParser`, `ExcelParser`,
    `PPTParser` — all producing `rag.Document` slices with configurable text
    chunking and overlap
  - **Multi-tenant RBAC** (`access/`): four permission levels
    (`none`/`read`/`write`/`admin`) across four resource kinds
    (`credential`/`agent`/`knowledge_base`/`session`); principals can be
    users, groups, or organizations; pluggable `Store` interface
  - **Component Hub** (`hub/`): `Hub` interface for browsing, searching, and
    installing MCP tools and skills from remote registries; `MCPHub`,
    `SkillHub` adapters; multi-hub `Registry` for unified search
  - **More retrieval and storage backends**: Elasticsearch, Milvus, and
    MongoDB indexes (`rag/`); SQL storage backend (`storage/sql.go`);
    cursor-based pagination for `list_messages`
  - **Model cards**: Kimi K3 (256K context, 64K output, vision + video +
    thinking), qwen3.7-plus (131K context, thinking), GLM-5.2 (131K context)
  - **`OnCheckPermission` middleware hook**: wraps the permission check before
    tool execution; enables custom authorization, audit logging, or dynamic
    permission policies (middleware hook count 6 → 7)
- **Six Go-native capabilities**:
  - **Deterministic Replay** (`replay/`): record/replay middleware capturing
    model-call request/response pairs into JSON tapes; replay without calling
    the LLM for offline CI/CD testing
  - **Hot-reload Config** (`hotreload/`): polling-based file watcher with
    generic `Reloader[T]` and atomic pointer swap; JSON/YAML/TOML parsers
  - **WASM Sandbox** (`wasm/`): execute WebAssembly modules with strict
    resource limits (memory, time, instruction count/fuel); auto-discovers
    `wasmtime`, `wasmer`, or `wasm3` CLI runtimes
  - **gRPC-style TCP A2A transport** (`a2a/grpc/`): bidirectional
    agent-to-agent communication over newline-delimited JSON on TCP; `Server`
    + `Client` with streaming support
  - **Agent Load Testing** (`bench/`): scenario-based benchmarking with
    configurable concurrency, duration, and ramp-up; throughput, latency
    percentiles (p50/p95/p99), and error breakdowns
  - **Generic Pool with backpressure**: bounded queue (`ErrPoolFull`) and
    atomic `PoolStats`, building on the fan-out `AgentPool` introduced in
    v2.0.4

## [v2.0.4] - 2026-07-13

### Added
- **v3 production infrastructure layer**: `protocol`, `loop`, `runtime`,
  `metrics`, `tracing`, `sandbox`, `platform` packages; universal agent loop
  with state machine and event streaming
- **Coding agent infrastructure**: fan-out `AgentPool`, `agenttest` mock-model
  harness, project/settings config, MCP stdio server, CostTracker + metrics
  middleware, prompt composer/providers, resilience primitives (circuit
  breaker, rate limiter, model wrapper)
- **6 new built-in tools (15 total)**: MultiEdit, ApplyPatch, WebFetch, Spawn,
  LSP, Notebook
- **Tool backend routing**: read/write/edit tools route through a configured
  workspace backend (`tool.WithBackend`), closing sandbox isolation for file
  tools
- **AgentManager** for subagent lifecycle; **SkillManager**; Windows safety
  checks; `InMemoryProvider`
- **Anthropic prompt caching** (opt-in) + prompt-cache token accounting in
  loop events and budget
- **JSON-Schema tool input validation**
- **Prometheus metrics provider** + optional `/metrics` endpoint
- **OpenAI TTS** (`tts/openai.go`): OpenAI Audio Speech API
  (`/v1/audio/speech`) with streaming support
- **Streaming error propagation**: `ChatResponse` carries `Error` and
  `StopReason`
- **Production hardening (first pass)**: WebFetch SSRF guard (blocks
  loopback/private/link-local incl. cloud metadata), MCP subprocess env
  isolation, per-tool timeout + result cap, opt-in workspace-root jail for
  file tools, token and duration budgets on the loop path, atomic file writes,
  HTTP server hardening (`ReadHeaderTimeout`/`IdleTimeout`/body caps) +
  `/healthz` + `/readyz` + graceful shutdown, credential redaction in URLs and
  logs, OTEL span attributes and per-label metrics, fuzz targets + coverage
  step in CI
- Gemini schema sanitizer (const→enum, removes `$ref`/`$schema`/
  `additionalProperties`), JSON repair utility for truncated tool-call inputs,
  Ollama formatter `tool_name` field, Gemini synthetic tool-call ID generation
- STABILITY.md documenting versioning and stability tiers

### Changed
- **BREAKING**: module path migrated to `/v2` — import
  `github.com/alanfokco/agentscope-go/v2/pkg/agentscope/...`
- Deprecated `ReActAgent`; examples migrated to `UnifiedAgent`

### Fixed
- OpenAI-compatible API: use `max_completion_tokens` instead of the deprecated
  `max_tokens` wire field
- Anthropic formatter: drop empty text/thinking blocks (fixes 400 responses)
- Gemini formatter: drop empty text/thinking blocks (fixes 400 responses)
- Event-forwarder goroutine leaks and torn reads in managers/schedulers
- Kept HITL/external tools off the concurrent batch path
- 10 code-review fixes (middleware bypass, HITL backfill, safety regex, races,
  panics)
- Credential leakage via URLs and logs

### Security
- Closed the bash read-only classification bypass (write redirects are no
  longer auto-allowed) plus 8 further hardening fixes
- Removed `curl`/`wget` from the read-only bash allowlist

## [v2.0.3] - 2026-06-25

### Added
- 9 model provider adapters (OpenAI, Anthropic, DashScope, DeepSeek, Gemini,
  Ollama, Moonshot, xAI, OpenAI Responses API)
- 54 bundled model cards with context sizes, capabilities, and status
- UnifiedAgent (v2) with native API-level tool calling and streaming
- ReActAgent (v1) with text-based tool calling protocol
- 9 built-in tools: Bash, Read, Write, Edit, Glob, Grep, ResetTools,
  TaskCreate/Get/List/Update, Schedule (Create/Delete/List/View)
- Bash safety analysis with AST-level injection detection
- 6-hook middleware system (OnReply, OnReasoning, OnModelCall, OnActing,
  OnSystemPrompt, OnCompressContext)
- Per-tool middleware support
- Built-in middleware: TracingMiddleware, TTSMiddleware,
  ReplyBudgetControlMiddleware, LongTermMemoryMiddleware
- Permission engine with 5 modes (Default, AcceptEdits, Explore, Bypass,
  DontAsk)
- MCP client (Stdio + HTTP/SSE transport) with MCPTool adapter
- A2A (Agent-to-Agent) HTTP protocol
- Agent Teams with Leader/Worker coordination tools
- Pipeline with Then/If combinators + MsgHub for multi-agent routing
- Embedding models (OpenAI, DashScope, Gemini, Ollama) with batch processing
  and caching
- RAG with InMemoryIndex and Qdrant vector store
- TTS (DashScope standard + CosyVoice realtime)
- Audio caption streaming (PCM to WAV) for OpenAI/DashScope omni models
- Context compression with structured summaries and tool result truncation
- Workspace sandboxing (Local, Docker, E2B)
- HTTP Agent Service with REST + SSE streaming + AG-UI protocol
- InMemory + File + Redis storage backends
- InMemory + Redis message bus with registry operations
- Scheduled task execution (one-shot and recurring)
- Configurable ID factory (GenerateID/SetIDFactory)
- ClientOptions for all model providers (Timeout, DefaultHeaders, Transport)
- mem0 REST API memory store adapter
- SubagentHitlProjector with durable storage and 5 event types
- 25 examples covering all major features
