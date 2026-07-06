package model

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
)

type mockStructuredModel struct {
	toolCallInput string
}

func (m *mockStructuredModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (*ChatResponse, error) {
	return &ChatResponse{
		Content: []message.ContentBlock{
			message.ToolCallBlock{
				Type:  "tool_call",
				ID:    "call_1",
				Name:  structuredOutputToolName,
				Input: m.toolCallInput,
			},
		},
		IsLast: true,
	}, nil
}

func (m *mockStructuredModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (<-chan ChatResponse, error) {
	return nil, ErrStreamNotSupported
}

func (m *mockStructuredModel) CountTokens(msgs []*message.Msg, tools []ToolSchema) int {
	return 0
}

func TestGenerateStructuredOutput_Success(t *testing.T) {
	mock := &mockStructuredModel{
		toolCallInput: `{"name":"Alice","age":30}`,
	}

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "integer"}
		},
		"required": ["name", "age"]
	}`)

	msgs := []*message.Msg{
		message.UserMsg("user", "Extract name and age from: Alice is 30 years old."),
	}

	result, err := GenerateStructuredOutput(context.Background(), mock, msgs, schema)
	if err != nil {
		t.Fatal(err)
	}

	var parsed struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Name != "Alice" || parsed.Age != 30 {
		t.Fatalf("got %+v", parsed)
	}
}

func TestGenerateStructuredOutput_NoToolCall(t *testing.T) {
	mock := &mockNoToolCallModel{}
	schema := json.RawMessage(`{"type":"object","properties":{}}`)
	msgs := []*message.Msg{message.UserMsg("user", "test")}

	_, err := GenerateStructuredOutput(context.Background(), mock, msgs, schema)
	if err == nil {
		t.Fatal("expected error when model doesn't produce tool call")
	}
}

type mockNoToolCallModel struct{}

func (m *mockNoToolCallModel) Chat(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (*ChatResponse, error) {
	return &ChatResponse{
		Content: []message.ContentBlock{
			message.TextBlock{Type: "text", Text: "I can't do that."},
		},
		IsLast: true,
	}, nil
}

func (m *mockNoToolCallModel) ChatStream(ctx context.Context, msgs []*message.Msg, opts ...CallOption) (<-chan ChatResponse, error) {
	return nil, ErrStreamNotSupported
}

func (m *mockNoToolCallModel) CountTokens(msgs []*message.Msg, tools []ToolSchema) int { return 0 }

func TestGenerateStructuredOutput_InvalidJSON(t *testing.T) {
	mock := &mockStructuredModel{
		toolCallInput: `{invalid json`,
	}
	schema := json.RawMessage(`{"type":"object"}`)
	msgs := []*message.Msg{message.UserMsg("user", "test")}

	_, err := GenerateStructuredOutput(context.Background(), mock, msgs, schema)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
