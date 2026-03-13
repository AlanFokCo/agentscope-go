package main

import (
	"context"
	"fmt"
	"os"

	as "github.com/alanfokco/agentscope-go/pkg/agentscope"
	asagent "github.com/alanfokco/agentscope-go/pkg/agentscope/agent"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tracing"
)

// This example shows how to enable LoggerTracer and add basic trace logs around Agent and model calls.

type TracedAgent struct {
	asagent.AgentBase
	model model.ChatModel
}

func NewTracedAgent(m model.ChatModel) *TracedAgent {
	return &TracedAgent{
		AgentBase: asagent.NewAgentBase(),
		model:     m,
	}
}

func (a *TracedAgent) Reply(ctx context.Context, args ...any) (*message.Msg, error) {
	ctx, end := tracing.TracerInstance().StartSpan(ctx, "TracedAgent.Reply")
	defer end()

	var userText string
	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			userText = s
		}
	}
	if userText == "" {
		userText = "Hello with tracing!"
	}
	userMsg := message.NewMsg("user", message.RoleUser, userText)

	ctx, endModel := tracing.TracerInstance().StartSpan(ctx, "ChatModel.Chat")
	defer endModel()

	resp, err := a.model.Chat(ctx, []*message.Msg{userMsg})
	if err != nil {
		return nil, err
	}
	_ = a.Print(ctx, resp.Msg)
	return resp.Msg, nil
}

func (a *TracedAgent) Observe(ctx context.Context, msgs []*message.Msg) error {
	_ = ctx
	_ = msgs
	return nil
}

func main() {
	as.Init()

	// Enable simple logger-based tracing.
	tracing.SetupTracing(tracing.LoggerTracer{Logger: as.Logger()})

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("please set OPENAI_API_KEY before running this example")
		return
	}

	cm, err := model.NewOpenAIChatModel(model.OpenAIConfig{
		APIKey: apiKey,
		Model:  "gpt-4o-mini",
	})
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	ag := NewTracedAgent(cm)
	_, err = ag.Reply(ctx, "Briefly explain what tracing is useful for.")
	if err != nil {
		fmt.Println("agent error:", err)
	}
}

