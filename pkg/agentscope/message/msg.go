package message

import (
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/types"
	"github.com/google/uuid"
)

// Role represents the sender role of a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// ContentBlockType enumerates supported content block kinds.
type ContentBlockType string

const (
	ContentBlockText       ContentBlockType = "text"
	ContentBlockThinking   ContentBlockType = "thinking"
	ContentBlockToolUse    ContentBlockType = "tool_use"
	ContentBlockToolResult ContentBlockType = "tool_result"
	ContentBlockImage      ContentBlockType = "image"
	ContentBlockAudio      ContentBlockType = "audio"
	ContentBlockVideo      ContentBlockType = "video"
)

// ContentBlock is the common interface of all content blocks.
type ContentBlock interface {
	GetType() ContentBlockType
}

// TextBlock represents plain text content.
type TextBlock struct {
	Type ContentBlockType `json:"type"`
	Text string           `json:"text"`
}

func (b TextBlock) GetType() ContentBlockType { return b.Type }

// ThinkingBlock represents internal reasoning content.
type ThinkingBlock struct {
	Type     ContentBlockType `json:"type"`
	Thinking string           `json:"thinking"`
}

func (b ThinkingBlock) GetType() ContentBlockType { return b.Type }

// ToolUseBlock represents a tool call request.
type ToolUseBlock struct {
	Type      ContentBlockType `json:"type"`
	ToolName  string           `json:"tool_name"`
	Arguments types.JSONValue  `json:"arguments"`
}

func (b ToolUseBlock) GetType() ContentBlockType { return b.Type }

// ToolResultBlock represents the result of a tool call.
type ToolResultBlock struct {
	Type     ContentBlockType `json:"type"`
	ToolName string           `json:"tool_name"`
	Result   types.JSONValue  `json:"result"`
}

func (b ToolResultBlock) GetType() ContentBlockType { return b.Type }

// Base64Source and URLSource are kept minimal for now to match Python concepts.
type Base64Source struct {
	Type string `json:"type"` // "base64"
	Data string `json:"data"`
}

type URLSource struct {
	Type string `json:"type"` // "url"
	URL  string `json:"url"`
}

// ImageBlock represents an image content block.
type ImageBlock struct {
	Type   ContentBlockType `json:"type"`
	Source types.JSONValue  `json:"source"` // Base64Source or URLSource
}

func (b ImageBlock) GetType() ContentBlockType { return b.Type }

// AudioBlock represents an audio content block.
type AudioBlock struct {
	Type   ContentBlockType `json:"type"`
	Source types.JSONValue  `json:"source"` // Base64Source or URLSource
}

func (b AudioBlock) GetType() ContentBlockType { return b.Type }

// VideoBlock represents a video content block.
type VideoBlock struct {
	Type   ContentBlockType `json:"type"`
	Source types.JSONValue  `json:"source"` // Base64Source or URLSource
}

func (b VideoBlock) GetType() ContentBlockType { return b.Type }

// Msg is the core message type in agentscope-go.
type Msg struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Role         Role                 `json:"role"`
	Content      any                  `json:"content"` // string or []ContentBlock
	Metadata     types.JSONObject     `json:"metadata,omitempty"`
	Timestamp    string               `json:"timestamp"`
	InvocationID string               `json:"invocation_id,omitempty"`
}

// NewMsg constructs a new Msg with generated ID and timestamp.
func NewMsg(name string, role Role, content any) *Msg {
	if contentStr, ok := content.(string); !ok {
		if _, ok := content.([]ContentBlock); !ok {
			panic("message content must be string or []ContentBlock")
		}
	} else {
		_ = contentStr
	}

	now := time.Now().Format("2006-01-02 15:04:05.000")

	return &Msg{
		ID:        uuid.NewString(),
		Name:      name,
		Role:      role,
		Content:   content,
				Metadata:  types.JSONObject{},
		Timestamp: now,
	}
}

// ToMap converts message to a generic map representation, convenient for JSON.
func (m *Msg) ToMap() map[string]any {
	if m == nil {
		return nil
	}
	return map[string]any{
		"id":            m.ID,
		"name":          m.Name,
		"role":          string(m.Role),
		"content":       m.Content,
		"metadata":      m.Metadata,
		"timestamp":     m.Timestamp,
		"invocation_id": m.InvocationID,
	}
}

// FromMap reconstructs a Msg from a generic map, assuming trusted input.
func FromMap(data map[string]any) *Msg {
	if data == nil {
		return nil
	}
	msg := &Msg{
		ID:        strOrEmpty(data["id"]),
		Name:      strOrEmpty(data["name"]),
		Role:      Role(strOrEmpty(data["role"])),
		Content:   data["content"],
		Timestamp: strOrEmpty(data["timestamp"]),
	}
	if meta, ok := data["metadata"].(types.JSONObject); ok {
		msg.Metadata = meta
	}
	if inv, ok := data["invocation_id"].(string); ok {
		msg.InvocationID = inv
	}
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	if msg.Timestamp == "" {
		msg.Timestamp = time.Now().Format("2006-01-02 15:04:05.000")
	}
	return msg
}

func strOrEmpty(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// HasContentBlocks reports whether the message contains content blocks,
// optionally filtered by type.
func (m *Msg) HasContentBlocks(targetTypes ...ContentBlockType) bool {
	blocks := m.GetContentBlocks(targetTypes...)
	return len(blocks) > 0
}

// GetTextContent returns concatenated text from all text blocks (if any),
// or returns the string content directly if Content is a string.
func (m *Msg) GetTextContent(separator string) *string {
	if m == nil {
		return nil
	}
	if s, ok := m.Content.(string); ok {
		return &s
	}
	blocks, ok := m.Content.([]ContentBlock)
	if !ok {
		return nil
	}
	var texts []string
	for _, b := range blocks {
		if b.GetType() == ContentBlockText {
			if tb, ok := b.(TextBlock); ok {
				texts = append(texts, tb.Text)
			}
		}
	}
	if len(texts) == 0 {
		return nil
	}
	joined := texts[0]
	for i := 1; i < len(texts); i++ {
		joined += separator + texts[i]
	}
	return &joined
}

// GetContentBlocks returns all content blocks, optionally filtered by types.
// If Content is a string, it is wrapped into a single TextBlock.
func (m *Msg) GetContentBlocks(targetTypes ...ContentBlockType) []ContentBlock {
	if m == nil {
		return nil
	}

	var blocks []ContentBlock
	switch c := m.Content.(type) {
	case string:
		blocks = []ContentBlock{TextBlock{Type: ContentBlockText, Text: c}}
	case []ContentBlock:
		blocks = c
	default:
		return nil
	}

	if len(targetTypes) == 0 {
		return blocks
	}

	filter := make(map[ContentBlockType]struct{}, len(targetTypes))
	for _, t := range targetTypes {
		filter[t] = struct{}{}
	}
	var out []ContentBlock
	for _, b := range blocks {
		if _, ok := filter[b.GetType()]; ok {
			out = append(out, b)
		}
	}
	return out
}

