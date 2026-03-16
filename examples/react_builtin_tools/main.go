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

// This example demonstrates ReActAgent with built-in tools:
// execute_shell_command and view_text_file.
func main() {
	as.Init()

	cm, err := loadChatModelFromEnv()
	if err != nil {
		fmt.Println("load chat model error:", err)
		return
	}

	// Use built-in toolkit with execute_shell_command and view_text_file.
	tk := tool.NewBuiltinToolkit()
	mem := memory.NewInMemoryStore()

	sysPrompt := `You are a helpful assistant with access to tools.
- execute_shell_command: run shell commands. Args: {"command": "cmd", "timeout": 30}
- view_text_file: read text files. Args: {"path": "/path/to/file"}
When the user asks to run a command or read a file, respond with JSON:
{"tool":"execute_shell_command","args":{"command":"..."}} or
{"tool":"view_text_file","args":{"path":"..."}}`

	react := asagent.NewReActAgent("assistant", sysPrompt, cm, tk, mem)

	ctx := context.Background()
	// Example: list current directory
	userQuestion := "Run 'ls -la' in the current directory and tell me what files you see."
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
