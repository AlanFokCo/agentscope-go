package model

import (
	"context"
	"fmt"
	"net/http"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/internal/httpx"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

const defaultMoonshotBaseURL = "https://api.moonshot.cn"

// MoonshotChatModel wraps the Moonshot/Kimi API (OpenAI-compatible).
type MoonshotChatModel struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// MoonshotConfig configures MoonshotChatModel.
type MoonshotConfig struct {
	APIKey     string
	BaseURL    string
	Model      string
	HTTPClient *http.Client
}

// NewMoonshotChatModel creates a ChatModel backed by Moonshot/Kimi.
func NewMoonshotChatModel(cfg MoonshotConfig) (*MoonshotChatModel, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("moonshot: APIKey is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("moonshot: Model is required")
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultMoonshotBaseURL
	}
	return &MoonshotChatModel{
		apiKey:     cfg.APIKey,
		baseURL:    base,
		model:      cfg.Model,
		httpClient: defaultHTTPClient(cfg.HTTPClient),
	}, nil
}

// Chat implements the ChatModel interface.
func (m *MoonshotChatModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (*ChatResponse, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("moonshot: msgs must not be empty")
	}

	callOpts := &CallOptions{}
	for _, opt := range opts {
		opt(callOpts)
	}

	reqBody := openAIChatRequest{
		Model:    m.model,
		Messages: convertMessagesToOpenAI(msgs),
	}
	if callOpts.Temperature != nil {
		t := float32(*callOpts.Temperature)
		reqBody.Temperature = &t
	}
	if callOpts.MaxTokens != nil {
		reqBody.MaxTokens = callOpts.MaxTokens
	}
	if callOpts.TopP != nil {
		p := float32(*callOpts.TopP)
		reqBody.TopP = &p
	}
	if len(callOpts.Tools) > 0 {
		reqBody.Tools = callOpts.Tools
	}
	if callOpts.ToolChoice != nil {
		reqBody.ToolChoice = formatToolChoice(callOpts.ToolChoice)
	}

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
		return nil, fmt.Errorf("moonshot: %w", err)
	}

	return parseOpenAIResponse(parsed, msgs)
}

// ChatStream implements streaming chat via SSE.
func (m *MoonshotChatModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (<-chan ChatResponse, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("moonshot: msgs must not be empty")
	}

	callOpts := &CallOptions{}
	for _, opt := range opts {
		opt(callOpts)
	}

	reqBody := openAIChatRequest{
		Model:         m.model,
		Messages:      convertMessagesToOpenAI(msgs),
		Stream:        true,
		StreamOptions: &openAIStreamOpts{IncludeUsage: true},
	}
	if callOpts.Temperature != nil {
		t := float32(*callOpts.Temperature)
		reqBody.Temperature = &t
	}
	if callOpts.MaxTokens != nil {
		reqBody.MaxTokens = callOpts.MaxTokens
	}
	if callOpts.TopP != nil {
		p := float32(*callOpts.TopP)
		reqBody.TopP = &p
	}
	if len(callOpts.Tools) > 0 {
		reqBody.Tools = callOpts.Tools
	}
	if callOpts.ToolChoice != nil {
		reqBody.ToolChoice = formatToolChoice(callOpts.ToolChoice)
	}

	sseCh, err := httpx.DoSSERequest(
		ctx,
		m.httpClient,
		"POST",
		m.baseURL+"/v1/chat/completions",
		reqBody,
		map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer " + m.apiKey,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("moonshot: %w", err)
	}

	outCh := make(chan ChatResponse, 16)
	go processOpenAIStream(ctx, sseCh, outCh)
	return outCh, nil
}

// CountTokens estimates token count.
func (m *MoonshotChatModel) CountTokens(msgs []*message.Msg, tools []ToolSchema) int {
	return countTokensByBytes(msgs, tools)
}
