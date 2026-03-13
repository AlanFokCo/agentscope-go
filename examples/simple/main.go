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

// SimpleAgent demonstrates how to create an Agent in Go and call a ChatModel.
type SimpleAgent struct {
	agent.AgentBase
	model model.ChatModel
}

func NewSimpleAgent(m model.ChatModel) *SimpleAgent {
	return &SimpleAgent{
		AgentBase: agent.NewAgentBase(),
		model:     m,
	}
}

func (a *SimpleAgent) Reply(ctx context.Context, args ...any) (*message.Msg, error) {
	var userText string
	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			userText = s
		}
	}
	if userText == "" {
		userText = "Hello from Go AgentScope!"
	}

	userMsg := message.NewMsg("user", message.RoleUser, userText)
	resp, err := a.model.Chat(ctx, []*message.Msg{userMsg})
	if err != nil {
		return nil, err
	}
	// Print the reply to stdout.
	_ = a.Print(ctx, resp.Msg)
	return resp.Msg, nil
}

func (a *SimpleAgent) Observe(ctx context.Context, msgs []*message.Msg) error {
	// No-op for this simple example.
	_ = ctx
	_ = msgs
	return nil
}

func main() {
	as.Init()

	m, err := loadChatModelFromEnv()
	if err != nil {
		fmt.Println("load chat model error:", err)
		return
	}

	ctx := context.Background()
	ag := NewSimpleAgent(m)
	_, err = ag.Reply(ctx, "Introduce yourself in one sentence. (Go AgentScope example)")
	if err != nil {
		fmt.Println("agent error:", err)
	}
}

// loadChatModelFromEnv picks the LLM backend based on environment variables.
// Priority: Anthropic > DashScope > OpenAI. Supported variables:
//   ANTHROPIC_API_KEY
//   DASHSCOPE_API_KEY (optional DASHSCOPE_BASE_URL)
//   OPENAI_API_KEY
func loadChatModelFromEnv() (model.ChatModel, error) {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return model.NewAnthropicChatModel(model.AnthropicConfig{
			APIKey:          key,
			Model:           "claude-3-opus-20240229",
			MaxOutputTokens: 1024,
		})
	}
	if key := os.Getenv("DASHSCOPE_API_KEY"); key != "" {
		base := os.Getenv("DASHSCOPE_BASE_URL")
		return model.NewDashScopeChatModel(model.DashScopeConfig{
			APIKey:  key,
			BaseURL: base,
			Model:   "qwen-plus",
		})
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return model.NewOpenAIChatModel(model.OpenAIConfig{
			APIKey: key,
			Model:  "gpt-4o-mini",
		})
	}
	return nil, fmt.Errorf("please set one of ANTHROPIC_API_KEY, DASHSCOPE_API_KEY, or OPENAI_API_KEY")
}


