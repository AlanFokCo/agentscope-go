# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]


### Added — Edge & Embedded Intelligence
- **ConnectivityAwareModel** (`model/connectivity.go`): wraps local + cloud ChatModel with internal circuit breaker; routes to cloud when online, falls back to local (Ollama) when offline, auto-recovers via single-probe half-open
- **PubSub interface** (`messagebus/pubsub.go`): minimal pub/sub contract for IoT protocols with QoS (0/1/2), retain, configurable buffer size
- **MQTT adapter** (`messagebus/mqtt/`): Eclipse Paho-based PubSub implementation with auto-reconnect, topic prefix, credentials; build tag `mqtt` to avoid bloating non-MQTT builds
- **Device framework** (`device/`): `Connector` interface + 4 pure-Go hardware drivers (Serial via termios, GPIO via chardev, CAN via SocketCAN, I2C via i2c-dev) — all `//go:build linux`, zero CGO
- **DeviceTool**: wraps any Connector as `tool.Tool`; sensors auto-allowed, actuators require bypass-immune ASK; integrated watchdog kick on success
- **SensorTool**: read-only sensor tool with JSON output and auto-allow permissions
- **SensorMiddleware**: injects live sensor readings into system prompt (`[SENSOR DATA]...[/SENSOR DATA]`) with configurable max-token budget to prevent context bloat
- **Watchdog**: timer-based safety; if `Kick()` not called within timeout, triggers safe-state callback (motor off, valve close, etc.)
- **CI cross-arch job**: build verification for linux/arm64, arm, mips64le, riscv64 with binary size check (< 18MB)
- 4 edge examples: `edge_offline`, `edge_sensor`, `edge_serial_robot`, `edge_fleet`
- 4 docs: edge-deployment.md, device-tools.md, offline-operation.md, multi-device.md
### Added — Security & Audit
- **Audit logging** (`audit/`): new package with structured `Logger` interface and 4 implementations (InMemory, File/JSON-Lines, Multi fan-out, Nop); 10 action types; context propagation via `WithLogger`/`GetLogger`
- **Sandbox execution events**: 3 new event types — `tool_exec_start`, `tool_exec_end`, `tool_policy_denied` — providing visibility into the orchestrator execution layer
- **Sandbox Policy enforcement**: `sandbox.Policy` now actually controls tool execution; `OrchestratorConfig.Policy` blocks tool calls that violate filesystem/network/process policies before execution
- **Process-group isolation** (`proc_unix.go`/`proc_windows.go`): child processes killed as a group on timeout via `Setpgid` + `SIGKILL` to process group, preventing orphans and fork-bombs
- **Interpreter attack detection** (`CheckInterpreterAttack`): detects dangerous API calls hidden inside interpreter inline-code flags (`python -c`, `node -e`, `perl -e`, `ruby -e`, `lua -e`, `php -r`); checks for `os.system`, `subprocess`, `child_process`, etc.
- **Write hardening**: 10 MB size cap (`MaxWriteBytes`), atomic writes via `fsutil.WriteFileAtomic`, executable-extension detection triggers bypass-immune ASK for .sh/.py/.exe/.dll etc.
- **Expanded dangerous paths**: added 20+ credential files (.kube/config, .aws/credentials, .docker/config.json, SSH private keys, .gnupg/*) and 4 directories (.kube, .aws, .docker, .gnupg) to the safety layer
- **awk removed from read-only allowlist**: `awk` can execute arbitrary commands via `system()` and write files

### Added — Go-Exclusive Features
- **Web UI Studio** (`webui/`): Embedded single-page web interface for agent interaction; zero-dependency SPA served via `go:embed` with streaming chat, thinking block display, tool call visualization, human-in-the-loop confirmation, session management, and model browser; mount with `service.HandlerWithWebUI` or use `webui.Handler` directly
- **Deterministic Replay** (`replay/`): Record/replay middleware that captures model call request/response pairs into JSON tapes; replay mode returns pre-recorded responses without calling the LLM, enabling fully offline deterministic CI/CD testing
- **Fan-out Agent Pool** (`runtime.AgentPool`): Worker pool pattern for high-throughput batch processing; each worker owns a fresh agent instance created from a factory function with configurable worker count and queue size
- **Hot-Reload Config** (`hotreload/`): Polling-based file watcher with `Watcher` and generic `Reloader[T]` for typed config with atomic pointer swap; supports custom parsers (JSON/YAML/TOML) and multi-file watching
- **WASM Sandbox** (`wasm/`): Execute WebAssembly modules with strict resource limits (memory, time, instruction count/fuel); auto-discovers `wasmtime`, `wasmer`, or `wasm3` CLI runtimes
- **TCP Agent Mesh** (`a2a/grpc/`): Bidirectional agent-to-agent communication over TCP using newline-delimited JSON; `Server` + `Client` types with streaming support via `IsStream`/`StreamEnd` flags
- **Agent Load Testing** (`bench/`): Benchmark runner with `Scenario` definitions supporting configurable concurrency, duration, iterations, and ramp-up; produces `Report` with throughput, latency percentiles (p50/p95/p99), and error breakdowns

### Added — Workspace Providers
- **K8s Workspace** (`workspace/k8s.go`): Execute agent tools inside ephemeral Kubernetes Pods
- **OpenSandbox Workspace** (`workspace/opensandbox.go`): Cloud sandbox via OpenSandbox API
- **Daytona Workspace** (`workspace/daytona.go`): Daytona-managed development environment sandbox
- **Apple Container Workspace** (`workspace/applecontainer.go`): macOS-native lightweight container isolation
- **Bubblewrap Workspace** (`workspace/bubblewrap.go`): Minimal Linux `bwrap` sandboxing without Docker

### Added — Model & TTS
- **Kimi K3** model card: 256K context, 64K output, vision + video + thinking support
- **qwen3.7-plus** model card: 131K context, 16K output, thinking support
- **GLM-5.2** model card: 131K context, 8K output (via DashScope)
- **Gemini TTS** (`tts/gemini.go`): Text-to-speech via Gemini generateContent API with audio response modality; default model `gemini-2.5-flash-preview-tts`
- **OpenAI TTS** (`tts/openai.go`): OpenAI Audio Speech API (`/v1/audio/speech`) with streaming support

### Added — RAG & Document Parsing
- **Document parsers** (`rag/parser/`): Parser interface with 5 implementations — `TextParser`, `PDFParser`, `WordParser`, `ExcelParser`, `PPTParser` — all producing `rag.Document` slices with configurable text chunking and overlap
- **QdrantTextIndex** (`rag/qdrant_text_index.go`): Higher-level index that auto-embeds text via an `Embedder` then persists to Qdrant

### Added — Access Control & Hub
- **Multi-tenant RBAC** (`access/`): Four permission levels (`none`/`read`/`write`/`admin`) across four resource kinds (`credential`/`agent`/`knowledge_base`/`session`); principals can be users, groups, or organizations; pluggable `Store` interface for policy persistence
- **Component Hub** (`hub/`): `Hub` interface for browsing, searching, and installing MCP tools and skills from remote registries; `MCPHub`, `SkillHub` adapters; multi-hub `Registry` for unified search

### Added — Middleware
- **`OnCheckPermission` hook**: Wraps the permission check before tool execution; enables custom authorization, audit logging, or dynamic permission policies
- **`OnReasoning` hook**: Wraps each reasoning step (ReAct iteration) for per-iteration observability, logging, or guardrails
- Middleware hook count increased from 5 to 7

### Added — Infrastructure
- OpenAI TTS model calling `/v1/audio/speech` endpoint with streaming support
- Gemini schema sanitizer: converts unsupported types, removes invalid fields, rewrites enums
- JSON repair utility for truncated tool-call inputs (closes malformed JSON instead of crashing)
- Ollama formatter: content field in tool result messages
- Gemini synthetic call ID generation for tool result matching

### Fixed
- OpenAI Chat Completions: deprecated `function_call` → `tool_choice` wire field
- Anthropic formatter: drop empty text blocks and empty thinking blocks (Anthropic rejects with 400)
- Gemini formatter: drop empty text blocks and empty thinking blocks (Gemini rejects with 400)

## [v2.0.3] - 2025-06-25

### Added
- 9 model provider adapters (OpenAI, Anthropic, DashScope, DeepSeek, Gemini, Ollama, Moonshot, xAI, OpenAI Responses API)
- 54 bundled model cards with context sizes, capabilities, and status
- UnifiedAgent (v2) with native API-level tool calling and streaming
- ReActAgent (v1) with text-based tool calling protocol
- 17 built-in tools: Bash, Read, Write, Edit, MultiEdit, ApplyPatch, Glob, Grep, WebFetch, ResetTools, Spawn, LSP, Notebook, TaskCreate/Get/List/Update, Schedule (Create/Delete/List/View)
- Bash safety analysis with AST-level injection detection
- 5-hook middleware system (OnReply, OnModelCall, OnActing, OnSystemPrompt, OnCompressContext)
- Per-tool middleware support
- Built-in middleware: TracingMiddleware, TTSMiddleware, ReplyBudgetControlMiddleware, LongTermMemoryMiddleware
- Permission engine with 5 modes (Default, AcceptEdits, Explore, Bypass, DontAsk)
- MCP client (Stdio + HTTP/SSE transport) with MCPTool adapter
- A2A (Agent-to-Agent) HTTP protocol
- Agent Teams with Leader/Worker coordination tools
- Pipeline with Then/If combinators + MsgHub for multi-agent routing
- Embedding models (OpenAI, DashScope, Gemini, Ollama) with batch processing and caching
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
