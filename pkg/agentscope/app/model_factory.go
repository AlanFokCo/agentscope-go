package app

import (
	"fmt"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/credential"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/embedding"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tts"
)

// EmbeddingModelConfig describes which embedding model to create.
type EmbeddingModelConfig struct {
	Provider     string `json:"provider"`
	CredentialID string `json:"credential_id"`
	Model        string `json:"model"`
}

// GetEmbeddingModel resolves a credential and constructs an embedding model.
func GetEmbeddingModel(config EmbeddingModelConfig, registry *credential.Registry) (embedding.EmbeddingModel, error) {
	cred, err := registry.Get(config.Provider)
	if err != nil {
		return nil, fmt.Errorf("credential %q not found: %w", config.Provider, err)
	}

	switch config.Provider {
	case "openai":
		return embedding.NewOpenAIEmbeddingModel(embedding.OpenAICompatConfig{
			APIKey:  cred.APIKey(),
			BaseURL: cred.BaseURL(),
			Model:   config.Model,
		})
	case "dashscope":
		if embedding.IsMultimodalModel(config.Model) {
			return embedding.NewDashScopeMultimodalEmbeddingModel(embedding.DashScopeMultimodalConfig{
				APIKey: cred.APIKey(),
				Model:  config.Model,
			})
		}
		return embedding.NewDashScopeEmbeddingModel(embedding.OpenAICompatConfig{
			APIKey:  cred.APIKey(),
			BaseURL: cred.BaseURL(),
			Model:   config.Model,
		})
	case "ollama":
		return embedding.NewOllamaEmbeddingModel(embedding.OpenAICompatConfig{
			BaseURL: cred.BaseURL(),
			Model:   config.Model,
		})
	case "gemini":
		return embedding.NewGeminiEmbeddingModel(embedding.GeminiConfig{
			APIKey: cred.APIKey(),
			Model:  config.Model,
		})
	default:
		return nil, fmt.Errorf("embedding provider %q not supported", config.Provider)
	}
}

// TTSModelConfig describes which TTS model to create.
type TTSModelConfig struct {
	Provider     string `json:"provider"`
	CredentialID string `json:"credential_id"`
	Model        string `json:"model"`
}

// GetTTSModel resolves a credential and constructs a TTS model.
func GetTTSModel(config TTSModelConfig, registry *credential.Registry) (tts.Model, error) {
	cred, err := registry.Get(config.Provider)
	if err != nil {
		return nil, fmt.Errorf("credential %q not found: %w", config.Provider, err)
	}

	switch config.Provider {
	case "dashscope":
		return tts.NewDashScopeTTSModel(tts.DashScopeConfig{
			APIKey: cred.APIKey(),
			Model:  config.Model,
		})
	default:
		return nil, fmt.Errorf("TTS provider %q not supported", config.Provider)
	}
}
