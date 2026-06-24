package formatter

import "github.com/alanfokco/agentscope-go/pkg/agentscope/message"

// OllamaFormatter formats messages for Ollama's OpenAI-compatible API.
type OllamaFormatter struct {
	OpenAIFormatter
}

func NewOllamaFormatter() *OllamaFormatter {
	return &OllamaFormatter{
		OpenAIFormatter: OpenAIFormatter{SupportsThinking: false},
	}
}

func (f *OllamaFormatter) FormatMultiAgent(msgs []*message.Msg, currentAgent string) ([]map[string]any, error) {
	return formatMultiAgentOpenAI(f, msgs, currentAgent)
}
