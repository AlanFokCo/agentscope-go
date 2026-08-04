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

Models: `qwen-long`, `qwen-plus`, `qwen3.5-omni-plus`, `qwen3.5-plus`, `qwen3.6-max-preview`, `qwen3.6-plus`, `qwen3.7-max`, `qwen3.7-plus`

GLM models via DashScope: `glm-5.2` (131K context, 8K output)

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

Models: `kimi-k2.5`, `kimi-k2.6`, `kimi-k3`, `moonshot-v1-8k`, `moonshot-v1-32k`, `moonshot-v1-128k`

**Kimi K3** — Latest Kimi model with 256K context, 64K output. Supports text, vision (JPEG/PNG/GIF/WebP), video (MP4), and extended thinking.

### xAI (Grok)

```go
cm, _ := model.NewXAIChatModel(&model.XAIConfig{
    APIKey: os.Getenv("XAI_API_KEY"),
    Model:  "grok-3",
})
```

Models: `grok-3`, `grok-3-fast`, `grok-3-mini`, `grok-4.3`

## Text-to-Speech (TTS)

agentscope-go includes TTS models for audio synthesis, usable standalone or via `TTSMiddleware`.

### DashScope TTS

Standard and realtime (CosyVoice) TTS:

```go
ttsModel, _ := tts.NewDashScopeTTSModel(&tts.DashScopeConfig{
    APIKey: os.Getenv("DASHSCOPE_API_KEY"),
    Model:  "cosyvoice-v3-flash",
    Voice:  "longxiaochun",
})
audio, _ := ttsModel.Synthesize(ctx, "Hello world")
```

Models: `cosyvoice-v1`, `cosyvoice-v2`, `cosyvoice-v3-flash`, `cosyvoice-v3-plus`, `qwen3-tts-flash`, `qwen3-tts-flash-realtime`

### OpenAI TTS

```go
ttsModel, _ := tts.NewOpenAITTSModel(&tts.OpenAITTSConfig{
    APIKey: os.Getenv("OPENAI_API_KEY"),
    Model:  "tts-1-hd",
    Voice:  "alloy",
})
```

Models: `tts-1`, `tts-1-hd`

### Gemini TTS

Uses the Gemini generateContent API with audio response modality:

```go
ttsModel, _ := tts.NewGeminiTTSModel(tts.GeminiTTSConfig{
    APIKey:       os.Getenv("GEMINI_API_KEY"),
    Model:        "gemini-2.5-flash-preview-tts",
    Voice:        "Kore",
    SpeakingRate: 1.0,
})
audio, _ := ttsModel.Synthesize(ctx, "Hello from Gemini TTS")
```

Models: `gemini-2.5-flash-preview-tts`

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

models := model.ListModels()                          // all 54 chat model cards
models = model.ListModels(model.WithProvider("openai")) // filter by provider
```

54 chat/embedding model cards + 9 TTS model cards are bundled and accessible at runtime via `//go:embed`.

## Model Card Summary

| Provider | Chat Models | Embedding Models | TTS Models |
|----------|------------|------------------|------------|
| OpenAI | 10 | 2 | 2 |
| Anthropic | 7 | — | — |
| DashScope | 9 (incl. GLM-5.2) | 2 | 6 |
| DeepSeek | 4 | — | — |
| Gemini | 4 | 2 | 1 |
| Moonshot | 6 (incl. Kimi K3) | — | — |
| Ollama | 4 | — | — |
| xAI | 4 | — | — |
| **Total** | **48** | **6** | **9** |
