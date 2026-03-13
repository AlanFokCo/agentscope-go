package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/internal/httpx"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

const defaultOpenAIBaseURL = "https://api.openai.com"

// OpenAIChatModel is a thin wrapper around the OpenAI Chat Completions API.
// It uses the https://api.openai.com/v1/chat/completions endpoint style.
type OpenAIChatModel struct {
	apiKey  string
	baseURL string
	model   string

	httpClient *http.Client
}

// OpenAIConfig configures OpenAIChatModel.
type OpenAIConfig struct {
	APIKey  string
	BaseURL string // Optional, defaults to https://api.openai.com
	Model   string

	HTTPClient *http.Client // Optional custom client
}

// NewOpenAIChatModel creates a ChatModel backed by OpenAI.
func NewOpenAIChatModel(cfg OpenAIConfig) (*OpenAIChatModel, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai: APIKey is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("openai: Model is required")
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultOpenAIBaseURL
	}
	return &OpenAIChatModel{
		apiKey:     cfg.APIKey,
		baseURL:    base,
		model:      cfg.Model,
		httpClient: defaultHTTPClient(cfg.HTTPClient),
	}, nil
}

// openAIChatMessage mirrors the OpenAI Chat message structure.
type openAIChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
	Name    string      `json:"name,omitempty"`
}

type openAIChatRequest struct {
	Model    string              `json:"model"`
	Messages []openAIChatMessage `json:"messages"`

	Temperature *float32 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	TopP        *float32 `json:"top_p,omitempty"`
}

type openAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int               `json:"index"`
		Message      openAIChatMessage `json:"message"`
		FinishReason string            `json:"finish_reason"`
	} `json:"choices"`
	Usage any `json:"usage"`
}

// Chat implements the ChatModel interface for OpenAI.
func (m *OpenAIChatModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (*ChatResponse, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("openai: msgs must not be empty")
	}

	callOpts := &CallOptions{}
	for _, opt := range opts {
		opt(callOpts)
	}

	reqBody := openAIChatRequest{
		Model:    m.model,
		Messages: convertMessagesToOpenAI(msgs),
	}
	reqBody.Temperature = callOpts.Temperature
	reqBody.MaxTokens = callOpts.MaxTokens
	reqBody.TopP = callOpts.TopP

	var parsed openAIChatResponse
	if err := httpx.DoJSONRequest(
		ctx,
		m.httpClient,
		http.MethodPost,
		m.baseURL+"/v1/chat/completions",
		reqBody,
		&parsed,
		map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer " + m.apiKey,
		},
	); err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("openai: empty choices")
	}

	choice := parsed.Choices[0]
	replyContent, ok := choice.Message.Content.(string)
	if !ok {
		// Some models return list[dict] content; convert it to a JSON string.
		b, err := json.Marshal(choice.Message.Content)
		if err != nil {
			return nil, fmt.Errorf("openai: unexpected content type and marshal failed: %w", err)
		}
		replyContent = string(b)
	}

	last := msgs[len(msgs)-1]
	reply := message.NewMsg(last.Name, message.RoleAssistant, replyContent)

	return &ChatResponse{
		Msg:       reply,
		Raw:       parsed,
		ModelName: parsed.Model,
		CreatedAt: time.Now(),
	}, nil
}

// ChatStream is not supported yet and returns ErrStreamNotSupported.
func (m *OpenAIChatModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (ChatStream, error) {
	_ = ctx
	_ = msgs
	_ = opts
	return nil, ErrStreamNotSupported
}

// convertMessagesToOpenAI maps internal Msg instances to OpenAI messages.
func convertMessagesToOpenAI(msgs []*message.Msg) []openAIChatMessage {
	out := make([]openAIChatMessage, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		role := string(m.Role)
		if role == "" {
			role = "user"
		}
		// Simplified: if structured content exists, prefer plain text content.
		if txt := m.GetTextContent("\n"); txt != nil {
			out = append(out, openAIChatMessage{
				Role:    role,
				Content: *txt,
				Name:    m.Name,
			})
		} else if s, ok := m.Content.(string); ok {
			out = append(out, openAIChatMessage{
				Role:    role,
				Content: s,
				Name:    m.Name,
			})
		} else {
			// Fallback: non-string content is converted to JSON text.
			b, err := json.Marshal(m.Content)
			if err != nil {
				out = append(out, openAIChatMessage{
					Role:    role,
					Content: fmt.Sprintf("unsupported content: %T", m.Content),
					Name:    m.Name,
				})
			} else {
				out = append(out, openAIChatMessage{
					Role:    role,
					Content: string(b),
					Name:    m.Name,
				})
			}
		}
	}
	return out
}
