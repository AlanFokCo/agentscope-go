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
  <a href="README_zh.md">中文</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/license-Apache--2.0-blue" alt="License" />
  <img src="https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go" alt="Go 1.22+" />
  <a href="https://pkg.go.dev/github.com/alanfokco/agentscope-go/pkg/agentscope"><img src="https://pkg.go.dev/badge/github.com/alanfokco/agentscope-go/pkg/agentscope.svg" alt="Go Reference" /></a>
</p>

---

AgentScope Go is the Go implementation of the [AgentScope](https://github.com/agentscope-ai/agentscope) multi-agent LLM framework. It provides Go-idiomatic APIs — interfaces, `context.Context`, explicit `error` returns, functional options — while maintaining full feature parity with the Python project.

## Highlights

### Smart Agents, Full Control

AgentScope adopts the ReAct (Reasoning-Acting) paradigm, enabling agents to autonomously plan and execute complex tasks. Agents dynamically decide which tools to use and when, adapting to changing requirements in real-time.

Autonomy without control is a liability in production. AgentScope provides:

- **Safe Interruption** — Pause agent execution at any point while preserving full context and tool state
- **Human-in-the-Loop** — Inject corrections or guidance at any reasoning step via the event system (`RequireUserConfirm` / `RequireExternalExecution`)
- **Permission Engine** — 5 permission modes (Default, AcceptEdits, Explore, Bypass, DontAsk) with per-tool rule matching and bypass-immune safety checks
- **Context Compression** — Automatic structured summarization when context exceeds configurable thresholds, with tool result truncation and offloading

### Built-in Tools

Production-ready tools out of the box:

- **Bash / Read / Write / Edit / Glob / Grep** — Full coding agent toolkit with safety analysis (AST-level injection detection, dangerous path protection, read-only command recognition)
- **Task Management** — `task_create`, `task_get`, `task_list`, `task_update` with bidirectional dependency tracking
- **Structured Output** — `GenerateStructuredOutput` forces JSON Schema-compliant responses via synthetic tool calls with automatic retry
- **Long-term Memory** — Cross-session memory middleware with 3 modes (static, agent-controlled, both), backed by vector similarity search or mem0 REST API
- **RAG** — `Index` / `KnowledgeBase` interfaces with in-memory and Qdrant vector store backends

### 9 Model Providers

All with `Chat`, `ChatStream` (SSE), `CountTokens`, and native tool calling:

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

51 model cards with context sizes, capabilities, and status are bundled via `//go:embed`.

Additional model features: `FallbackChatModel` (automatic primary→fallback failover), `ClientOptions` (custom HTTP timeout/headers/transport for proxy and enterprise), extended thinking with budget tokens, audio caption streaming (PCM→WAV).

### Seamless Integration

- **MCP Protocol** — Full MCP client (Stdio + HTTP/SSE transport) with automatic tool discovery and `MCPTool` adapter
- **A2A Protocol** — Agent-to-Agent HTTP communication via `A2AAgent` + `HTTPClient`
- **Agent Teams** — Leader/Worker coordination with `TeamCreate`, `AgentCreate`, `TeamSay`, `TeamDelete` tools and cross-session HITL event projection
- **Pipeline & MsgHub** — Sequential pipeline with `Then`/`If` combinators and multi-agent message routing

### Production Grade

- **Middleware System** — 5-hook onion chain (`OnReply`, `OnModelCall`, `OnActing`, `OnSystemPrompt`, `OnCompressContext`) + per-tool middleware. Built-in: tracing, TTS, budget control, long-term memory
- **Observability** — `TracingMiddleware` with OpenTelemetry semantic conventions, nested spans (invoke_agent → chat → execute_tool)
- **Workspace Sandboxing** — `LocalWorkspace`, `DockerWorkspace`, `E2BWorkspace` with `Backend` abstraction for isolated tool execution
- **Agent Service** — HTTP service with REST + SSE streaming, session management, credential CRUD, scheduled tasks, AG-UI protocol support
- **Embedding** — 4 providers (OpenAI, DashScope, Gemini, Ollama) with batch processing, caching, and multimodal support
- **TTS** — DashScope TTS (standard + CosyVoice realtime) with streaming WAV output via `TTSMiddleware`

## Quick Start

**Requirements:** Go 1.22+

```bash
go get github.com/alanfokco/agentscope-go/pkg/agentscope
```

```bash
export DASHSCOPE_API_KEY=sk-...   # or ANTHROPIC_API_KEY / OPENAI_API_KEY
go run ./examples/agent_v2
```

### Agent with Tool Calling

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"

    as "github.com/alanfokco/agentscope-go/pkg/agentscope"
    "github.com/alanfokco/agentscope-go/pkg/agentscope/agent"
    "github.com/alanfokco/agentscope-go/pkg/agentscope/model"
    "github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
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

### Multimodal (Image Input)

```go
msgs := []*message.Msg{
    message.NewMsg("user", message.RoleUser, []message.ContentBlock{
        message.TextBlock{Type: "text", Text: "What's in this image?"},
        message.DataBlock{Type: "data", ID: "img-1", Source: message.URLSource{
            Type: "url", URL: "https://example.com/photo.jpg", MediaType: "image/jpeg",
        }},
    }),
}
resp, _ := cm.Chat(ctx, msgs)
```

### Middleware

```go
type TimingMiddleware struct { middleware.BaseMiddleware }

func (m *TimingMiddleware) OnModelCall(ctx context.Context, input middleware.ModelCallInput, next middleware.ModelCallHandler) (*model.ChatResponse, error) {
    start := time.Now()
    resp, err := next(ctx, input)
    log.Printf("model call took %v", time.Since(start))
    return resp, err
}

a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithMiddlewares(&TimingMiddleware{
        BaseMiddleware: middleware.BaseMiddleware{MiddlewareKey: "timing"},
    }),
)
```

## Examples

25 examples in `examples/`. Run any with `go run ./examples/<name>`.

| Example | Description |
|---------|-------------|
| **Agent Basics** | |
| `simple` | Minimal agent + single chat call |
| `agent_v2` | UnifiedAgent with native API tool calling |
| `streaming` | Real-time streaming via `ReplyStream` + event channel |
| `react_tool` | Legacy ReActAgent with custom tool |
| `react_builtin_tools` | ReActAgent with built-in Bash/Read tools |
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
| `embedding` | Text embedding + cosine similarity matrix |
| `long_term_memory` | Cross-session memory middleware (3 modes) |
| `rag_react` | RAG with in-memory index + knowledge base |
| **Multi-Agent & Orchestration** | |
| `pipeline_multi_agent` | Pipeline + MsgHub orchestration |
| `agent_team` | Leader/Worker team with message routing |
| `mcp` | MCP client: tool discovery + remote execution |
| `a2a_http` | Agent-to-Agent over HTTP |
| **Deployment** | |
| `agent_service` | HTTP Agent Service (REST + SSE streaming) |
| `scheduled_task` | One-shot and recurring task scheduling |
| `realtime_echo` | Realtime streaming interface demo |

## Architecture

```
pkg/agentscope/
├── config.go              # Init, logging, ID factory
├── agent/                 # Agent interface + UnifiedAgent, ReActAgent, UserAgent, A2AAgent
├── model/                 # ChatModel interface + 9 provider adapters + 51 model cards
├── tool/                  # Tool interface + FunctionTool + 10 built-in tools + safety analysis
├── message/               # Msg + ContentBlock (text, thinking, tool_call, tool_result, data, hint)
├── event/                 # 28 event types for streaming lifecycle
├── middleware/             # 5-hook onion chain + tracing, TTS, budget, memory middleware
├── formatter/             # Per-provider message formatting (9 formatters × chat + multiagent)
├── permission/            # 5 modes + Engine + Checker + Rule matching
├── pipeline/              # Pipeline (Then/If) + MsgHub (multi-agent routing)
├── credential/            # 9 provider credential types + auto-detect from env
├── mcp/                   # MCP client (Stdio + HTTP) + MCP server
├── embedding/             # 4 providers + batch + cache + multimodal
├── rag/                   # Index + KnowledgeBase + Qdrant integration
├── team/                  # Agent teams with leader/worker coordination tools
├── workspace/             # Local + Docker + E2B sandboxed execution
├── storage/               # InMemory + File + Redis storage backends
├── service/               # HTTP agent service + SSE + AG-UI protocol
├── tts/                   # DashScope TTS + CosyVoice realtime
├── tracing/               # Tracer interface + OTel + LoggerTracer
├── schedule/              # InMemoryScheduler for periodic tasks
├── session/               # Session KV store (memory + JSON file)
├── skill/                 # Reusable skill system
├── memory/                # Conversation memory + compression
├── messagebus/            # InMemory + Redis pub/sub + registry
└── internal/              # httpx (HTTP+SSE helper), jsonx (repair)
```

## Migrating from Python

See [`docs/migration_from_python.md`](docs/migration_from_python.md) for a side-by-side mapping between Python AgentScope and the Go equivalents.

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](./CONTRIBUTING.md) for guidelines.

## License

Apache License 2.0 — see [LICENSE](./LICENSE) for details.

## Publications

If you find AgentScope helpful, please cite our papers:

- [AgentScope: A Flexible yet Robust Multi-Agent Platform](https://arxiv.org/abs/2402.14034)
