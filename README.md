# agentscope-go

A Go implementation of a multi-agent LLM application framework, inspired by the
Python project [AgentScope](https://github.com/agentscope-ai/agentscope). It
provides Go-idiomatic APIs (interfaces, `context.Context`, explicit `error`
returns) while keeping the same core concepts: agents, messages, models,
tools, memory, middleware, RAG, tracing, and more.

## Architecture Overview

```
                           +-----------+
                           |   Agent   |  UnifiedAgent / ReActAgent / UserAgent / A2AAgent
                           +-----+-----+
                                 |
               +-----------------+-----------------+
               |                 |                 |
        +------+------+   +-----+-----+   +-------+-------+
        |  Middleware  |   |   Model   |   |     Tool      |
        | (5 hooks)   |   | ChatModel |   | FunctionTool  |
        +------+------+   +-----+-----+   +-------+-------+
               |                 |                 |
   +-----------+-----------+     |         +-------+-------+
   | OnReply  | OnModelCall|     |         | ToolGroup     |
   | OnActing | OnSystem   |     |         | Toolkit       |
   | OnCompress            |     |         | Built-in      |
   +-----------------------+     |         | MCP           |
                                 |         +---------------+
                        +--------+--------+
                        | OpenAI     | Anthropic
                        | DashScope  | DeepSeek
                        | Gemini     | Ollama
                        | Moonshot   | xAI
                        +--------+--------+
                                 |
               +-----------------+-----------------+
               |                 |                 |
        +------+------+   +-----+-----+   +-------+-------+
        |   Memory    |   |  Message   |   |    Event      |
        | InMemory    |   | Msg + Blocks|  | 27 event types|
        | Long-term   |   +-----+-----+   +-------+-------+
        | Compress    |         |
        +-------------+   +-----+-----+
                          | Pipeline  |
                          | MsgHub    |
                          +-----------+
```

## Features

### Core

| Package | Description |
|---------|-------------|
| `config.go` | Global `agentscope.Init()`, project config, logging (logrus) |
| `message` | `Msg` with typed `ContentBlock` variants (text, thinking, tool_use, tool_result, image, audio, video) |
| `event` | 27 event types for streaming and lifecycle tracking |
| `agent` | `Agent` interface, `UnifiedAgent` (v2, native tool calling + streaming), `ReActAgent`, `UserAgent`, `A2AAgent` |
| `model` | `ChatModel` interface (`Chat`/`ChatStream`/`CountTokens`) with 8 adapters |
| `tool` | `Tool` interface, `FunctionTool`, `ToolGroup`, `Toolkit`, 6 built-in tools |
| `memory` | Short-term `InMemoryStore`, structured context compression |
| `pipeline` | Sequential `Pipeline` with `Then`/`If` combinators, `MsgHub` for multi-agent orchestration |

### Middleware & Extensions

| Package | Description |
|---------|-------------|
| `middleware` | 5-hook onion chain: `OnReply`, `OnModelCall`, `OnActing`, `OnSystemPrompt`, `OnCompressContext` |
| `credential` | 8 credential providers (env, file, vault, keychain, etc.) |
| `formatter` | Prompt formatting and template system |
| `model/models/` | 44 YAML model cards with pricing, limits, and capabilities |

### Advanced

| Package | Description |
|---------|-------------|
| `permission` | 5 permission modes + Engine + Checker for tool execution control |
| `team` | Agent Teams with Leader/Worker pattern + 4 coordination tools |
| `mcp` | MCP client (Stdio + HTTP) with `MCPTool` adapter |
| `embedding` | Embedding models (4 providers) + `FileEmbeddingCache` |
| `rag` | `Index` / `KnowledgeBase` interfaces, Qdrant integration |
| `skill` | Reusable skill system for agents |
| `session` | In-memory and JSON file session persistence |
| `tracing` | `Tracer` interface + `TracingMiddleware`, OpenTelemetry support |

### Services

| Package | Description |
|---------|-------------|
| `workspace` | `Workspace` / `Sandbox` interfaces, `LocalWorkspace` + `Offloader` |
| `service` | HTTP Agent Service (10 endpoints + SSE + CORS) |
| `messagebus` | `MessageBus` interface + `InMemoryMessageBus` |
| `storage` | `InMemoryStorage` + `FileStorage` backends |
| `schedule` | `InMemoryScheduler` for scheduled agent tasks |

### Also included

- `a2a` / `realtime` / `tts` / `tune` / `types`: agent-to-agent HTTP, realtime streaming, TTS, fine-tuning, and shared types.

## Model Support

8 LLM provider adapters, all with `Chat` and `ChatStream` (SSE):

| Provider | Adapter | Example Models |
|----------|---------|----------------|
| OpenAI | `model.NewOpenAIChatModel` | gpt-4o, gpt-4o-mini |
| Anthropic | `model.NewAnthropicChatModel` | claude-sonnet-4, claude-haiku-4 |
| DashScope | `model.NewDashScopeChatModel` | qwen-plus, qwen-max |
| DeepSeek | `model.NewDeepSeekChatModel` | deepseek-chat, deepseek-reasoner |
| Gemini | `model.NewGeminiChatModel` | gemini-2.0-flash |
| Ollama | `model.NewOllamaChatModel` | llama3, mistral |
| Moonshot | `model.NewMoonshotChatModel` | moonshot-v1-8k |
| xAI | `model.NewXAIChatModel` | grok-2 |

LLM-backed examples need one of: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, or `DASHSCOPE_API_KEY` (+ optional `DASHSCOPE_BASE_URL`). The `loadChatModelFromEnv` helper picks a backend in the order Anthropic -> DashScope -> OpenAI.

## Getting Started

### Quick start

```bash
export OPENAI_API_KEY=sk-...   # or ANTHROPIC_API_KEY / DASHSCOPE_API_KEY
go run ./examples/agent_v2
```

### v2 Agent with tool calling

The v2 `UnifiedAgent` uses native API-level tool calling (OpenAI/Anthropic function calling), not the text-JSON parsing used by the legacy `ReActAgent`:

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

    cm, _ := model.NewOpenAIChatModel(model.OpenAIConfig{
        APIKey: "sk-...",
        Model:  "gpt-4o-mini",
    })

    weatherTool := tool.NewFunctionTool(
        "get_weather",
        "Get current weather for a city",
        json.RawMessage(`{
            "type": "object",
            "properties": {
                "location": {"type": "string", "description": "City name"}
            },
            "required": ["location"]
        }`),
        func(ctx context.Context, input map[string]any) (any, error) {
            loc, _ := input["location"].(string)
            return map[string]any{"location": loc, "temp": "22°C"}, nil
        },
    )

    a := agent.NewUnifiedAgent(
        "weather-bot",
        "You are a weather assistant. Use get_weather to look up weather.",
        cm,
        agent.WithToolkit(tool.NewToolkit(weatherTool)),
        agent.WithReactConfig(agent.ReactConfig{MaxIters: 5}),
    )

    reply, err := a.Reply(context.Background(), "What's the weather in Shanghai?")
    if err != nil {
        panic(err)
    }
    if txt := reply.GetTextContent("\n"); txt != nil {
        fmt.Println("Assistant:", *txt)
    }
}
```

### Streaming

Use `ReplyStream` to receive events as they arrive from the model:

```go
ch, _ := a.ReplyStream(ctx, "Tell me a story about a robot.")

