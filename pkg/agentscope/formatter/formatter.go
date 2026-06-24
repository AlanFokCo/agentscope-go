package formatter

import (
	"strings"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

// Formatter transforms internal Msg objects into provider-specific API payloads.
type Formatter interface {
	// Format converts messages into the format expected by a model provider's API.
	Format(msgs []*message.Msg) ([]map[string]any, error)
}

// MultiAgentFormatter extends Formatter with multi-agent support.
// It handles injecting agent names, merging consecutive same-role messages,
// and system prompt interleaving for multi-agent conversations.
type MultiAgentFormatter interface {
	Formatter
	FormatMultiAgent(msgs []*message.Msg, currentAgent string) ([]map[string]any, error)
}

// formatMultiAgentOpenAI applies multi-agent formatting for OpenAI-compatible APIs:
// injects "name" field on user/assistant messages, merges consecutive same-role
// messages with name prefixes.
func formatMultiAgentOpenAI(base Formatter, msgs []*message.Msg, currentAgent string) ([]map[string]any, error) {
	formatted, err := base.Format(msgs)
	if err != nil {
		return nil, err
	}

	for i, m := range formatted {
		role, _ := m["role"].(string)
		if role == "user" || role == "assistant" {
			// Find the original msg to get the name
			if i < len(msgs) && msgs[i] != nil && msgs[i].Name != "" && msgs[i].Name != currentAgent {
				formatted[i]["name"] = sanitizeName(msgs[i].Name)
			}
		}
	}

	return mergeConsecutiveRoles(formatted), nil
}

func mergeConsecutiveRoles(msgs []map[string]any) []map[string]any {
	if len(msgs) <= 1 {
		return msgs
	}
	var result []map[string]any
	for _, m := range msgs {
		if len(result) > 0 {
			prev := result[len(result)-1]
			prevRole, _ := prev["role"].(string)
			curRole, _ := m["role"].(string)
			if prevRole == curRole && curRole != "tool" && curRole != "system" {
				prevContent, _ := prev["content"].(string)
				curContent, _ := m["content"].(string)
				name, _ := m["name"].(string)
				if name != "" {
					curContent = "[" + name + "]: " + curContent
				}
				prev["content"] = prevContent + "\n" + curContent
				continue
			}
		}
		result = append(result, m)
	}
	return result
}

func sanitizeName(name string) string {
	r := strings.NewReplacer(" ", "_", "-", "_", ".", "_")
	return r.Replace(name)
}
