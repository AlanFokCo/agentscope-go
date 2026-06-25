# Model Providers

agentscope-go supports 9 LLM providers, all implementing the `ChatModel` interface with `Chat`, `ChatStream`, and `CountTokens`.

## Configuration Pattern

Every provider follows the same pattern:

```go
cm, err := model.NewXxxChatModel(&model.XxxConfig{
    APIKey: "...",
    Model:  "model-name",
    // Optional:
    ClientOptions: &model.ClientOptions{
        Timeout:        120 * time.Second,
        DefaultHeaders: map[string]string{"X-Custom": "value"},
        Transport:      customTransport, // for proxy
    },
})
```

## Providers

### OpenAI

```go
cm, _ := model.NewOpenAIChatModel(&model.OpenAIConfig{
    APIKey: os.Getenv("OPENAI_API_KEY"),
    Model:  "gpt-4o",
})
```

Models: `gpt-4o`, `gpt-4o-mini`, `gpt-4.1`, `gpt-4.1-mini`, `gpt-4.1-nano`, `gpt-5.4`, `gpt-5.5`, `gpt-audio-mini`, `o3`, `o4-mini`

### OpenAI Responses API

```go
cm, _ := model.NewOpenAIResponseModel(&model.OpenAIResponseConfig{
    APIKey:          os.Getenv("OPENAI_API_KEY"),
    Model:           "gpt-4.1",
    ReasoningEffort: "high",
})
```

Uses the Responses API (`/v1/responses`) instead of Chat Completions. Default timeout is 5 minutes for long reasoning tasks.

### Anthropic

```go
cm, _ := model.NewAnthropicChatModel(&model.AnthropicConfig{
    APIKey:          os.Getenv("ANTHROPIC_API_KEY"),
    Model:           "claude-sonnet-4-20250514",
    MaxOutputTokens: 4096,
})
```

Models: `claude-opus-4-5`, `claude-opus-4-6`, `claude-opus-4-7`, `claude-opus-4-8`, `claude-sonnet-4-5`, `claude-sonnet-4-6`, `claude-haiku-4-5`

Extended thinking: `model.WithThinking(true, 10000)` enables thinking with a 10K token budget.

### DashScope (Alibaba Qwen)

```go
cm, _ := model.NewDashScopeChatModel(&model.DashScopeConfig{
    APIKey:  os.Getenv("DASHSCOPE_API_KEY"),
    BaseURL: os.Getenv("DASHSCOPE_BASE_URL"), // optional override
    Model:   "qwen-plus",
})
```

Models: `qwen-long`, `qwen-plus`, `qwen3.5-omni-plus`, `qwen3.5-plus`, `qwen3.6-max-preview`, `qwen3.6-plus`, `qwen3.7-max`

### DeepSeek

```go
cm, _ := model.NewDeepSeekChatModel(&model.DeepSeekConfig{
    APIKey: os.Getenv("DEEPSEEK_API_KEY"),
    Model:  "deepseek-chat",
})
```

Models: `deepseek-chat`, `deepseek-reasoner`, `deepseek-v4-flash`, `deepseek-v4-pro`

### Google Gemini

```go
cm, _ := model.NewGeminiChatModel(&model.GeminiConfig{
    APIKey: os.Getenv("GEMINI_API_KEY"),
    Model:  "gemini-2.5-flash",
})
```

Models: `gemini-2.5-flash`, `gemini-2.5-pro`, `gemini-3-flash-preview`, `gemini-3.1-pro-preview`

### Ollama (Local)

```go
cm, _ := model.NewOllamaChatModel(&model.OllamaConfig{
    BaseURL: "http://localhost:11434", // default
    Model:   "llama4",
})
```

No API key required. Models: `deepseek-r1-14b`, `llama4`, `qwen3-14b`, `qwen3.5-9b`

### Moonshot (Kimi)

```go
cm, _ := model.NewMoonshotChatModel(&model.MoonshotConfig{
    APIKey: os.Getenv("MOONSHOT_API_KEY"),
    Model:  "kimi-k2.6",
})
```

Models: `kimi-k2.5`, `kimi-k2.6`, `moonshot-v1-8k`, `moonshot-v1-32k`, `moonshot-v1-128k`

### xAI (Grok)

```go
cm, _ := model.NewXAIChatModel(&model.XAIConfig{
    APIKey: os.Getenv("XAI_API_KEY"),
    Model:  "grok-3",
})
```

Models: `grok-3`, `grok-3-fast`, `grok-3-mini`, `grok-4.3`

## Common Call Options

All providers support the same `CallOption` set:

```go
resp, _ := cm.Chat(ctx, msgs,
    model.WithTemperature(0.7),
    model.WithMaxTokens(1024),
    model.WithTopP(0.9),
    model.WithTools(toolSchemas),
    model.WithToolChoice(&model.ToolChoice{Mode: "auto"}),
    model.WithThinking(true, 5000),
    model.WithReasoningEffort("high"),
    model.WithVoice("alloy"),
    model.WithRetries(3, time.Second),
)
```

## Fallback Model

Automatic failover from a primary to a backup provider:

```go
cm := model.NewFallbackChatModel(primaryModel, fallbackModel)
```

## Structured Output

Force any model to produce JSON matching a schema:

```go
result, _ := model.GenerateStructuredOutput(ctx, cm, msgs, jsonSchema)
```

## Model Cards

Query bundled model metadata:

```go
card, _ := model.GetModelCard("claude-sonnet-4-6")
// card.Name, card.Provider, card.ContextSize, card.OutputSize

models := model.ListModels()                          // all 51
models = model.ListModels(model.WithProvider("openai")) // filter by provider
```
