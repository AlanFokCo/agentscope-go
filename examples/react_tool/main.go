package main

import (
	"context"
	"fmt"
	"os"

	as "github.com/alanfokco/agentscope-go/pkg/agentscope"
	asagent "github.com/alanfokco/agentscope-go/pkg/agentscope/agent"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/memory"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

// This example demonstrates basic usage of ReActAgent with tools.

func main() {
	as.Init()

	cm, err := loadChatModelFromEnv()
	if err != nil {
		fmt.Println("load chat model error:", err)
		return
	}

	// Define a simple sum tool.
	sumTool := &tool.Tool{
		Name:        "sum_numbers",
		Description: "sum a list of numbers",
		Execute: func(ctx context.Context, args map[string]any) (any, error) {
			_ = ctx
			raw, ok := args["numbers"]
			if !ok {
				return nil, fmt.Errorf("numbers is required")
			}
			list, ok := raw.([]any)
			if !ok {
				return nil, fmt.Errorf("numbers must be array")
			}
			var total float64
			for _, v := range list {
				if n, ok := v.(float64); ok {
					total += n
				}
			}
			return map[string]any{"result": total}, nil
		},
	}

	tk := tool.NewToolkit(sumTool)
	mem := memory.NewInMemoryStore()

	sysPrompt := "You are a helpful assistant. " +
		"When the user asks for a calculation, " +
		"respond with JSON {\"tool\":\"sum_numbers\",\"args\":{\"numbers\":[...]}}."

	react := asagent.NewReActAgent("assistant", sysPrompt, cm, tk, mem)

	ctx := context.Background()
	userQuestion := "Please use the tool to calculate the sum of [1, 2, 3.5] and tell me the result."
	reply, err := react.Reply(ctx, userQuestion)
	if err != nil {
		fmt.Println("ReActAgent error:", err)
		return
	}

	if txt := reply.GetTextContent("\n"); txt != nil {
		fmt.Println("final answer:", *txt)
	} else if s, ok := reply.Content.(string); ok {
		fmt.Println("final answer:", s)
	}
}

// Reuse the model selection logic from the simple example.
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


