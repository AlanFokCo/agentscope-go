package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/formatter"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/internal/httpx"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

const (
	defaultAnthropicBaseURL         = "https://api.anthropic.com"
	defaultAnthropicVersion         = "2023-06-01"
	defaultAnthropicMaxOutputTokens = 1024
)

// AnthropicChatModel wraps Anthropic's Messages API.
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
	APIKey          string
	BaseURL         string
	Model           string
	Version         string
	MaxOutputTokens int
	HTTPClient      *http.Client
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
		base = defaultAnthropicBaseURL
	}
	ver := cfg.Version
	if ver == "" {
		ver = defaultAnthropicVersion
	}
	maxTok := cfg.MaxOutputTokens
	if maxTok <= 0 {
		maxTok = defaultAnthropicMaxOutputTokens
	}
	return &AnthropicChatModel{
		apiKey:       cfg.APIKey,
		baseURL:      base,
		model:        cfg.Model,
		version:      ver,
		maxOutputTok: maxTok,
		httpClient:   defaultHTTPClient(cfg.HTTPClient),
	}, nil
}

type anthropicMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type anthropicRequest struct {
	Model      string              `json:"model"`
	Messages   []anthropicMessage  `json:"messages"`
	MaxTokens  int                 `json:"max_tokens"`
	System     string              `json:"system,omitempty"`
	Tools      []anthropicTool     `json:"tools,omitempty"`
	ToolChoice *anthropicToolChoice `json:"tool_choice,omitempty"`
	Thinking   *anthropicThinking  `json:"thinking,omitempty"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`           // "auto", "any", "tool", "none"
	Name string `json:"name,omitempty"` // only for type="tool"
}

type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Role    string `json:"role"`
	Content []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text,omitempty"`
		Thinking  string          `json:"thinking,omitempty"`
		Signature string          `json:"signature,omitempty"`
		ID        string          `json:"id,omitempty"`
		Name      string          `json:"name,omitempty"`
		Input     json.RawMessage `json:"input,omitempty"`
	} `json:"content"`
	Usage *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	} `json:"usage,omitempty"`
}

// Chat implements the non-streaming ChatModel call for Anthropic.
func (m *AnthropicChatModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (*ChatResponse, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("anthropic: msgs must not be empty")
	}

	callOpts := &CallOptions{}
	for _, opt := range opts {
		opt(callOpts)
	}

	am, system := convertMessagesToAnthropic(msgs)

	reqBody := anthropicRequest{
		Model:     m.model,
		Messages:  am,
		MaxTokens: m.maxOutputTok,
		System:    system,
	}

	// Thinking parameters
	if callOpts.ThinkingEnable != nil && *callOpts.ThinkingEnable {
		budget := reqBody.MaxTokens / 2
		if callOpts.ThinkingBudget != nil && *callOpts.ThinkingBudget > 0 {
			budget = *callOpts.ThinkingBudget
		}
		if budget >= reqBody.MaxTokens {
			reqBody.MaxTokens = budget + 1024
		}
		reqBody.Thinking = &anthropicThinking{Type: "enabled", BudgetTokens: budget}
	}

	if len(callOpts.Tools) > 0 {
		for _, ts := range callOpts.Tools {
			reqBody.Tools = append(reqBody.Tools, anthropicTool{
				Name:        ts.Function.Name,
				Description: ts.Function.Description,
				InputSchema: ts.Function.Parameters,
			})
		}
	}

	if callOpts.ToolChoice != nil && len(reqBody.Tools) > 0 {
		reqBody.ToolChoice = formatToolChoiceAnthropic(callOpts.ToolChoice)
	}

	// Retry loop
	maxRetries := callOpts.MaxRetries
	retryDelay := callOpts.RetryDelay
	if retryDelay == 0 {
		retryDelay = time.Second
	}

	var parsed anthropicResponse
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}
		lastErr = httpx.DoJSONRequest(
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
		)
		if lastErr == nil {
			break
		}
		if !IsRetryableError(lastErr) {
			return nil, fmt.Errorf("anthropic: %w", lastErr)
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("anthropic: %w", lastErr)
	}

	if len(parsed.Content) == 0 {
		return nil, fmt.Errorf("anthropic: empty content")
	}

	var content []message.ContentBlock
	for _, c := range parsed.Content {
		switch c.Type {
		case "text":
			content = append(content, message.TextBlock{
				Type: "text",
				ID:   fmt.Sprintf("text_%s", parsed.ID),
				Text: c.Text,
			})
		case "thinking":
			tb := message.ThinkingBlock{
				Type:     "thinking",
				ID:       fmt.Sprintf("thinking_%s", parsed.ID),
				Thinking: c.Thinking,
			}
			if c.Signature != "" {
				tb.Extra = map[string]any{"signature": c.Signature}
			}
			content = append(content, tb)
		case "tool_use":
			inputStr := string(c.Input)
			content = append(content, message.ToolCallBlock{
				Type:  "tool_call",
				ID:    c.ID,
				Name:  c.Name,
				Input: inputStr,
				State: message.ToolCallPending,
			})
		}
	}

	var usage *ChatUsage
	if parsed.Usage != nil {
		usage = &ChatUsage{
			InputTokens:              parsed.Usage.InputTokens,
			OutputTokens:             parsed.Usage.OutputTokens,
			CacheCreationInputTokens: parsed.Usage.CacheCreationInputTokens,
			CacheInputTokens:         parsed.Usage.CacheReadInputTokens,
		}
	}

	return &ChatResponse{
		Content:   content,
		IsLast:    true,
		ID:        parsed.ID,
		CreatedAt: time.Now().Format(message.TimestampFormat),
		Usage:     usage,
		ModelName: parsed.Model,
	}, nil
}

