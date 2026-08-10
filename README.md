<p align="center">
  <img
    src="https://img.alicdn.com/imgextra/i1/O1CN01nTg6w21NqT5qFKH1u_!!6000000001621-55-tps-550-550.svg"
    alt="AgentScope Logo"
    width="200"
  />
</p>

<h3 align="center">Build Production-Ready AI Agents in Go</h3>

<p align="center">
  <a href="https://github.com/agentscope-ai/agentscope">🐍 Python</a>
  &nbsp;|&nbsp;
  <a href="https://github.com/agentscope-ai/agentscope-java">☕ Java</a>
  &nbsp;|&nbsp;
  <a href="README.es-ES.md">Español</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/license-Apache--2.0-blue" alt="License" />
  <img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go" alt="Go 1.25+" />
  <a href="https://pkg.go.dev/github.com/alanfokco/agentscope-go/v2/pkg/agentscope"><img src="https://pkg.go.dev/badge/github.com/alanfokco/agentscope-go/v2/pkg/agentscope.svg" alt="Go Reference" /></a>
</p>

---

AgentScope Go is the Go implementation of the [AgentScope](https://github.com/agentscope-ai/agentscope) multi-agent LLM framework. It provides Go-idiomatic APIs — interfaces, `context.Context`, explicit `error` returns, functional options — while delivering capabilities that go beyond the Python project.

---

## Why agentscope-go?

| Property | What it means for you |
|----------|----------------------|
| **Single binary deployment** | `go build` produces one static binary. No pip, no venv, no Docker layer for Python dependencies. Deploy to a VM, a container, or an edge device with `scp`. |
| **True concurrency** | Goroutines and channels — not `async/await`. Process thousands of concurrent agent sessions on a single node with bounded memory. The `AgentPool` applies backpressure automatically. |
| **Embeddable library** | `go get` and import into any existing Go service. No separate process, no sidecar, no IPC overhead. Your HTTP server, gRPC service, or CLI tool gains agent capabilities in-process. |
| **Production-hardened** | Compile-time type safety. No `None` at runtime, no `AttributeError` in production. Interfaces enforce contracts. Generics eliminate casting. The type system catches integration bugs before deployment. |

---

## Go-Exclusive Features

These capabilities exist only in the Go implementation. They leverage Go's concurrency primitives, type system, and compilation model.

### Web UI Studio (`webui/`)

Embedded web interface for agent interaction. Zero external dependencies — the SPA is compiled into the binary via `go:embed`. Supports streaming chat with thinking blocks, tool call visualization, human-in-the-loop confirmation, session management, and model browsing.

```go
svc := service.New(cfg, cm, factory)
handler := svc.HandlerWithWebUI(service.WebUIConfig{Enable: true})
http.ListenAndServe(":8080", handler)
// Open http://localhost:8080 in your browser
```

### Deterministic Replay (`replay/`)

Record every LLM call during a session. Replay the exact sequence in CI without API costs or network dependency.

```go
// Record
recorder := replay.NewRecorder()
a := agent.NewUnifiedAgent("bot", "...", cm, agent.WithMiddlewares(recorder))
a.Reply(ctx, "plan a trip to Tokyo")
tape := recorder.Tape()
replay.SaveTape(tape, "testdata/trip.json")

// Replay in CI (zero API calls)
tape, _ := replay.LoadTape("testdata/trip.json")
replayer := replay.NewReplayer(tape)
a := agent.NewUnifiedAgent("bot", "...", cm, agent.WithMiddlewares(replayer))
a.Reply(ctx, "plan a trip to Tokyo") // returns recorded response instantly
```

### Fan-out Agent Pool (`runtime/`)

Process N concurrent sessions with bounded worker goroutines and backpressure. Each worker owns its own agent instance — no shared mutable state.

```go
pool := runtime.NewAgentPool(
    func() agent.Agent {
        return agent.NewUnifiedAgent("worker", "...", cm, agent.WithToolkit(tk))
    },
    runtime.Workers(16),
    runtime.QueueSize(256),
)
defer pool.Close()

resultCh, _ := pool.Submit(ctx, "Summarize this document...")
result := <-resultCh
fmt.Println(result.Output.GetTextContent("\n"))
```

### Hot-Reload Config (`hotreload/`)

Zero-downtime configuration updates with typed generics. File changes are detected by polling; the new config is atomically swapped in.

```go
type AgentCfg struct {
    Model       string  `json:"model"`
    Temperature float64 `json:"temperature"`
    MaxTokens   int     `json:"max_tokens"`
}

watcher := hotreload.NewWatcher(hotreload.WatcherConfig{PollInterval: 2 * time.Second})
reloader, _ := hotreload.NewReloader[AgentCfg](watcher, "config/agent.json",
    hotreload.WithOnChange(func(old, new_ *AgentCfg) {
        log.Printf("model changed: %s -> %s", old.Model, new_.Model)
    }),
)
watcher.Start(ctx)

// Always reads the latest config — no restart needed
cfg := reloader.Get()
```

### WASM Sandbox (`wasm/`)

Execute untrusted tool code in a WASM sandbox. Sub-second cold start (vs Docker's multi-second overhead). Memory-limited, time-bounded, filesystem-isolated.

```go
rt, _ := wasm.AutoDiscover() // finds wasmtime/wasmer/wasm3 in PATH
sandbox := wasm.NewSandbox(wasm.SandboxConfig{
    Runtime:     rt,
    MaxMemory:   64 * 1024 * 1024, // 64MB
    MaxDuration: 5 * time.Second,
})

result, _ := sandbox.Run(ctx, "tools/transform.wasm", inputJSON)
fmt.Println(string(result.Stdout))
```

### TCP Agent Mesh (`a2a/grpc/`)

Low-latency bidirectional agent communication over TCP with newline-delimited JSON. Agents connect to a mesh server and exchange typed messages with streaming support.

```go
server := grpc.NewServer(":9090", func(msg *grpc.Message) *grpc.Message {
    // Route or process inter-agent messages
    return &grpc.Message{From: "router", To: msg.From, Payload: responseJSON}
})
server.Start(ctx)

// Client side
client := grpc.NewClient("localhost:9090", "agent-alpha")
client.Send(ctx, &grpc.Message{To: "agent-beta", Method: "analyze", Payload: data})
resp, _ := client.Receive(ctx)
```

### Agent Load Testing (`bench/`)

Built-in load testing framework with P50/P95/P99 latency reporting. Define scenarios with configurable concurrency, ramp-up, and duration.

```go
runner := bench.NewRunner()
report, _ := runner.Run(ctx, &bench.Scenario{
    Name:        "rag-query-load",
    Concurrency: 20,
    Duration:    30 * time.Second,
    Run: func(ctx context.Context, iter int) error {
        _, err := agent.Reply(ctx, queries[iter%len(queries)])
        return err
    },
})
fmt.Printf("P50=%v P95=%v P99=%v throughput=%.1f/s\n",
    report.Latencies.P50, report.Latencies.P95, report.Latencies.P99, report.Throughput)
```

---

## Full Feature Set

### 9 Model Providers

All providers support `Chat`, `ChatStream` (SSE), `CountTokens`, and native tool calling:

| Provider | Constructor | Example Models |
|----------|-------------|----------------|
| **OpenAI** | `model.NewOpenAIChatModel` | gpt-4o, gpt-4.1, gpt-5.5, o3, o4-mini |
| **OpenAI Responses** | `model.NewOpenAIResponseModel` | gpt-4.1, o3 (Responses API) |
| **Anthropic** | `model.NewAnthropicChatModel` | claude-opus-4-8, claude-sonnet-4-6 |
| **DashScope** | `model.NewDashScopeChatModel` | qwen3.5-plus, qwen3.7-max |
| **DeepSeek** | `model.NewDeepSeekChatModel` | deepseek-chat, deepseek-v4-pro |
| **Google Gemini** | `model.NewGeminiChatModel` | gemini-2.5-pro, gemini-3.1-pro |
| **Ollama** | `model.NewOllamaChatModel` | llama4, qwen3-14b (local) |
| **Moonshot** | `model.NewMoonshotChatModel` | kimi-k2.6, moonshot-v1-128k |
| **xAI** | `model.NewXAIChatModel` | grok-3, grok-4.3 |

54 model cards with context sizes, capabilities, and status are bundled via `//go:embed`.

Additional model features: `FallbackChatModel` (automatic primary→fallback failover), `ClientOptions` (custom HTTP timeout/headers/transport), extended thinking with budget tokens, audio caption streaming (PCM→WAV).

### 8 Workspace Backends

Isolated execution environments for tool sandboxing:

| Backend | Package | Notes |
|---------|---------|-------|
| **Local** | `workspace/local.go` | Direct filesystem execution |
| **Docker** | `workspace/docker.go` | Container-based isolation |
| **E2B** | `workspace/e2b.go` | Cloud sandbox (e2b.dev) |
| **Apple Container** | `workspace/applecontainer.go` | macOS native lightweight VM |
| **Bubblewrap** | `workspace/bubblewrap.go` | Linux user-namespace sandbox (bwrap) |
| **Daytona** | `workspace/daytona.go` | Daytona workspace API |
| **OpenSandbox** | `workspace/opensandbox.go` | OpenSandbox cloud environment |
| **Kubernetes** | `workspace/k8s.go` | Pod-based execution in K8s clusters |

### 5 RAG Vector Stores

| Store | File | Notes |
|-------|------|-------|
| **InMemory** | `rag/rag.go` | Zero-dependency, suitable for small corpora |
| **Qdrant** | `rag/qdrant_index.go` | Production vector DB with filtering |
| **Elasticsearch** | `rag/elasticsearch.go` | Full-text + vector hybrid search |
| **MongoDB** | `rag/mongodb.go` | Atlas Vector Search |
| **Milvus** | `rag/milvus.go` | High-performance vector DB |

### 5 Document Parsers

| Format | File | Notes |
|--------|------|-------|
| **Plain Text** | `rag/parser/text.go` | UTF-8 text with configurable chunking |
| **PDF** | `rag/parser/pdf.go` | Text extraction from PDF documents |
| **Word** | `rag/parser/word.go` | .docx parsing |
| **Excel** | `rag/parser/excel.go` | .xlsx sheet extraction |
| **PowerPoint** | `rag/parser/ppt.go` | .pptx slide text extraction |

### 4 Storage Backends

| Backend | File | Notes |
|---------|------|-------|
| **InMemory** | `storage/storage.go` | Fast, ephemeral |
| **File** | `storage/full_storage.go` | JSON file persistence |
| **Redis** | `storage/redis.go` | Distributed, TTL support |
| **SQL** | `storage/sql.go` | PostgreSQL/MySQL/SQLite via `database/sql` |

### Hub System

Unified registry for installable components:

| Component | Description |
|-----------|-------------|
| **MCP Hub** | Browse, search, and install MCP servers from a remote registry |
| **Skill Hub** | Discover and install reusable agent skills |
| **Registry** | Multi-hub aggregation with unified search across sources |

### Access Control (`access/`)

Resource sharing across users, groups, and organizations:

- 4 permission levels: None, Read, Write, Admin
- 3 principal types: User, Group, Org
- 4 resource kinds: Credential, Agent, KnowledgeBase, Session
- Policy-based checker with ownership shortcut
- `ListAccessible` for permission-filtered resource discovery

### 7 Middleware Hooks

Onion-chain architecture — each hook wraps the next in the chain:

| Hook | Purpose |
|------|---------|
| `OnReply` | Wraps the entire reply lifecycle (outermost) |
| `OnReasoning` | Wraps each reasoning step in the ReAct loop |
| `OnModelCall` | Wraps each model API call |
| `OnActing` | Wraps each tool execution |
| `OnSystemPrompt` | Transforms the system prompt (pipeline mode) |
| `OnCompressContext` | Wraps context compression |
| `OnCheckPermission` | Wraps permission checks for tool calls |

Built-in middleware: TracingMiddleware, TTSMiddleware, ReplyBudgetControlMiddleware, LongTermMemoryMiddleware, CostTrackerMiddleware, MetricsMiddleware.

### 3 TTS Providers

| Provider | Features |
|----------|----------|
| **DashScope** | Standard + CosyVoice realtime streaming |
| **OpenAI** | OpenAI TTS API with streaming WAV output |
| **Gemini** | Google Gemini TTS |

### Built-in Tools

Production-ready coding agent toolkit:

- **Bash / Read / Write / Edit / Glob / Grep** — Full filesystem + shell with AST-level injection detection, dangerous path protection, read-only command recognition
- **Task Management** — `task_create`, `task_get`, `task_list`, `task_update` with bidirectional dependency tracking
- **Structured Output** — `GenerateStructuredOutput` forces JSON Schema-compliant responses via synthetic tool calls with automatic retry
- **Long-term Memory** — Cross-session memory middleware with 3 modes (static, agent-controlled, both), backed by vector similarity search or mem0 REST API

### Agent Architecture

- **ReAct Loop** — Autonomous reasoning-acting with configurable max iterations
- **Safe Interruption** — Pause execution at any point, preserving full context
- **Human-in-the-Loop** — Inject corrections via event system (`RequireUserConfirm` / `RequireExternalExecution`)
- **Permission Engine** — 5 permission modes with per-tool rule matching and bypass-immune safety checks
- **Context Compression** — Automatic structured summarization when context exceeds thresholds

### Security & Execution Safety

- **AST-level Bash Analysis** — `mvdan.cc/sh/v3/syntax`-based analysis: injection risk, dangerous removal, redirect safety, read-only verification, sed constraints, file path extraction
- **Interpreter Attack Detection** — Blocks dangerous API calls hidden inside `python -c`, `node -e`, `perl -e`, `ruby -e`, `lua -e`, `php -r` (8 languages, 20+ patterns)
- **Process-group Isolation** — Child processes killed as a group on timeout (`Setpgid` + `SIGKILL` to pgid), preventing orphans and fork-bombs
- **Sandbox Policy Enforcement** — `sandbox.Policy` controls: FSReadOnly blocks writes, AllowExec=false blocks bash, NetDisabled blocks WebFetch, DenyPaths blocks file access
- **Write Hardening** — 10 MB size cap, atomic writes (temp+fsync+rename), executable-extension bypass-immune ASK
- **SSRF Guard** — Dial-time IP resolution blocks loopback/private/link-local addresses (covers DNS rebinding + redirects)
- **Workspace Jail** — Symlink-aware path confinement to workspace root
- **Credential Protection** — 40+ dangerous file paths protected (.kube/config, .aws/credentials, .docker/config.json, SSH keys, .gnupg/*)
- **Audit Logging** — Structured `audit.Logger` records every tool execution, permission decision, and policy denial (InMemory/File/Multi/Nop backends)

### Integration Protocols

| Protocol | Description |
|----------|-------------|
| **MCP** | Full MCP client (Stdio + HTTP/SSE) with automatic tool discovery |
| **A2A HTTP** | Agent-to-Agent over HTTP via `A2AAgent` + `HTTPClient` |
| **A2A gRPC/TCP** | Low-latency bidirectional mesh (TCP + newline-delimited JSON) |
| **AG-UI** | Agent service protocol for frontend integration |
| **Agent Teams** | Leader/Worker coordination with cross-session HITL event projection |
| **Pipeline & MsgHub** | Sequential `Then`/`If` combinators + multi-agent message routing |

### Observability & Operations

- **Tracing** — `TracingMiddleware` with OpenTelemetry semantic conventions, nested spans
- **Metrics** — `Counter`/`Histogram` interfaces with `InMemoryProvider`, `Prometheus` provider, and `MetricsHook`
- **Audit** — `audit.Logger` interface with InMemory/File(JSON-Lines)/Multi/Nop backends; records tool executions, permission decisions, and sandbox policy denials
- **Sandbox Events** — `tool_exec_start`, `tool_exec_end`, `tool_policy_denied` events for execution-layer visibility
- **Budget Tracking** — Turn/token/duration/concurrency limits with `BudgetTracker`
- **Resilience** — Circuit breaker + rate limiter wrappers for `ChatModel`
- **Embedding** — 4 providers (OpenAI, DashScope, Gemini, Ollama) with batch processing, caching, multimodal support
- **Cross-Platform** — Shell detection with PowerShell/Cmd safety analysis, Windows support

### Edge & Embedded Intelligence

Designed for deploying AI agents on edge devices (Jetson, RPi, RISC-V) with intermittent or no connectivity:

| Component | Description |
|-----------|-------------|
| **ConnectivityAwareModel** | Wraps any `ChatModel`; routes to cloud when online, falls back to local (Ollama) when offline, auto-recovers via circuit breaker |
| **PubSub Interface** | `messagebus.PubSub` with QoS/retain semantics for IoT protocols |
| **MQTT Adapter** | Eclipse Paho-based implementation (build tag `mqtt`) with auto-reconnect |
| **Device Connectors** | Serial (UART), GPIO (chardev), CAN (SocketCAN), I2C — all pure Go, no CGO |
| **DeviceTool** | Wraps hardware as `tool.Tool` with permission model (sensors auto-allow, actuators require ASK) |
| **SensorMiddleware** | Injects live sensor readings into system prompt with token budget control |
| **Watchdog** | Timer-based safety: triggers safe-state if agent loop stalls |

Cross-compiles to arm64/arm/mips64le/riscv64. Stripped binary ~6MB.

---

## Quick Start

**Requirements:** Go 1.25+

```bash
go get github.com/alanfokco/agentscope-go/v2/pkg/agentscope
```

```bash
export DASHSCOPE_API_KEY=sk-...   # or ANTHROPIC_API_KEY / OPENAI_API_KEY
go run ./examples/agent_v2
```

### Minimal Agent with Tool Calling

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"

    as "github.com/alanfokco/agentscope-go/v2/pkg/agentscope"
    "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/agent"
    "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
    "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
)

func main() {
    as.Init()

    cm, _ := model.NewDashScopeChatModel(model.DashScopeConfig{
        APIKey: "sk-...", Model: "qwen-plus",
    })

    weatherTool := tool.NewFunctionTool(
        "get_weather", "Get current weather for a city",
        json.RawMessage(`{
            "type": "object",
            "properties": {"location": {"type": "string"}},
            "required": ["location"]
        }`),
        func(ctx context.Context, input map[string]any) (any, error) {
            return map[string]any{"temp": "22°C", "condition": "sunny"}, nil
        },
    )

    a := agent.NewUnifiedAgent("assistant", "You are a weather bot.", cm,
        agent.WithToolkit(tool.NewToolkit(weatherTool)),
        agent.WithReactConfig(agent.ReactConfig{MaxIters: 5}),
    )

    reply, _ := a.Reply(context.Background(), "What's the weather in Shanghai?")
    if txt := reply.GetTextContent("\n"); txt != nil {
        fmt.Println(*txt)
    }
}
```

### Streaming

```go
ch, _ := a.ReplyStream(ctx, "Tell me a story.")
for evt := range ch {
    switch e := evt.(type) {
    case event.TextBlockDeltaEvent:
        fmt.Print(e.Delta)
    case event.ReplyEndEvent:
        fmt.Println()
    }
}
```

### Custom Middleware

```go
type TimingMiddleware struct { middleware.BaseMiddleware }

func (m *TimingMiddleware) OnModelCall(ctx context.Context, input *middleware.ModelCallInput, next middleware.ModelCallHandler) (*model.ChatResponse, error) {
    start := time.Now()
    resp, err := next(ctx, input)
    log.Printf("[%s] model call: %v", input.ModelName, time.Since(start))
    return resp, err
}

a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithMiddlewares(&TimingMiddleware{
        BaseMiddleware: middleware.BaseMiddleware{MiddlewareKey: "timing"},
    }),
)
```

---

## Architecture

```
pkg/agentscope/
├── agent/                  # Agent interface + UnifiedAgent, UserAgent, A2AAgent
├── model/                  # ChatModel interface + 9 providers + 54 model cards
├── tool/                   # Tool interface + FunctionTool + 17 built-in tools + safety analysis
├── message/                # Msg + ContentBlock (text, thinking, tool_call, tool_result, data, hint)
├── event/                  # 30 event types for streaming lifecycle
├── middleware/             # 7-hook onion chain + tracing, TTS, budget, memory, metrics, cost
├── formatter/              # Per-provider message formatting (9 formatters)
├── permission/             # 5 modes + Engine + Checker + Rule matching
├── pipeline/               # Pipeline (Then/If) + MsgHub (multi-agent routing)
├── credential/             # 9 provider credential types + auto-detect from env
│
├── replay/                 # Deterministic record/replay of LLM calls
├── runtime/                # AgentPool, SessionEngine, AgentManager, BudgetTracker, Harness
├── hotreload/              # Typed generic config reloader with file watching
├── wasm/                   # WASM sandbox (wasmtime/wasmer/wasm3 backends)
├── bench/                  # Load testing framework with P50/P95/P99 reporting
├── a2a/                    # A2A protocol types + HTTP client
├── a2a/grpc/               # TCP transport: bidirectional agent mesh
│
├── hub/                    # MCP Hub + Skill Hub + Registry (multi-hub aggregation)
├── access/                 # Resource sharing: users/groups/orgs with 4 permission levels
├── workspace/              # 8 backends: Local, Docker, E2B, Apple, Bubblewrap, Daytona, OpenSandbox, K8s
├── rag/                    # Index + KnowledgeBase + 5 vector stores
├── rag/parser/             # 5 document parsers: Text, PDF, Word, Excel, PPT
├── storage/                # 4 backends: InMemory, File, Redis, SQL
├── tts/                    # 3 providers: DashScope, OpenAI, Gemini
├── embedding/              # 4 providers + batch + cache + multimodal
│
├── audit/                  # Structured audit logging (InMemory/File/Multi/Nop)
├── mcp/                    # MCP client (Stdio + HTTP) + MCP server
├── team/                   # Agent teams with leader/worker coordination
├── service/                # HTTP agent service + SSE + AG-UI protocol
├── webui/                  # Embedded web UI (go:embed SPA)
├── tracing/                # Tracer interface + OTel + LoggerTracer
├── metrics/                # Counter/Histogram + InMemoryProvider + MetricsHook
├── resilience/             # Circuit breaker + rate limiter for ChatModel
├── loop/                   # Configurable agent loop (model → tool → iterate)
├── memory/                 # Conversation memory + compression
├── messagebus/             # InMemory + Redis pub/sub + registry
├── messagebus/mqtt/        # MQTT PubSub adapter for edge/IoT (build tag: mqtt)
├── device/                 # Hardware connectors (Serial/GPIO/CAN/I2C) + DeviceTool + Watchdog
├── session/                # Session KV store (memory + JSON file)
├── skill/                  # Reusable skill system + SkillManager registry
├── prompt/                 # Composable system prompt assembly
├── schedule/               # InMemoryScheduler for periodic tasks
├── realtime/               # Realtime streaming interface
├── sandbox/                # Execution policies (Allow/Deny/AskUser)
├── platform/               # Cross-platform shell detection + safety
├── logging/                # Structured logging handlers
├── protocol/               # LoopState, LoopEvent — shared types
├── errors/                 # Typed error hierarchy (Retriable, Throttled, PermissionDenied)
├── config/                 # Configuration loading
├── app/                    # Application bootstrap
├── tune/                   # Model tuning utilities
├── types/                  # Shared type definitions
├── agenttest/              # Test helpers and mocks
├── exception/              # Exception handling
└── internal/               # fsutil (atomic writes), httpsec (SSRF guard), httpx (HTTP+SSE), jsonx (repair)
```

---

## Examples

41 examples in `examples/`. Run any with `go run ./examples/<name>`.

| Example | Description |
|---------|-------------|
| **Agent Basics** | |
| `simple` | Minimal agent + single chat call |
| `agent_v2` | UnifiedAgent with native API tool calling |
| `streaming` | Real-time streaming via `ReplyStream` + event channel |
| `react_tool` | UnifiedAgent with custom FunctionTool |
| `react_builtin_tools` | UnifiedAgent with enhanced built-in toolkit (bash, read, write, edit, glob, grep) |
| **Model API** | |
| `model_call` | Raw model API: streaming + two-round tool calling + structured output |
| `structured_output` | Force JSON Schema-compliant output via `GenerateStructuredOutput` |
| `multi_provider` | Model card queries + 9-provider switching |
| `multimodal` | Image input via URL and Base64 `DataBlock` |
| `multiagent` | Multi-agent conversation with moderator summary |
| `multiagent_multimodal` | Multi-agent + shared image input |
| `openai_response` | OpenAI Responses API (call + tools + structured output) |
| **Infrastructure** | |
| `middleware` | Custom logging middleware (model call + tool execution hooks) |
| `permission` | Permission engine: Explore / Default / Bypass modes |
| `tracing` | OpenTelemetry-style tracing with nested spans |
| `agent_loop` | v3 agent loop with MetricsHook and InMemoryProvider |
| `embedding` | Text embedding + cosine similarity matrix |
| `long_term_memory` | Cross-session memory middleware (3 modes) |
| `rag_react` | RAG with in-memory index + knowledge base |
| **Multi-Agent & Orchestration** | |
| `pipeline_multi_agent` | Pipeline + MsgHub orchestration |
| `agent_team` | Leader/Worker team with message routing |
| `mcp` | MCP client: tool discovery + remote execution |
| `a2a_http` | Agent-to-Agent over HTTP |
| **Go-Exclusive** | |
| `replay` | Record LLM calls, replay in CI without API costs |
| `agent_pool` | Fan-out agent pool with backpressure |
| `hotreload` | Zero-downtime config updates with typed `Reloader[T]` |
| `wasm_sandbox` | WASM tool sandbox with memory/time limits |
| `grpc_a2a` | TCP agent mesh with bidirectional streaming |
| `bench` | Agent load testing with P50/P95/P99 latency |
| `hub_install` | Browse and install MCP servers/skills from hub |
| `access_control` | Resource sharing across users/groups/orgs |
| `document_parser` | Parse PDF/Word/Excel/PPT into RAG chunks |
| `audit_logging` | Sandbox policy enforcement + structured audit trail |
| **Deployment** | |
| `agent_service` | HTTP Agent Service (REST + SSE streaming) |
| `webui` | Web UI Studio with streaming chat, tool visualization, HITL |
| `scheduled_task` | One-shot and recurring task scheduling |
| `realtime_echo` | Realtime streaming interface demo |
| **Edge & IoT** | |
| `edge_offline` | ConnectivityAwareModel — automatic cloud/local fallback |
| `edge_sensor` | SensorMiddleware + Watchdog for physical sensors |
| `edge_serial_robot` | DeviceTool with serial robot arm control |
| `edge_fleet` | Multi-agent PubSub coordination across devices |

---

## Comparison with Python

Factual comparison of features available in each implementation:

| Capability | Go | Python |
|------------|:--:|:------:|
| **Deployment** | Single static binary | pip + venv + dependencies |
| **Concurrency model** | Goroutines (OS-thread-multiplexed) | asyncio (single-thread event loop) |
| **Type safety** | Compile-time (interfaces + generics) | Runtime (type hints optional) |
| **Deterministic replay** | Yes (`replay/`) | No |
| **Agent pool with backpressure** | Yes (`runtime/AgentPool`) | No |
| **Hot-reload config** | Yes (`hotreload/Reloader[T]`) | No |
| **WASM tool sandbox** | Yes (`wasm/`) | No |
| **TCP agent mesh** | Yes (`a2a/grpc/`) | No |
| **Built-in load testing** | Yes (`bench/`) | No |
| **Embedded Web UI** | Yes (`webui/`) | No |
| **Hub system (MCP + Skill)** | Yes (`hub/`) | Partial (registry only) |
| **Access control** | Yes (`access/`) | No |
| **Document parsers** | 5 formats (Text/PDF/Word/Excel/PPT) | External (LangChain loaders) |
| **Workspace backends** | 8 | 3 (Local/Docker/E2B) |
| **Vector stores** | 5 (InMemory/Qdrant/ES/MongoDB/Milvus) | 3 (InMemory/Qdrant/ChromaDB) |
| **Storage backends** | 4 (InMemory/File/Redis/SQL) | 2 (InMemory/File) |
| **TTS providers** | 3 (DashScope/OpenAI/Gemini) | 1 (DashScope) |
| **Model providers** | 9 | 9 |
| **MCP support** | Client + Server | Client + Server |
| **A2A protocol** | HTTP + TCP mesh | HTTP only |
| **Middleware hooks** | 7 | 5 |
| **OpenTelemetry tracing** | Yes | Yes |
| **Circuit breaker / rate limiter** | Yes (`resilience/`) | No |
| **Cross-platform (Windows)** | Yes (`platform/`) | Partial |
| **Embedding support** | 4 providers | 4 providers |
| **Agent teams** | Yes | Yes |
| **Pipeline / MsgHub** | Yes | Yes |
| **Edge/IoT device support** | Yes (`device/` + MQTT PubSub) | No |

---

## Documentation

Detailed documentation is available in the [`docs/`](docs/) directory:

- [Getting Started](docs/getting-started.md) — Installation, first agent, environment setup
- [Architecture](docs/architecture.md) — Package structure, core concepts, data flow
- [Model Providers](docs/model-providers.md) — Configure 9 LLM providers with examples
- [Tools](docs/tools.md) — Built-in tools, custom functions, permissions
- [Middleware](docs/middleware.md) — 7-hook system, tracing, budget, memory
- [Examples](docs/examples.md) — Full catalog of 36+ runnable examples
- [Deployment](docs/deployment.md) — HTTP service, sandboxing, production checklist
- [Edge Deployment](docs/edge-deployment.md) — Cross-compile, Jetson/RPi quickstart, offline operation
- [Device Tools](docs/device-tools.md) — Serial/GPIO/CAN/I2C connectors, DeviceTool, Watchdog
- [Multi-Device](docs/multi-device.md) — Fleet coordination via MQTT PubSub
- [Offline Operation](docs/offline-operation.md) — ConnectivityAwareModel, data buffering, power management

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](./CONTRIBUTING.md) for guidelines.

## License

Apache License 2.0 — see [LICENSE](./LICENSE) for details.

## Publications

If you find AgentScope helpful, please cite our papers:

- [AgentScope: A Flexible yet Robust Multi-Agent Platform](https://arxiv.org/abs/2402.14034)
