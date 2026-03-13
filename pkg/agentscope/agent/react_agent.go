package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/memory"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/rag"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

// ReActAgent is a simplified Go implementation of a ReAct-style agent,
// conceptually aligned with the Python ReActAgent: system prompt, model, tools,
// memory, and a reasoning-acting loop with a max iteration count.
type ReActAgent struct {
	AgentBase

	Name      string
	SysPrompt string

	Model   model.ChatModel
	Toolkit *tool.Toolkit
	Memory  memory.Store

	Knowledge   []rag.KnowledgeBase
	Compression *memory.CompressionConfig

	MaxIters int
}

// NewReActAgent constructs a ReActAgent.
// - name: agent name
// - sysPrompt: system prompt
// - m: underlying ChatModel
// - tk: toolkit (may be nil)
// - mem: conversation memory (may be nil; defaults to in-memory)
func NewReActAgent(
	name string,
	sysPrompt string,
	m model.ChatModel,
	tk *tool.Toolkit,
	mem memory.Store,
) *ReActAgent {
	if tk == nil {
		tk = tool.NewToolkit()
	}
	if mem == nil {
		mem = memory.NewInMemoryStore()
	}
	return &ReActAgent{
		AgentBase: NewAgentBase(),
		Name:      name,
		SysPrompt: sysPrompt,
		Model:     m,
		Toolkit:   tk,
		Memory:    mem,
		MaxIters:  4,
	}
}

// WithKnowledge attaches one or more knowledge bases (for RAG) to the agent.
func (a *ReActAgent) WithKnowledge(bases ...rag.KnowledgeBase) *ReActAgent {
	a.Knowledge = bases
	return a
}

// WithCompression configures a basic conversation compression strategy.
func (a *ReActAgent) WithCompression(cfg *memory.CompressionConfig) *ReActAgent {
	a.Compression = cfg
	return a
}

// toolCall describes the simple tool invocation protocol used by ReActAgent.
type toolCall struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

// Reply implements a simplified ReAct loop:
// 1. Load history from memory and append the current user question.
// 2. Call the model to get a reply.
// 3. If the reply is a tool call JSON {"tool": "...", "args": {...}}, execute the tool,
//    append the result to the context and call the model again.
// 4. Stop when no tool call is returned or MaxIters is reached.
func (a *ReActAgent) Reply(ctx context.Context, args ...any) (*message.Msg, error) {
	var userText string
	if len(args) > 0 {
		if s, ok := args[0].(string); ok {
			userText = s
		}
	}
	if userText == "" {
		return nil, fmt.Errorf("react agent: empty user input")
	}

	memKey := a.ID()
	history, _ := a.Memory.Load(ctx, memKey)

	userMsg := message.NewMsg(a.Name, message.RoleUser, userText)
	history = append(history, userMsg)

	// ReAct iteration loop.
	iters := 0
	for {
		if a.MaxIters > 0 && iters >= a.MaxIters {
			break
		}
		iters++

		// Optionally compress history.
		if a.Compression != nil {
			history = memory.CompressMessages(history, *a.Compression)
		}

		// Build system prompt and attach RAG context if available.
		sysPrompt := a.SysPrompt
		if len(a.Knowledge) > 0 {
			if ctxText := a.retrieveKnowledge(ctx, userText); ctxText != "" {
				sysPrompt = sysPrompt + "\n\n[KNOWLEDGE]\n" + ctxText
			}
		}

		systemMsg := message.NewMsg(a.Name, message.RoleSystem, sysPrompt)
		chatHistory := append([]*message.Msg{systemMsg}, history...)

		resp, err := a.Model.Chat(ctx, chatHistory)
		if err != nil {
			return nil, err
		}
		assistantMsg := resp.Msg
		if assistantMsg == nil {
			return nil, fmt.Errorf("react agent: nil response message")
		}

		// Try to parse the reply as a tool call.
		if call, ok := tryParseToolCall(assistantMsg); ok && call.Tool != "" {
			// Execute tool.
			t := a.Toolkit.Get(call.Tool)
			if t == nil {
				// Tool not found; treat model reply as final answer.
				history = append(history, assistantMsg)
				break
			}
			result, err := t.Execute(ctx, call.Args)
			if err != nil {
				result = fmt.Sprintf("tool %s error: %v", t.Name, err)
			}
			// Append tool result to context so the model can reason over it.
			resultBytes, _ := json.Marshal(result)
			toolMsg := message.NewMsg(
				a.Name,
				message.RoleAssistant,
				fmt.Sprintf("TOOL[%s] RESULT: %s", t.Name, string(resultBytes)),
			)
			history = append(history, assistantMsg, toolMsg)
			continue
		}

		// Non-tool reply is treated as the final answer.
		history = append(history, assistantMsg)
		_ = a.Memory.Save(ctx, memKey, userMsg)
		_ = a.Memory.Save(ctx, memKey, assistantMsg)

		// Print final reply.
		_ = a.Print(ctx, assistantMsg)
		return assistantMsg, nil
	}

	// If the loop ends without a clear final answer, return the last message.
	if len(history) == 0 {
		return nil, fmt.Errorf("react agent: no messages in history")
	}
	last := history[len(history)-1]
	_ = a.Print(ctx, last)
	return last, nil
}

// Observe is a no-op for now and can be extended as needed.
func (a *ReActAgent) Observe(ctx context.Context, msgs []*message.Msg) error {
	_ = ctx
	_ = msgs
	return nil
}

// retrieveKnowledge queries configured knowledge bases and concatenates results into a text hint.
func (a *ReActAgent) retrieveKnowledge(ctx context.Context, query string) string {
	if len(a.Knowledge) == 0 || query == "" {
		return ""
	}
	const topK = 3
	type kbSnippet struct {
		name string
		text string
	}
	var snippets []kbSnippet
	for _, kb := range a.Knowledge {
		if kb == nil {
			continue
		}
		docs, err := kb.Query(ctx, query, topK)
		if err != nil || len(docs) == 0 {
			continue
		}
		var text string
		for i, d := range docs {
			if i >= topK {
				break
			}
			if text != "" {
				text += "\n---\n"
			}
			text += d.Content
		}
		if text != "" {
			name := kb.Name()
			if name == "" {
				name = "kb"
			}
			snippets = append(snippets, kbSnippet{name: name, text: text})
		}
	}
	if len(snippets) == 0 {
		return ""
	}
	out := ""
	for _, s := range snippets {
		if out != "" {
			out += "\n\n"
		}
		out += fmt.Sprintf("[%s]\n%s", s.name, s.text)
	}
	return out
}

// tryParseToolCall attempts to parse the assistant Msg content as a tool call JSON.
// Convention: if the assistant returns a JSON string like {"tool": "...", "args": {...}},
// it is treated as a tool invocation.
func tryParseToolCall(msg *message.Msg) (*toolCall, bool) {
	if msg == nil {
		return nil, false
	}
	textPtr := msg.GetTextContent("\n")
	if textPtr == nil {
		return nil, false
	}
	text := *textPtr
	var call toolCall
	if err := json.Unmarshal([]byte(text), &call); err != nil {
		return nil, false
	}
	return &call, true
}

