package formatter

import (
	"encoding/json"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

// OpenAIFormatter formats messages for OpenAI-compatible APIs (OpenAI, DashScope, DeepSeek).
type OpenAIFormatter struct {
	// SupportsThinking enables reasoning_content field (DashScope/DeepSeek).
	SupportsThinking bool
}

func (f *OpenAIFormatter) Format(msgs []*message.Msg) ([]map[string]any, error) {
	var result []map[string]any
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		formatted := f.formatMsg(msg)
		if formatted != nil {
			result = append(result, formatted)
		}
	}
	return result, nil
}

func (f *OpenAIFormatter) formatMsg(msg *message.Msg) map[string]any {
	m := map[string]any{"role": string(msg.Role)}

	blocks := msg.GetContentBlocks()
	if len(blocks) == 0 {
		m["content"] = ""
		return m
	}

	var textParts []string
	var thinkingParts []string
	var toolCalls []map[string]any
	var toolResultID, toolResultContent string
	isToolResult := false

	for _, b := range blocks {
		switch blk := b.(type) {
		case message.TextBlock:
			textParts = append(textParts, blk.Text)
		case message.ThinkingBlock:
			thinkingParts = append(thinkingParts, blk.Thinking)
		case message.ToolCallBlock:
			tc := map[string]any{
				"id":   blk.ID,
				"type": "function",
				"function": map[string]any{
					"name":      blk.Name,
					"arguments": blk.Input,
				},
			}
			toolCalls = append(toolCalls, tc)
		case message.ToolResultBlock:
			isToolResult = true
			toolResultID = blk.ID
			toolResultContent = blk.GetOutputText()
		case message.HintBlock:
			textParts = append(textParts, blk.Hint)
		}
	}

	if isToolResult {
		return map[string]any{
			"role":         "tool",
			"tool_call_id": toolResultID,
			"content":      toolResultContent,
		}
	}

	if len(toolCalls) > 0 {
		m["tool_calls"] = toolCalls
	}

	content := joinStrings(textParts)
	if content != "" {
		m["content"] = content
	} else if len(toolCalls) > 0 {
		m["content"] = nil
	} else {
		m["content"] = ""
	}

	if f.SupportsThinking && len(thinkingParts) > 0 {
		m["reasoning_content"] = joinStrings(thinkingParts)
	}

	return m
}

func joinStrings(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += p
	}
	return result
}

// --- DashScope Formatter (extends OpenAI with thinking support) ---

// DashScopeFormatter formats messages for DashScope APIs.
// DashScope uses OpenAI-compatible format with reasoning_content support.
type DashScopeFormatter struct {
	OpenAIFormatter
}

// NewDashScopeFormatter creates a formatter for DashScope.
func NewDashScopeFormatter() *DashScopeFormatter {
	return &DashScopeFormatter{
		OpenAIFormatter: OpenAIFormatter{SupportsThinking: true},
	}
}

// NewOpenAIFormatter creates a formatter for standard OpenAI APIs.
func NewOpenAIFormatter() *OpenAIFormatter {
	return &OpenAIFormatter{SupportsThinking: false}
}

// --- Anthropic Formatter ---

// AnthropicFormatter formats messages for Anthropic's Messages API.
type AnthropicFormatter struct{}

func (f *AnthropicFormatter) Format(msgs []*message.Msg) ([]map[string]any, error) {
	var result []map[string]any
	for _, msg := range msgs {
		if msg == nil {
			continue
		}
		if msg.Role == message.RoleSystem {
			continue
		}
		formatted := f.formatMsg(msg)
		if formatted != nil {
			result = append(result, formatted)
		}
	}
	return result, nil
}

func (f *AnthropicFormatter) formatMsg(msg *message.Msg) map[string]any {
	role := string(msg.Role)
	if role == "tool" {
		role = "user"
	}

	blocks := msg.GetContentBlocks()
	if len(blocks) == 0 {
		return map[string]any{
			"role":    role,
			"content": "",
		}
	}

	var content []map[string]any
	for _, b := range blocks {
		switch blk := b.(type) {
		case message.TextBlock:
			content = append(content, map[string]any{
				"type": "text",
				"text": blk.Text,
			})
		case message.ThinkingBlock:
			content = append(content, map[string]any{
				"type":     "thinking",
				"thinking": blk.Thinking,
			})
		case message.ToolCallBlock:
			var inputObj any
			if err := json.Unmarshal([]byte(blk.Input), &inputObj); err != nil {
				inputObj = map[string]any{}
			}
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    blk.ID,
				"name":  blk.Name,
				"input": inputObj,
			})
		case message.ToolResultBlock:
			content = append(content, map[string]any{
				"type":       "tool_result",
				"tool_use_id": blk.ID,
				"content":    blk.GetOutputText(),
			})
		case message.HintBlock:
			content = append(content, map[string]any{
				"type": "text",
				"text": blk.Hint,
			})
		}
	}

	if len(content) == 0 {
		return nil
	}

	return map[string]any{
		"role":    role,
		"content": content,
	}
}

// NewAnthropicFormatter creates a formatter for Anthropic's Messages API.
func NewAnthropicFormatter() *AnthropicFormatter {
	return &AnthropicFormatter{}
}

// ExtractSystemPrompt extracts the system message text from a message list.
// Anthropic requires system prompt to be passed separately, not in the messages array.
func ExtractSystemPrompt(msgs []*message.Msg) string {
	for _, msg := range msgs {
		if msg != nil && msg.Role == message.RoleSystem {
			if txt := msg.GetTextContent("\n"); txt != nil {
				return *txt
			}
		}
	}
	return ""
}
