package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

// OpenAIResponseConfig configures the OpenAI Responses API model.
type OpenAIResponseConfig struct {
	APIKey          string
	Model           string
	BaseURL         string // default: https://api.openai.com
	MaxOutputTokens int
	ReasoningEffort string // "low", "medium", "high"
}

type openaiResponseModel struct {
	cfg OpenAIResponseConfig
}

// NewOpenAIResponseModel creates a ChatModel that uses the OpenAI Responses API.
func NewOpenAIResponseModel(cfg OpenAIResponseConfig) (ChatModel, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openai response: api key is required")
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4.1"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com"
	}
	return &openaiResponseModel{cfg: cfg}, nil
}

func (m *openaiResponseModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (*ChatResponse, error) {
	o := CallOptions{}
	for _, opt := range opts {
		opt(&o)
	}

	body := m.buildRequestBody(msgs, &o, false)

	respBody, err := m.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	var raw map[string]any
	if err := json.NewDecoder(respBody).Decode(&raw); err != nil {
		return nil, fmt.Errorf("openai response: decode failed: %w", err)
	}

	return m.parseResponse(raw)
}

func (m *openaiResponseModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (<-chan ChatResponse, error) {
	o := CallOptions{}
	for _, opt := range opts {
		opt(&o)
	}

	body := m.buildRequestBody(msgs, &o, true)

	respBody, err := m.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	ch := make(chan ChatResponse, 32)
	go m.processStream(respBody, ch)
	return ch, nil
}

func (m *openaiResponseModel) CountTokens(msgs []*message.Msg, tools []ToolSchema) int {
	return countTokensByBytes(msgs, tools)
}

func (m *openaiResponseModel) ContextSize() int { return 200000 }
func (m *openaiResponseModel) ModelName() string { return m.cfg.Model }

func (m *openaiResponseModel) buildRequestBody(msgs []*message.Msg, opts *CallOptions, stream bool) map[string]any {
	body := map[string]any{
		"model":  m.cfg.Model,
		"stream": stream,
	}

	// Convert messages to input items
	var input []map[string]any
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		if msg.Role == message.RoleSystem {
			if txt := msg.GetTextContent("\n"); txt != nil {
				body["instructions"] = *txt
			}
			continue
		}
		item := m.formatInputItem(msg)
		if item != nil {
			input = append(input, item)
		}
	}
	body["input"] = input

	if m.cfg.MaxOutputTokens > 0 {
		body["max_output_tokens"] = m.cfg.MaxOutputTokens
	}
	if opts.MaxTokens != nil {
		body["max_output_tokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if m.cfg.ReasoningEffort != "" {
		body["reasoning"] = map[string]any{"effort": m.cfg.ReasoningEffort}
	}

	if len(opts.Tools) > 0 {
		var tools []map[string]any
		for _, t := range opts.Tools {
			tools = append(tools, map[string]any{
				"type":        "function",
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  json.RawMessage(t.Function.Parameters),
			})
		}
		body["tools"] = tools
	}
	if opts.ToolChoice != nil {
		body["tool_choice"] = opts.ToolChoice.Mode
	}

	return body
}

func (m *openaiResponseModel) formatInputItem(msg *message.Msg) map[string]any {
	role := string(msg.Role)
	if role == "assistant" {
		role = "assistant"
	}

	blocks := msg.GetContentBlocks()
	if len(blocks) == 0 {
		return map[string]any{"role": role, "content": ""}
	}

	// Check for tool results
	for _, b := range blocks {
		if tr, ok := b.(message.ToolResultBlock); ok {
			return map[string]any{
				"type":    "function_call_output",
				"call_id": tr.ID,
				"output":  tr.GetOutputText(),
			}
		}
	}

	var content []map[string]any
	for _, b := range blocks {
		switch blk := b.(type) {
		case message.TextBlock:
			content = append(content, map[string]any{
				"type": "input_text",
				"text": blk.Text,
			})
		case message.ToolCallBlock:
			// Tool calls from assistant are represented as function_call items
			return map[string]any{
				"type":      "function_call",
				"id":        blk.ID,
				"call_id":   blk.ID,
				"name":      blk.Name,
				"arguments": blk.Input,
			}
		}
	}

	if len(content) == 1 {
		return map[string]any{"role": role, "content": content[0]["text"]}
	}
	return map[string]any{"role": role, "content": content}
}

func (m *openaiResponseModel) doRequest(ctx context.Context, body map[string]any) (io.ReadCloser, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai response: marshal request: %w", err)
	}

	url := strings.TrimRight(m.cfg.BaseURL, "/") + "/v1/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.cfg.APIKey)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai response: request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai response: HTTP %d: %s", resp.StatusCode, string(b))
	}
	return resp.Body, nil
}

