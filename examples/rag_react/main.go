package main

import (
	"context"
	"fmt"
	"os"

	as "github.com/alanfokco/agentscope-go/pkg/agentscope"
	asagent "github.com/alanfokco/agentscope-go/pkg/agentscope/agent"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/memory"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/rag"
)

// This example demonstrates ReActAgent combined with a simple RAG setup.

func main() {
	as.Init()

	cm, err := loadChatModelFromEnv()
	if err != nil {
		fmt.Println("load chat model error:", err)
		return
	}

	// Build an in-memory index and insert several documents.
	idx := rag.NewInMemoryIndex()
	docs := []rag.Document{
		{ID: "1", Content: "Go is a statically typed, compiled programming language."},
		{ID: "2", Content: "AgentScope-Go is a Go framework for building multi-agent LLM applications."},
	}
	if err := idx.AddDocuments(context.Background(), docs); err != nil {
		panic(err)
	}

	kb := rag.NewSimpleKnowledgeBase("docs", idx)

	mem := memory.NewInMemoryStore()

	sysPrompt := "You are an expert on Go and multi-agent frameworks. You may reference information from the knowledge base when answering."
	react := asagent.
		NewReActAgent("assistant", sysPrompt, cm, nil, mem).
		WithKnowledge(kb).
		WithCompression(&memory.CompressionConfig{
			Enable:          true,
			TriggerMessages: 10,
			KeepRecent:      6,
		})

	ctx := context.Background()
	userQuestion := "What is agentscope-go? Please briefly introduce it."
	_, err = react.Reply(ctx, userQuestion)
	if err != nil {
		fmt.Println("ReActAgent error:", err)
	}
}

// Keep the same model selection logic as other examples.
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


