package model

import (
	"context"
	"fmt"
	"net/http"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/internal/httpx"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

const defaultOpenAIBaseURL = "https://api.openai.com"

// OpenAIChatModel wraps the OpenAI Chat Completions API.
type OpenAIChatModel struct {
	apiKey  string
	baseURL string
	model   string

	httpClient *http.Client
}

// OpenAIConfig configures OpenAIChatModel.
type OpenAIConfig struct {
	APIKey  string
	BaseURL string
	Model   string

	HTTPClient *http.Client
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
		return nil, fmt.Errorf("openai: %w", err)
	}

	return parseOpenAIResponse(parsed, msgs)
}

// ChatStream implements streaming chat via SSE (OpenAI format).
func (m *OpenAIChatModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (<-chan ChatResponse, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("openai: msgs must not be empty")
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
		return nil, fmt.Errorf("openai: %w", err)
	}

	outCh := make(chan ChatResponse, 16)
	go processOpenAIStream(ctx, sseCh, outCh)
	return outCh, nil
}

// CountTokens estimates token count.
func (m *OpenAIChatModel) CountTokens(msgs []*message.Msg, tools []ToolSchema) int {
	return countTokensByBytes(msgs, tools)
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

		msg := openAIChatMessage{Role: role, Name: m.Name}

		// Check for tool result blocks (need special handling)
		toolResults := m.GetContentBlocks(message.ContentBlockToolResult)
		if len(toolResults) > 0 {
			tr := toolResults[0].(message.ToolResultBlock)
			msg.Role = "tool"
			msg.ToolCallID = tr.ID
			msg.Content = tr.GetOutputText()
			msg.Name = tr.Name
			out = append(out, msg)
			continue
		}

		// Check for tool call blocks
		toolCalls := m.GetContentBlocks(message.ContentBlockToolCall)
		if len(toolCalls) > 0 {
			for _, tc := range toolCalls {
				tcb := tc.(message.ToolCallBlock)
				msg.ToolCalls = append(msg.ToolCalls, openAIToolCall{
					ID:   tcb.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: tcb.Name, Arguments: tcb.Input},
				})
			}
		}

		// Text content
		if txt := m.GetTextContent("\n"); txt != nil {
			msg.Content = *txt
		} else if msg.ToolCalls == nil {
			msg.Content = ""
		}

		out = append(out, msg)
	}
	return out
}