func (m *openaiResponseModel) parseResponse(raw map[string]any) (*ChatResponse, error) {
	resp := &ChatResponse{IsLast: true, ModelName: m.cfg.Model}

	if id, ok := raw["id"].(string); ok {
		resp.ID = id
	}

	if output, ok := raw["output"].([]any); ok {
		for _, item := range output {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch obj["type"] {
			case "message":
				if content, ok := obj["content"].([]any); ok {
					for _, c := range content {
						cm, ok := c.(map[string]any)
						if !ok {
							continue
						}
						if cm["type"] == "output_text" {
							if text, ok := cm["text"].(string); ok {
								resp.Content = append(resp.Content, message.TextBlock{Type: "text", Text: text})
							}
						}
					}
				}
			case "reasoning":
				if summary, ok := obj["summary"].([]any); ok {
					for _, s := range summary {
						sm, ok := s.(map[string]any)
						if !ok {
							continue
						}
						if text, ok := sm["text"].(string); ok {
							resp.Content = append(resp.Content, message.ThinkingBlock{Type: "thinking", Thinking: text})
						}
					}
				}
			case "function_call":
				name, _ := obj["name"].(string)
				args, _ := obj["arguments"].(string)
				id, _ := obj["id"].(string)
				resp.Content = append(resp.Content, message.ToolCallBlock{
					Type:  "tool_call",
					ID:    id,
					Name:  name,
					Input: args,
				})
			}
		}
	}

	if usage, ok := raw["usage"].(map[string]any); ok {
		resp.Usage = &ChatUsage{}
		if v, ok := usage["input_tokens"].(float64); ok {
			resp.Usage.InputTokens = int(v)
		}
		if v, ok := usage["output_tokens"].(float64); ok {
			resp.Usage.OutputTokens = int(v)
		}
	}

	return resp, nil
}

func (m *openaiResponseModel) processStream(body io.ReadCloser, ch chan<- ChatResponse) {
	defer close(ch)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var accText string
	var accThinking string
	toolCalls := make(map[string]*message.ToolCallBlock)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)

		switch eventType {
		case "response.output_text.delta":
			delta, _ := event["delta"].(string)
			accText += delta
			ch <- ChatResponse{
				Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: delta}},
			}

		case "response.reasoning_summary_text.delta":
			delta, _ := event["delta"].(string)
			accThinking += delta
			ch <- ChatResponse{
				Content: []message.ContentBlock{message.ThinkingBlock{Type: "thinking", Thinking: delta}},
			}

		case "response.output_item.added":
			if item, ok := event["item"].(map[string]any); ok {
				if itemType, _ := item["type"].(string); itemType == "function_call" {
					id, _ := item["id"].(string)
					name, _ := item["name"].(string)
					toolCalls[id] = &message.ToolCallBlock{
						Type: "tool_call", ID: id, Name: name,
					}
				}
			}

		case "response.function_call_arguments.delta":
			itemID, _ := event["item_id"].(string)
			delta, _ := event["delta"].(string)
			if tc, ok := toolCalls[itemID]; ok {
				tc.Input += delta
			}

		case "response.completed":
			resp := &ChatResponse{IsLast: true, ModelName: m.cfg.Model}
			if accText != "" {
				resp.Content = append(resp.Content, message.TextBlock{Type: "text", Text: accText})
			}
			if accThinking != "" {
				resp.Content = append(resp.Content, message.ThinkingBlock{Type: "thinking", Thinking: accThinking})
			}
			for _, tc := range toolCalls {
				resp.Content = append(resp.Content, *tc)
			}

			if respObj, ok := event["response"].(map[string]any); ok {
				if id, ok := respObj["id"].(string); ok {
					resp.ID = id
				}
				if usage, ok := respObj["usage"].(map[string]any); ok {
					resp.Usage = &ChatUsage{}
					if v, ok := usage["input_tokens"].(float64); ok {
						resp.Usage.InputTokens = int(v)
					}
					if v, ok := usage["output_tokens"].(float64); ok {
						resp.Usage.OutputTokens = int(v)
					}
				}
			}

			ch <- *resp
		}
	}

	if err := scanner.Err(); err != nil {
		logrus.WithError(err).Error("openai response: stream scan error")
	}
}
