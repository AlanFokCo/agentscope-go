package model

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/internal/jsonx"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

const structuredOutputToolName = "generate_structured_output"

// GenerateStructuredOutput uses a model to produce JSON conforming to the given schema.
// It works by injecting a synthetic tool with forced tool_choice, then extracting
// the tool call arguments as the structured result.
func GenerateStructuredOutput(ctx context.Context, model ChatModel, msgs []*message.Msg, schema json.RawMessage) (json.RawMessage, error) {
	tool := ToolSchema{
		Type: "function",
		Function: ToolFunction{
			Name:        structuredOutputToolName,
			Description: "Generate a structured JSON output conforming to the provided schema. You MUST call this tool with the result.",
			Parameters:  schema,
		},
	}

	opts := []CallOption{
		WithTools([]ToolSchema{tool}),
		WithToolChoice(&ToolChoice{Mode: "required"}),
	}

	resp, err := model.Chat(ctx, msgs, opts...)
	if err != nil {
		return nil, fmt.Errorf("structured output: model call failed: %w", err)
	}

	// Extract the tool call arguments from the response
	for _, block := range resp.Content {
		tc, ok := block.(message.ToolCallBlock)
		if !ok {
			continue
		}
		if tc.Name == structuredOutputToolName {
			raw := json.RawMessage(tc.Input)
			if json.Valid(raw) {
				return raw, nil
			}
			// Try JSON repair
			var repaired any
			if err := jsonx.RepairAndUnmarshal([]byte(tc.Input), &repaired); err == nil {
				if b, err := json.Marshal(repaired); err == nil {
					return json.RawMessage(b), nil
				}
			}
			return nil, fmt.Errorf("structured output: invalid JSON in tool call arguments")
		}
	}

	return nil, fmt.Errorf("structured output: model did not produce a tool call")
}
