# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [v2.0.3] - 2025-06-25

### Added
- 9 model provider adapters (OpenAI, Anthropic, DashScope, DeepSeek, Gemini, Ollama, Moonshot, xAI, OpenAI Responses API)
- 51 bundled model cards with context sizes, capabilities, and status
- UnifiedAgent (v2) with native API-level tool calling and streaming
- ReActAgent (v1) with text-based tool calling protocol
- 10 built-in tools: Bash, Read, Write, Edit, Glob, Grep, ResetTools, TaskCreate/Get/List/Update
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