// ChatStream implements streaming chat via Anthropic's SSE event format.
func (m *AnthropicChatModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (<-chan ChatResponse, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("anthropic: msgs must not be empty")
	}

	callOpts := &CallOptions{}
	for _, opt := range opts {
		opt(callOpts)
	}

	am, system := convertMessagesToAnthropic(msgs)

	reqBody := anthropicStreamRequest{
		Model:     m.model,
		Messages:  am,
		MaxTokens: m.maxOutputTok,
		System:    system,
		Stream:    true,
	}

	if callOpts.ThinkingEnable != nil && *callOpts.ThinkingEnable {
		budget := reqBody.MaxTokens / 2
		if callOpts.ThinkingBudget != nil && *callOpts.ThinkingBudget > 0 {
			budget = *callOpts.ThinkingBudget
		}
		if budget >= reqBody.MaxTokens {
			reqBody.MaxTokens = budget + 1024
		}
		reqBody.Thinking = &anthropicThinking{Type: "enabled", BudgetTokens: budget}
	}

	if len(callOpts.Tools) > 0 {
		for _, ts := range callOpts.Tools {
			reqBody.Tools = append(reqBody.Tools, anthropicTool{
				Name:        ts.Function.Name,
				Description: ts.Function.Description,
				InputSchema: ts.Function.Parameters,
			})
		}
	}

	if callOpts.ToolChoice != nil && len(reqBody.Tools) > 0 {
		reqBody.ToolChoice = formatToolChoiceAnthropic(callOpts.ToolChoice)
	}

	sseCh, err := httpx.DoSSERequest(
		ctx,
		m.httpClient,
		"POST",
		m.baseURL+"/v1/messages",
		reqBody,
		map[string]string{
			"Content-Type":      "application/json",
			"x-api-key":         m.apiKey,
			"anthropic-version": m.version,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}

	outCh := make(chan ChatResponse, 16)
	go processAnthropicStream(ctx, sseCh, outCh)
	return outCh, nil
}

type anthropicStreamRequest struct {
	Model      string              `json:"model"`
	Messages   []anthropicMessage  `json:"messages"`
	MaxTokens  int                 `json:"max_tokens"`
	System     string              `json:"system,omitempty"`
	Tools      []anthropicTool     `json:"tools,omitempty"`
	ToolChoice *anthropicToolChoice `json:"tool_choice,omitempty"`
	Thinking   *anthropicThinking  `json:"thinking,omitempty"`
	Stream     bool                `json:"stream"`
}

// Anthropic SSE event data types

type anthropicMessageStart struct {
	Type    string `json:"type"`
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage,omitempty"`
	} `json:"message"`
}

