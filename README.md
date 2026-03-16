# agentscope-go

A Go implementation of a multi-agent LLM application framework, inspired by the
Python project [AgentScope](https://github.com/agentscope-ai/agentscope). It
provides Go-idiomatic APIs (interfaces, `context.Context`, explicit `error`
returns) while keeping the same core concepts: agents, messages, models,
tools, memory, RAG, tracing, realtime, A2A, etc.

## Features

- `pkg/agentscope/config.go`: global initialization `agentscope.Init`, config and logging.
- `pkg/agentscope/message`: `Msg` and multi-modal content blocks.
- `pkg/agentscope/agent`: `Agent` interface, `AgentBase`, `ReActAgent`, `UserAgent`, `A2AAgent`.
- `pkg/agentscope/model`: `ChatModel` interface with adapters for OpenAI, Anthropic and DashScope.
- `pkg/agentscope/session`: in-memory and JSON file sessions.
- `pkg/agentscope/tool`: tool registration and execution, per-agent `Toolkit`.
- `pkg/agentscope/memory`: short-term memory store and simple compression configuration.
- `pkg/agentscope/pipeline`: sequential Pipeline + `MsgHub` for multi-agent orchestration.
- `pkg/agentscope/rag` / `tracing` / `a2a` / `realtime` / `tts` / `tune`:
  interfaces and basic reference implementations.

## Dependencies

The Go module is defined in `go.mod` and only depends on a few small libraries:

- `github.com/google/uuid` for ID generation.
- Standard library packages (`net/http`, `encoding/json`, etc.).

LLM backends require API keys:

- OpenAI: `OPENAI_API_KEY`
- Anthropic: `ANTHROPIC_API_KEY`
- DashScope (Qwen): `DASHSCOPE_API_KEY` (optional `DASHSCOPE_BASE_URL`)

All examples compile with `go 1.22` or newer.

## Getting Started: create your first Agent

The minimal example is in `examples/simple/main.go`:

```bash
export OPENAI_API_KEY=sk-...
go run ./examples/simple
```

This will:

1. Call `agentscope.Init()` to initialize global configuration and logging.
2. Build a `ChatModel` using `loadChatModelFromEnv` (OpenAI / Anthropic / DashScope).
3. Create a `SimpleAgent` that embeds `agent.AgentBase` and holds the `ChatModel`.
4. Call `Reply(ctx, "Introduce yourself in one sentence.")` and print the result.

### Inline usage example

If you prefer to see everything in one file, here is a minimal inline example
using the OpenAI HTTP model adapter:

```go
package main

import (
	"context"
	"fmt"
	"os"

	as "github.com/alanfokco/agentscope-go/pkg/agentscope"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/agent"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
)

type SimpleAgent struct {
	agent.AgentBase
	Model model.ChatModel
}

func (a *SimpleAgent) Reply(ctx context.Context, args ...any) (*message.Msg, error) {
	userMsg := message.NewMsg("user", message.RoleUser, args[0].(string))
	resp, err := a.Model.Chat(ctx, []*message.Msg{userMsg})
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

func main() {
	as.Init()

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		panic("OPENAI_API_KEY is not set")
	}
	m := model.NewOpenAIChatModel(model.OpenAIConfig{
		APIKey: apiKey,
		Model:  "gpt-4o-mini",
	})

	ag := &SimpleAgent{Model: m}
	ctx := context.Background()
	reply, err := ag.Reply(ctx, "Introduce yourself in one sentence.")
	if err != nil {
		panic(err)
	}
	if txt := reply.GetTextContent("\n"); txt != nil {
		fmt.Println("assistant:", *txt)
	}
}
```

### Using Anthropic or DashScope instead of OpenAI

You can switch providers without changing your agent code, simply by setting
different environment variables:

```bash
# Anthropic
export ANTHROPIC_API_KEY=sk-ant-...
go run ./examples/simple

# DashScope (Qwen)
export DASHSCOPE_API_KEY=sk-...
export DASHSCOPE_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
go run ./examples/simple
```

The helper `loadChatModelFromEnv` in the examples picks a backend in this order:
Anthropic → DashScope → OpenAI.

### ReAct + tool usage example

The `react_tool` example shows how to let the model call tools. The core idea is:

```go
// 1) Define a tool
sumTool := &tool.Tool{
	Name:        "sum_numbers",
	Description: "Sum a list of numbers",
	Execute: func(ctx context.Context, args map[string]any) (any, error) {
		numsAny, _ := args["numbers"].([]any)
		var sum float64
		for _, v := range numsAny {
			if n, ok := v.(float64); ok {
				sum += n
			}
		}
		return map[string]any{"result": sum}, nil
	},
}
tk := tool.NewToolkit(sumTool)

// 2) Create ReActAgent with a system prompt telling the model how to call the tool
mem := memory.NewInMemoryStore()
sysPrompt := "You are a helpful assistant. " +
	"When the user asks for a calculation, " +
	"respond with JSON {\"tool\":\"sum_numbers\",\"args\":{\"numbers\":[...]}}."
react := asagent.NewReActAgent("assistant", sysPrompt, cm, tk, mem)

// 3) Call Reply and let the agent decide whether to use the tool
reply, err := react.Reply(ctx, "Please calculate the sum of [1, 2, 3.5].")
```

### Built-in tools: execute_shell_command and view_text_file

The `tool` package provides two built-in tools usable by ReActAgent:

- **execute_shell_command**: runs shell commands. Args: `command` (string), `timeout` (optional, seconds).
- **view_text_file**: reads text file contents. Args: `path` or `file_path` (string).

```go
tk := tool.NewBuiltinToolkit()
react := asagent.NewReActAgent("assistant", sysPrompt, cm, tk, mem)
```

Or add them to an existing toolkit:

```go
tk := tool.NewToolkit(
	tool.ExecuteShellCommandTool(),
	tool.ViewTextFileTool(),
	myCustomTool,
)
```

See `examples/react_builtin_tools` for a full example.

### RAG + Qdrant usage sketch

For more advanced RAG scenarios, you can plug Qdrant as a vector index:

```go
client, err := qdrant.NewClient(&qdrant.Config{
	Host: "localhost",
	Port: 6334,
})
if err != nil {
	panic(err)
}

// Assume you have an Embedder implementation that calls OpenAI embeddings or another model.
embedder := myEmbedder{}

idx, err := rag.NewQdrantTextIndex(rag.QdrantTextConfig{
	Client:        client,
	Collection:    "agentscope_docs",
	VectorMetaKey: "vector",
	Embedder:      embedder,
})
if err != nil {
	panic(err)
}

// Insert documents (vectors are computed internally via the Embedder)
docs := []rag.Document{
	{ID: "doc-1", Content: "agentscope-go is a Go multi-agent framework."},
}
_ = idx.AddDocuments(ctx, docs)

kb := rag.NewSimpleKnowledgeBase("docs", idx)
// Then pass kb into ReActAgent.WithKnowledge(kb) as shown in examples/rag_react.
```

## More Advanced Examples

All examples are in the `examples/` directory:

- `examples/simple`: single Agent + ChatModel.
- `examples/react_tool`: `ReActAgent` with a custom tool (`tool.Toolkit`).
- `examples/react_builtin_tools`: `ReActAgent` with built-in tools (`execute_shell_command`, `view_text_file`).
- `examples/rag_react`: `ReActAgent` with RAG (`rag.KnowledgeBase`) and memory compression.
- `examples/tracing`: enable `tracing.LoggerTracer` to log spans.
- `examples/a2a_http`: `A2AAgent` + `a2a.HTTPClient` calling a local HTTP server Agent.
- `examples/realtime_echo`: `RealtimeAgent` using `realtime.EchoClient`.
- `examples/pipeline_multi_agent`: multi-agent orchestration with `pipeline.Pipeline` + `MsgHub`.

Run any example with:

```bash
go run ./examples/<name>
```

Make sure the corresponding API keys are set when an example depends on an LLM backend.

## Migrating from Python AgentScope

See `docs/migration_from_python.md` for a side-by-side mapping between Python
AgentScope modules/APIs and the Go `pkg/agentscope` equivalents, along with
concrete migration suggestions.

