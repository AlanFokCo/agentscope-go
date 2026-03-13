# 从 Python AgentScope 迁移到 agentscope-go

本文概述如何将使用 Python AgentScope 的多智能体应用迁移到 Go 版本 `agentscope-go`，重点放在**概念对齐**而非逐行翻译。

## 1. 包与模块映射

- Python `agentscope` 顶层模块 → Go `pkg/agentscope` 顶层包：
  - `agentscope.agent` → `github.com/alanfokco/agentscope-go/pkg/agentscope/agent`
  - `agentscope.message` → `.../message`
  - `agentscope.model` → `.../model`
  - `agentscope.tool` → `.../tool`
  - `agentscope.memory` → `.../memory`
  - `agentscope.session` → `.../session`
  - `agentscope.pipeline` → `.../pipeline`
  - `agentscope.rag` → `.../rag`
  - `agentscope.tracing` → `.../tracing`
  - `agentscope.realtime` → `.../realtime`
  - `agentscope.a2a` → `.../a2a`
  - `agentscope.tts` → `.../tts`
  - `agentscope.tune` → `.../tune`
  - `agentscope.types` → `.../types`

## 2. 初始化与配置

- Python:

```python
import agentscope as ags
ags.init(project=\"demo\", name=\"run1\")
```

- Go:

```go
import as \"github.com/alanfokco/agentscope-go/pkg/agentscope\"

as.Init(
    as.WithProject(\"demo\"),
    as.WithName(\"run1\"),
)
log := as.Logger()
_ = log
```

## 3. 消息与内容块

- Python `Msg` 与内容块在 Go 中对应 `message.Msg` 与若干 Block 结构体：
  - `TextBlock`、`ThinkingBlock`、`ToolUseBlock`、`ToolResultBlock`、`ImageBlock`、`AudioBlock`、`VideoBlock`。
  - 常用方法在 Go 中对应：
    - `GetTextContent` ↔ `(*Msg).GetTextContent`
    - `get_content_blocks` ↔ `(*Msg).GetContentBlocks`
    - `has_content_blocks` ↔ `(*Msg).HasContentBlocks`

## 4. Agent 抽象与 ReActAgent / UserAgent

- Python:

```python
from agentscope.agent import ReActAgent, UserAgent
from agentscope.model import OpenAIChatModel

model = OpenAIChatModel(...)
react = ReActAgent(name=\"assistant\", sys_prompt=\"...\", model=model, ...)
user = UserAgent(name=\"user\")
```

- Go：

```go
import (
    asagent \"github.com/alanfokco/agentscope-go/pkg/agentscope/agent\"
    \"github.com/alanfokco/agentscope-go/pkg/agentscope/model\"
    \"github.com/alanfokco/agentscope-go/pkg/agentscope/memory\"
    \"github.com/alanfokco/agentscope-go/pkg/agentscope/tool\"
)

cm, _ := model.NewOpenAIChatModel(model.OpenAIConfig{
    APIKey: os.Getenv(\"OPENAI_API_KEY\"),
    Model:  \"gpt-4o-mini\",
})
tk := tool.NewToolkit(/* tools... */)
mem := memory.NewInMemoryStore()

react := asagent.NewReActAgent(\"assistant\", \"...\", cm, tk, mem)
user  := asagent.NewUserAgent(\"user\", nil) // 终端输入
```

调用方式从 Python 的 `await agent.reply(...)` 变为 Go 的同步：

```go
ctx := context.Background()
reply, err := react.Reply(ctx, \"你好，介绍一下你自己。\")
_ = reply
_ = err
```

## 5. 模型适配与 Tool Calling

- 所有聊天模型在 Go 中都实现 `model.ChatModel` 接口：
  - `Chat(ctx, msgs, opts...) (*ChatResponse, error)`
  - `ChatStream(ctx, msgs, opts...) (ChatStream, error)`（不支持流式时返回 `ErrStreamNotSupported`）。
- 对应 Python 的 Tool Calling，可在 Go 中使用 `tool.Tool` + `tool.Toolkit` 与 `ReActAgent` 的工具流程：
  - 模型根据 prompt 返回 `ToolUseBlock` 或约定 JSON；
  - `ReActAgent` 解析为工具调用，执行 `Tool.Execute`，再将结果注入上下文继续对话。

## 6. Memory 与 RAG

- 短期记忆：`memory.Store` + `InMemoryStore`；
- 长期/知识库：`rag.Index` + `rag.KnowledgeBase`：

```go
idx := rag.NewInMemoryIndex()
// idx.AddDocuments(ctx, docs)
kb := rag.NewSimpleKnowledgeBase(\"kb\", idx)
react.WithKnowledge(kb)
```

迁移时，将 Python 中对 `KnowledgeBase` 的使用映射到 Go 的 `rag.KnowledgeBase` 即可。

## 7. Tracing / Realtime / A2A / TTS / Tune

- Tracing：使用 `tracing.Tracer` 接口与 `tracing.SetupTracing`，可注入基于 OTEL 的实现；
- Realtime：使用 `realtime.Client` 与 `Stream` 接口构建实时 Agent；
- A2A：使用 `a2a.Client`（例如 `HTTPClient`）与 `agent.A2AAgent` 将本地调用转发到远程服务；
- TTS：`tts.Model` 接口，按需绑定任意 TTS 后端；
- Tune：`tune.Tuner` 抽象外部调优服务。

## 8. Pipeline 与 MsgHub

- Python 的 `MsgHub` 和 Pipeline 编排，在 Go 中对应于 `pipeline.MsgHub` 与 `pipeline.Pipeline`：

```go
hub := pipeline.NewMsgHub()
hub.Register(\"user\", user)
hub.Register(\"assistant\", react)

p := pipeline.New().
    Then(func(c *pipeline.Context) error {
        // 调用 user 获取输入
        msg, err := hub.RequestReply(c.Ctx, \"assistant\", \"user\", nil)
        if err != nil {
            return err
        }
        c.Messages = append(c.Messages, msg)
        return nil
    }).
    Then(func(c *pipeline.Context) error {
        last := c.Messages[len(c.Messages)-1]
        reply, err := hub.RequestReply(c.Ctx, \"user\", \"assistant\", last)
        if err != nil {
            return err
        }
        c.Messages = append(c.Messages, reply)
        return nil
    })
```

## 9. 实践建议

- **接口优先**：始终面向 `agent.Agent`、`model.ChatModel`、`session.Session`、`rag.KnowledgeBase`、`tracing.Tracer` 等接口编程，具体实现（OpenAI、pgvector、OTEL 等）通过构造函数和 Option 注入。
- **分步迁移**：
  1. 先迁移核心逻辑：消息格式、Agent 行为、模型调用；
  2. 再补工具、RAG、Memory 压缩；
  3. 最后接入 tracing、realtime、a2a、tts、tune 等高级功能。