for evt := range ch {
    switch e := evt.(type) {
    case event.TextBlockDeltaEvent:
        fmt.Print(e.Delta)
    case event.ModelCallEndEvent:
        fmt.Printf("\n[Tokens: in=%d out=%d]", e.InputTokens, e.OutputTokens)
    }
}
```

### Middleware

The middleware system uses an onion-chain pattern with 5 hooks. Embed `middleware.BaseMiddleware` and override the hooks you need:

```go
type LoggingMiddleware struct {
    middleware.BaseMiddleware
}

func (m *LoggingMiddleware) OnModelCall(
    ctx context.Context,
    input middleware.ModelCallInput,
    next middleware.ModelCallHandler,
) (*model.ChatResponse, error) {
    start := time.Now()
    resp, err := next(ctx, input)
    fmt.Printf("[%v] model call for %q\n", time.Since(start), input.AgentName)
    return resp, err
}

// Wire it up:
a := agent.NewUnifiedAgent("assistant", "...", cm,
    agent.WithMiddlewares(&LoggingMiddleware{
        BaseMiddleware: middleware.BaseMiddleware{MiddlewareKey: "logging"},
    }),
)
```

### Legacy ReActAgent

The original `ReActAgent` is still available for text-based tool calling:

```go
sumTool := &tool.Tool{
    Name:        "sum_numbers",
    Description: "Sum a list of numbers",
    Execute: func(ctx context.Context, args map[string]any) (any, error) {
        // ...
        return map[string]any{"result": sum}, nil
    },
}
react := agent.NewReActAgent("assistant", sysPrompt, cm, tool.NewToolkit(sumTool), mem)
reply, _ := react.Reply(ctx, "Calculate the sum of [1, 2, 3.5].")
```

## Examples

All 20 examples are in the `examples/` directory. Run any with `go run ./examples/<name>`.

| Example | Description |
|---------|-------------|
| `simple` | Minimal single Agent + ChatModel |
| `agent_v2` | UnifiedAgent with native tool calling (v2) |
| `streaming` | Streaming with `ReplyStream` + event handling |
| `middleware` | Custom middleware with timing/logging hooks |
| `react_tool` | Legacy ReActAgent with a custom tool |
| `react_builtin_tools` | Legacy ReActAgent with built-in shell/file tools |
| `multi_provider` | Same agent across multiple LLM providers |
| `structured_output` | Structured output with JSON schema validation |
| `pipeline_multi_agent` | Multi-agent orchestration with Pipeline + MsgHub |
| `permission` | Permission system controlling tool execution |
| `agent_team` | Agent Teams with Leader/Worker coordination |
| `mcp` | MCP client integration (Stdio/HTTP) |
| `embedding` | Embedding models and vector search |
| `long_term_memory` | Long-term memory middleware (3 modes) |
| `rag_react` | RAG with Qdrant + knowledge base |
| `tracing` | OpenTelemetry tracing with TracingMiddleware |
| `a2a_http` | Agent-to-agent HTTP communication |
| `realtime_echo` | Realtime streaming with EchoClient |
| `agent_service` | HTTP Agent Service (REST + SSE) |
| `scheduled_task` | Scheduled agent task execution |

## Migrating from Python AgentScope

See `docs/migration_from_python.md` for a side-by-side mapping between Python
AgentScope modules/APIs and the Go `pkg/agentscope` equivalents.