type anthropicContentBlockStart struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock struct {
		Type string `json:"type"` // "text", "thinking", "tool_use"
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
		Text string `json:"text,omitempty"`
	} `json:"content_block"`
}

type anthropicContentBlockDelta struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type        string `json:"type"` // "text_delta", "thinking_delta", "input_json_delta", "signature_delta"
		Text        string `json:"text,omitempty"`
		Thinking    string `json:"thinking,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
		Signature   string `json:"signature,omitempty"`
	} `json:"delta"`
}

type anthropicMessageDelta struct {
	Type  string `json:"type"`
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
}

func processAnthropicStream(ctx context.Context, sseCh <-chan httpx.SSEEvent, outCh chan<- ChatResponse) {
	defer close(outCh)

	var (
		responseID               string
		modelName                string
		inputTokens              int
		outputTokens             int
		cacheCreationInputTokens int
		cacheInputTokens         int
		accBlocks                []anthropicAccBlock
		currentBlockIdx          int = -1
	)

	for evt := range sseCh {
		select {
		case <-ctx.Done():
			return
		default:
		}

		switch evt.Event {
		case "message_start":
			var ms anthropicMessageStart
			if json.Unmarshal([]byte(evt.Data), &ms) == nil {
				responseID = ms.Message.ID
				modelName = ms.Message.Model
				if ms.Message.Usage != nil {
					inputTokens = ms.Message.Usage.InputTokens
				}
			}
			// Also try to extract cache tokens from message_start
			var raw map[string]json.RawMessage
			if json.Unmarshal([]byte(evt.Data), &raw) == nil {
				if msgRaw, ok := raw["message"]; ok {
					var msgData struct {
						Usage *struct {
							CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
							CacheReadInputTokens     int `json:"cache_read_input_tokens"`
						} `json:"usage"`
					}
					if json.Unmarshal(msgRaw, &msgData) == nil && msgData.Usage != nil {
						cacheCreationInputTokens = msgData.Usage.CacheCreationInputTokens
						cacheInputTokens = msgData.Usage.CacheReadInputTokens
					}
				}
			}

		case "content_block_start":
			var cbs anthropicContentBlockStart
			if json.Unmarshal([]byte(evt.Data), &cbs) == nil {
				currentBlockIdx = cbs.Index
				for len(accBlocks) <= currentBlockIdx {
					accBlocks = append(accBlocks, anthropicAccBlock{})
				}
				accBlocks[currentBlockIdx].blockType = cbs.ContentBlock.Type
				accBlocks[currentBlockIdx].id = cbs.ContentBlock.ID
				accBlocks[currentBlockIdx].name = cbs.ContentBlock.Name
			}

		case "content_block_delta":
			var cbd anthropicContentBlockDelta
			if json.Unmarshal([]byte(evt.Data), &cbd) == nil {
				idx := cbd.Index
				if idx >= 0 && idx < len(accBlocks) {
					switch cbd.Delta.Type {
					case "text_delta":
						accBlocks[idx].text += cbd.Delta.Text
						// Emit delta
						resp := ChatResponse{
							Content: []message.ContentBlock{message.TextBlock{
								Type: "text",
								Text: cbd.Delta.Text,
							}},
							IsLast:    false,
							ID:        responseID,
							ModelName: modelName,
						}
						select {
						case outCh <- resp:
						case <-ctx.Done():
							return
						}
					case "thinking_delta":
						accBlocks[idx].text += cbd.Delta.Thinking
						resp := ChatResponse{
							Content: []message.ContentBlock{message.ThinkingBlock{
								Type:     "thinking",
								Thinking: cbd.Delta.Thinking,
							}},
							IsLast:    false,
							ID:        responseID,
							ModelName: modelName,
						}
						select {
						case outCh <- resp:
						case <-ctx.Done():
							return
						}
					case "input_json_delta":
						accBlocks[idx].text += cbd.Delta.PartialJSON
					case "signature_delta":
						accBlocks[idx].signature += cbd.Delta.Signature
					}
				}
			}

		case "content_block_stop":
			// Block is complete, no action needed

		case "message_delta":
			var md anthropicMessageDelta
			if json.Unmarshal([]byte(evt.Data), &md) == nil {
				if md.Usage != nil {
					outputTokens = md.Usage.OutputTokens
				}
			}

		case "message_stop":
			// Stream complete

		case "error":
			// Anthropic error event — skip gracefully
		}
	}

	// Build final accumulated response
	var finalContent []message.ContentBlock
	for _, ab := range accBlocks {
		switch ab.blockType {
		case "text":
			finalContent = append(finalContent, message.TextBlock{
				Type: "text",
				ID:   fmt.Sprintf("text_%s", responseID),
				Text: ab.text,
			})
		case "thinking":
			tb := message.ThinkingBlock{
				Type:     "thinking",
				ID:       fmt.Sprintf("thinking_%s", responseID),
				Thinking: ab.text,
			}
			if ab.signature != "" {
				tb.Extra = map[string]any{"signature": ab.signature}
			}
			finalContent = append(finalContent, tb)
		case "tool_use":
			finalContent = append(finalContent, message.ToolCallBlock{
				Type:  "tool_call",
				ID:    ab.id,
				Name:  ab.name,
				Input: ab.text,
				State: message.ToolCallPending,
			})
		}
	}

	var usage *ChatUsage
	if inputTokens > 0 || outputTokens > 0 {
		usage = &ChatUsage{
			InputTokens:              inputTokens,
			OutputTokens:             outputTokens,
			CacheCreationInputTokens: cacheCreationInputTokens,
			CacheInputTokens:         cacheInputTokens,
		}
	}

	finalResp := ChatResponse{
		Content:   finalContent,
		IsLast:    true,
		ID:        responseID,
		CreatedAt: time.Now().Format(message.TimestampFormat),
		Usage:     usage,
		ModelName: modelName,
	}
	select {
	case outCh <- finalResp:
	case <-ctx.Done():
	}
}

type anthropicAccBlock struct {
	blockType string // "text", "thinking", "tool_use"
	id        string
	name      string
	text      string
	signature string
}

// CountTokens estimates token count.
func (m *AnthropicChatModel) CountTokens(msgs []*message.Msg, tools []ToolSchema) int {
	return countTokensByBytes(msgs, tools)
}

// convertMessagesToAnthropic converts internal Msg instances into Anthropic messages.
// It uses the AnthropicFormatter for full block-type support (thinking signatures,
// multimodal DataBlocks, HintBlocks, tool results), then converts from
// []map[string]any to []anthropicMessage.
func convertMessagesToAnthropic(msgs []*message.Msg) ([]anthropicMessage, string) {
	f := formatter.NewAnthropicFormatter()
	system := formatter.ExtractSystemPrompt(msgs)

	formatted, err := f.Format(msgs)
	if err != nil {
		// Fallback to simple text extraction
		return convertMessagesToAnthropicFallback(msgs)
	}

	out := make([]anthropicMessage, 0, len(formatted))
	for _, m := range formatted {
		role, _ := m["role"].(string)
		out = append(out, anthropicMessage{Role: role, Content: m["content"]})
	}
	return out, system
}

func convertMessagesToAnthropicFallback(msgs []*message.Msg) ([]anthropicMessage, string) {
	var system string
	out := make([]anthropicMessage, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if m.Role == message.RoleSystem {
			if txt := m.GetTextContent("\n"); txt != nil {
				if system != "" {
					system += "\n"
				}
				system += *txt
			}
			continue
		}
		role := string(m.Role)
		if role == "" {
			role = "user"
		}
		if txt := m.GetTextContent("\n"); txt != nil {
			out = append(out, anthropicMessage{Role: role, Content: *txt})
		} else {
			out = append(out, anthropicMessage{Role: role, Content: ""})
		}
	}
	return out, system
}

// formatToolChoiceAnthropic converts a generic ToolChoice to Anthropic's format.
// "required" → {"type":"any"}, "auto" → {"type":"auto"}, name → {"type":"tool","name":"..."}
func formatToolChoiceAnthropic(tc *ToolChoice) *anthropicToolChoice {
	if tc == nil {
		return nil
	}
	switch tc.Mode {
	case "auto":
		return &anthropicToolChoice{Type: "auto"}
	case "none":
		return nil
	case "required":
		return &anthropicToolChoice{Type: "any"}
	default:
		if tc.Mode != "" {
			return &anthropicToolChoice{Type: "tool", Name: tc.Mode}
		}
		return nil
	}
}
