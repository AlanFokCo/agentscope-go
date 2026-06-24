package formatter

import (
	"encoding/json"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

// GeminiFormatter formats messages for Google Gemini's native API.
// Gemini uses a different structure: role is "user"/"model", content is a
// "parts" array containing text, functionCall, and functionResponse objects.
type GeminiFormatter struct{}

func NewGeminiFormatter() *GeminiFormatter {
	return &GeminiFormatter{}
}

func (f *GeminiFormatter) Format(msgs []*message.Msg) ([]map[string]any, error) {
	var result []map[string]any
	for _, msg := range msgs {
		if msg == nil || msg.Role == message.RoleSystem {
			continue
		}
		formatted := f.formatMsg(msg)
		if formatted != nil {
			result = append(result, formatted)
		}
	}
	return result, nil
}

func (f *GeminiFormatter) FormatMultiAgent(msgs []*message.Msg, currentAgent string) ([]map[string]any, error) {
	formatted, err := f.Format(msgs)
	if err != nil {
		return nil, err
	}
	for i, m := range formatted {
		if i < len(msgs) && msgs[i] != nil && msgs[i].Name != "" && msgs[i].Name != currentAgent {
			parts, _ := m["parts"].([]map[string]any)
			namePrefix := map[string]any{"text": "[" + msgs[i].Name + "]: "}
			m["parts"] = append([]map[string]any{namePrefix}, parts...)
		}
	}
	return formatted, nil
}

func (f *GeminiFormatter) formatMsg(msg *message.Msg) map[string]any {
	role := "user"
	if msg.Role == message.RoleAssistant {
		role = "model"
	}

	blocks := msg.GetContentBlocks()
	if len(blocks) == 0 {
		return map[string]any{
			"role":  role,
			"parts": []map[string]any{{"text": ""}},
		}
	}

	var parts []map[string]any
	for _, b := range blocks {
		switch blk := b.(type) {
		case message.TextBlock:
			parts = append(parts, map[string]any{"text": blk.Text})
		case message.ThinkingBlock:
			parts = append(parts, map[string]any{
				"thought": true,
				"text":    blk.Thinking,
			})
		case message.ToolCallBlock:
			var args any
			if err := json.Unmarshal([]byte(blk.Input), &args); err != nil {
				args = map[string]any{}
			}
			parts = append(parts, map[string]any{
				"functionCall": map[string]any{
					"name": blk.Name,
					"args": args,
				},
			})
		case message.ToolResultBlock:
			parts = append(parts, map[string]any{
				"functionResponse": map[string]any{
					"name":     blk.Name,
					"response": map[string]any{"result": blk.GetOutputText()},
				},
			})
		case message.HintBlock:
			parts = append(parts, map[string]any{"text": blk.GetHintText()})
		case message.DataBlock:
			if formatted := formatGeminiDataBlock(blk); formatted != nil {
				parts = append(parts, formatted)
			}
		}
	}

	if len(parts) == 0 {
		return nil
	}

	return map[string]any{
		"role":  role,
		"parts": parts,
	}
}

func formatGeminiDataBlock(blk message.DataBlock) map[string]any {
	mt := blk.GetMediaType()
	switch src := blk.Source.(type) {
	case message.Base64Source:
		return map[string]any{
			"inlineData": map[string]any{
				"mimeType": mt,
				"data":     src.Data,
			},
		}
	case message.URLSource:
		return map[string]any{
			"fileData": map[string]any{
				"mimeType": mt,
				"fileUri":  src.URL,
			},
		}
	}
	return nil
}

// ExtractGeminiSystemInstruction extracts the system message for Gemini.
// Gemini passes system instructions separately via systemInstruction field.
func ExtractGeminiSystemInstruction(msgs []*message.Msg) map[string]any {
	for _, msg := range msgs {
		if msg != nil && msg.Role == message.RoleSystem {
			if txt := msg.GetTextContent("\n"); txt != nil {
				return map[string]any{
					"parts": []map[string]any{{"text": *txt}},
				}
			}
		}
	}
	return nil
}
