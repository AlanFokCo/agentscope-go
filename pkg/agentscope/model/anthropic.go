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

// AnthropicChatModel is a wrapper around Anthropic's Messages API,
// aligned with the ChatModel interface so it can be swapped with other models
// in ReActAgent / Pipeline.
//
// Reference: https://docs.anthropic.com/en/api/messages
type AnthropicChatModel struct {
	apiKey       string
	baseURL      string
	model        string
	version      string
	maxOutputTok int

	httpClient *http.Client
}

// AnthropicConfig configures AnthropicChatModel.
type AnthropicConfig struct {
	APIKey  string
	BaseURL string // Optional, defaults to https://api.anthropic.com
	Model   string

	// Anthropic API version, for example: 2023-06-01
	Version string

	MaxOutputTokens int          // Optional, defaults to 1024
	HTTPClient      *http.Client // Optional custom client
}

// NewAnthropicChatModel creates a ChatModel backed by Anthropic.
func NewAnthropicChatModel(cfg AnthropicConfig) (*AnthropicChatModel, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("anthropic: APIKey is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("anthropic: Model is required")
	}
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	ver := cfg.Version
	if ver == "" {
		ver = "2023-06-01"
	}
	maxTok := cfg.MaxOutputTokens
	if maxTok <= 0 {
		maxTok = 1024
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 60 * time.Second,
		}
	}
	return &AnthropicChatModel{
		apiKey:       cfg.APIKey,
		baseURL:      base,
		model:        cfg.Model,
		version:      ver,
		maxOutputTok: maxTok,
		httpClient:   client,
	}, nil
}

// anthropicMessage mirrors the Anthropic messages API message object.
type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// anthropicRequest is the request body for the Messages API.
type anthropicRequest struct {
	Model           string             `json:"model"`
	Messages        []anthropicMessage `json:"messages"`
	MaxOutputTokens int                `json:"max_output_tokens"`
	// For now we use only basic fields; can be extended with system/tool_choice/temperature etc.
}

// anthropicResponse is a simplified representation of the Messages API response.
type anthropicResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// Chat implements the non-streaming ChatModel call for Anthropic.
func (m *AnthropicChatModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (*ChatResponse, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("anthropic: msgs must not be empty")
	}

	// Convert internal Msg slice into Anthropic messages.
	am := convertMessagesToAnthropic(msgs)

	reqBody := anthropicRequest{
		Model:           m.model,
		Messages:        am,
		MaxOutputTokens: m.maxOutputTok,
	}

	var parsed anthropicResponse
	if err := httpx.DoJSONRequest(
		ctx,
		m.httpClient,
		http.MethodPost,
		m.baseURL+"/v1/messages",
		reqBody,
		&parsed,
		map[string]string{
			"Content-Type":      "application/json",
			"x-api-key":         m.apiKey,
			"anthropic-version": m.version,
		},
	); err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}

	// Simplified: concatenate all text segments into the final reply.
	if len(parsed.Content) == 0 {
		return nil, fmt.Errorf("anthropic: empty content")
	}
	replyText := ""
	for _, c := range parsed.Content {
		if c.Type == "text" {
			replyText += c.Text
		}
	}

	last := msgs[len(msgs)-1]
	reply := message.NewMsg(last.Name, message.RoleAssistant, replyText)

	return &ChatResponse{
		Msg:       reply,
		Raw:       parsed,
		ModelName: parsed.Model,
		CreatedAt: time.Now(),
	}, nil
}

// ChatStream is not supported yet and returns ErrStreamNotSupported.
func (m *AnthropicChatModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (ChatStream, error) {
	_ = ctx
	_ = msgs
	_ = opts
	return nil, ErrStreamNotSupported
}

// convertMessagesToAnthropic converts internal Msg instances into Anthropic messages.
func convertMessagesToAnthropic(msgs []*message.Msg) []anthropicMessage {
	out := make([]anthropicMessage, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		role := string(m.Role)
		if role == "" {
			role = "user"
		}
		// Simplified: we only use plain text content for now.
		if txt := m.GetTextContent("\n"); txt != nil {
			out = append(out, anthropicMessage{
				Role:    role,
				Content: *txt,
			})
		} else if s, ok := m.Content.(string); ok {
			out = append(out, anthropicMessage{
				Role:    role,
				Content: s,
			})
		} else {
			b, err := json.Marshal(m.Content)
			if err != nil {
				out = append(out, anthropicMessage{
					Role:    role,
					Content: fmt.Sprintf("unsupported content: %T", m.Content),
				})
			} else {
				out = append(out, anthropicMessage{
					Role:    role,
					Content: string(b),
				})
			}
		}
	}
	return out
}
