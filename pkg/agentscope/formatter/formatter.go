package formatter

import "github.com/alanfokco/agentscope-go/pkg/agentscope/message"

// Formatter transforms internal Msg objects into provider-specific API payloads.
type Formatter interface {
	// Format converts messages into the format expected by a model provider's API.
	Format(msgs []*message.Msg) ([]map[string]any, error)
}
