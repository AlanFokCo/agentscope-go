# Examples

26 runnable examples in `examples/`. Run any with:

```bash
export DASHSCOPE_API_KEY=sk-...  # or ANTHROPIC_API_KEY / OPENAI_API_KEY
go run ./examples/<name>
```

## Agent Basics

| Example | Description | API Key Required |
|---------|-------------|------------------|
| `simple` | Minimal custom agent with a single chat call | Yes |
| `agent_v2` | UnifiedAgent with native API tool calling (get_weather) | Yes |
| `streaming` | Real-time streaming via `ReplyStream` + event channel | Yes |
| `react_tool` | UnifiedAgent with a custom sum_numbers FunctionTool | Yes |
| `react_builtin_tools` | UnifiedAgent with enhanced built-in toolkit (bash, read, write, edit, glob, grep) | Yes |

## Model API

| Example | Description | API Key Required |
|---------|-------------|------------------|
| `model_call` | Raw model API: streaming + two-round tool calling + structured output | Yes |
| `structured_output` | Force JSON Schema-compliant output via GenerateStructuredOutput | Yes |
| `multi_provider` | Model card queries across all 9 providers + capability inspection | Yes |
| `multimodal` | Image input via URL and Base64 DataBlock | Yes (vision model) |
| `multiagent` | Multi-agent conversation (Alice/Bob) with moderator summary | Yes |
| `multiagent_multimodal` | Multi-agent conversation with shared image | Yes (vision model) |
| `openai_response` | OpenAI Responses API: call + tools + structured output | OPENAI_API_KEY |

## Infrastructure

| Example | Description | API Key Required |
|---------|-------------|------------------|
| `middleware` | Custom logging middleware (model call + tool execution timing) | Yes |
| `permission` | Permission engine demo: Explore / Default / Bypass modes | Yes |
| `tracing` | OpenTelemetry-style tracing with nested spans | Yes |
| `agent_loop` | v3 agent loop with MetricsHook and InMemoryProvider telemetry | Yes |
| `embedding` | Text embedding vectors + cosine similarity matrix | Yes |
| `long_term_memory` | Cross-session memory middleware with 3 modes | Yes |
| `rag_react` | RAG with in-memory document index + knowledge base | Yes |

## Multi-Agent & Orchestration

| Example | Description | API Key Required |
|---------|-------------|------------------|
| `pipeline_multi_agent` | Sequential Pipeline + MsgHub for agent orchestration | Yes |
| `agent_team` | Leader/Worker team with message routing + broadcast | Yes |
| `mcp` | MCP client: tool discovery + remote tool execution | Yes |
| `a2a_http` | Agent-to-Agent communication over HTTP | Yes |

## Deployment

| Example | Description | API Key Required |
|---------|-------------|------------------|
| `agent_service` | HTTP Agent Service with REST + SSE streaming endpoints | Yes |
| `scheduled_task` | One-shot and recurring task scheduling | No |
| `realtime_echo` | Realtime streaming interface with echo client | No |
