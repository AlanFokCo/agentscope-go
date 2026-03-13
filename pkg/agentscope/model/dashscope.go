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

const defaultDashScopeBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"

// DashScopeChatModel calls Alibaba Cloud DashScope (Qwen) models via the OpenAI-compatible API.
// Reference: https://help.aliyun.com/zh/model-studio/compatibility-of-openai-with-dashscope
type DashScopeChatModel struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// DashScopeConfig configures DashScopeChatModel.
type DashScopeConfig struct {
	APIKey  string
	BaseURL string // Optional, defaults to https://dashscope.aliyuncs.com/compatible-mode/v1
	Model   string

	HTTPClient *http.Client // Optional custom client
}

// NewDashScopeChatModel creates a ChatModel using the DashScope backend.
func NewDashScopeChatModel(cfg DashScopeConfig) (*DashScopeChatModel, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("dashscope: APIKey is required")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("dashscope: Model is required")
	}
	base := cfg.BaseURL
	if base == "" {
		base = defaultDashScopeBaseURL
	}
	return &DashScopeChatModel{
		apiKey:     cfg.APIKey,
		baseURL:    base,
		model:      cfg.Model,
		httpClient: defaultHTTPClient(cfg.HTTPClient),
	}, nil
}

// Chat implements ChatModel using the OpenAI-compatible chat/completions path.
func (m *DashScopeChatModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (*ChatResponse, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("dashscope: msgs must not be empty")
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
		m.baseURL+"/chat/completions",
		reqBody,
		&parsed,
		map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer " + m.apiKey,
		},
	); err != nil {
		return nil, fmt.Errorf("dashscope: %w", err)
	}

	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("dashscope: empty choices")
	}

	choice := parsed.Choices[0]
	replyContent, ok := choice.Message.Content.(string)
	if !ok {
		b, err := json.Marshal(choice.Message.Content)
		if err != nil {
			return nil, fmt.Errorf("dashscope: unexpected content type and marshal failed: %w", err)
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
func (m *DashScopeChatModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (ChatStream, error) {
	_ = ctx
	_ = msgs
	_ = opts
	return nil, ErrStreamNotSupported
}
