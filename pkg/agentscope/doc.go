// Package agentscope provides the core framework for building multi-agent LLM applications in Go.
//
// It is the Go implementation of the AgentScope project (https://github.com/agentscope-ai/agentscope),
// offering Go-idiomatic APIs while maintaining full feature parity with the Python version.
//
// # Quick Start
//
//	as.Init()
//	cm, _ := model.NewDashScopeChatModel(model.DashScopeConfig{APIKey: "sk-...", Model: "qwen-plus"})
//	a := agent.NewUnifiedAgent("bot", "You are helpful.", cm)
//	reply, _ := a.Reply(ctx, "Hello!")
//
// # Architecture
//
// The framework is organized into subpackages under pkg/agentscope/:
//
//   - agent: Agent interface and implementations (UnifiedAgent, ReActAgent, UserAgent, A2AAgent)
//   - model: ChatModel interface with 9 provider adapters
//   - tool: Tool interface with 10 built-in tools and safety analysis
//   - message: Msg type with polymorphic ContentBlock variants
//   - event: 28 event types for streaming lifecycle
//   - middleware: 5-hook onion chain for intercepting agent behavior
//   - permission: Permission engine with 5 modes for tool execution control
//   - pipeline: Sequential pipeline with MsgHub for multi-agent orchestration
//
// See the README and examples/ directory for comprehensive usage examples.
package agentscope
