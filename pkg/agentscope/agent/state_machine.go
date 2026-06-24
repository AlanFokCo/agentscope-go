package agent

import (
	"strings"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
)

type replyAction int

const (
	actionReasoning replyAction = iota
	actionActing
	actionExit
)

// getLastAssistantMsg returns the last message in context if it belongs to
// this agent and has role "assistant". Otherwise returns nil.
func (a *UnifiedAgent) getLastAssistantMsg() *message.Msg {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.state.Context) == 0 {
		return nil
	}
	last := a.state.Context[len(a.state.Context)-1]
	if last.Role == message.RoleAssistant && last.Name == a.name {
		return last
	}
	return nil
}

// checkNextAction inspects the last assistant message's tool call states to
// determine the next loop action:
//   - actionActing: there are pending/allowed tool calls to execute
//   - actionExit: there are awaiting (asking/submitted) calls but nothing executable
//   - actionReasoning: no unfinished tool calls — ready for next model call
func (a *UnifiedAgent) checkNextAction() (replyAction, *message.Msg) {
	lastMsg := a.getLastAssistantMsg()
	if lastMsg == nil {
		return actionReasoning, nil
	}

	finishedIDs := make(map[string]bool)
	for _, b := range lastMsg.GetContentBlocks(message.ContentBlockToolResult) {
		if tr, ok := b.(message.ToolResultBlock); ok {
			finishedIDs[tr.ID] = true
		}
	}

	var executable, awaiting []message.ToolCallBlock
	for _, b := range lastMsg.GetContentBlocks(message.ContentBlockToolCall) {
		tc, ok := b.(message.ToolCallBlock)
		if !ok || finishedIDs[tc.ID] {
			continue
		}
		switch tc.State {
		case message.ToolCallPending, message.ToolCallAllowed:
			executable = append(executable, tc)
		case message.ToolCallAsking, message.ToolCallSubmitted:
			awaiting = append(awaiting, tc)
		}
	}

	if len(executable) > 0 {
		return actionActing, nil
	}

	if len(awaiting) > 0 {
		var parts []string
		askCount, subCount := 0, 0
		for _, tc := range awaiting {
			if tc.State == message.ToolCallAsking {
				askCount++
			} else {
				subCount++
			}
		}
		if askCount > 0 {
			parts = append(parts, "user confirmation")
		}
		if subCount > 0 {
			parts = append(parts, "external execution results")
		}
		text := "Waiting for " + strings.Join(parts, " and ") + "."
		exitMsg := message.AssistantMsg(a.name, text)
		return actionExit, exitMsg
	}

	return actionReasoning, nil
}

// getExecutableToolCalls returns tool calls from the last assistant message
// that are ready to execute (state pending or allowed, no result yet).
func (a *UnifiedAgent) getExecutableToolCalls() []message.ToolCallBlock {
	lastMsg := a.getLastAssistantMsg()
	if lastMsg == nil {
		return nil
	}

	resultIDs := make(map[string]bool)
	for _, b := range lastMsg.GetContentBlocks(message.ContentBlockToolResult) {
		if tr, ok := b.(message.ToolResultBlock); ok {
			resultIDs[tr.ID] = true
		}
	}

	var calls []message.ToolCallBlock
	for _, b := range lastMsg.GetContentBlocks(message.ContentBlockToolCall) {
		tc, ok := b.(message.ToolCallBlock)
		if !ok || resultIDs[tc.ID] {
			continue
		}
		if tc.State == message.ToolCallPending || tc.State == message.ToolCallAllowed {
			calls = append(calls, tc)
		}
	}
	return calls
}

// updateToolCallState mutates the State field of a ToolCallBlock in the last
// assistant message's content. This is an in-place mutation matching Python's
// behavior — the context holds the authoritative state.
func (a *UnifiedAgent) updateToolCallState(callID string, newState message.ToolCallState) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.state.Context) == 0 {
		return
	}
	last := a.state.Context[len(a.state.Context)-1]
	if last.Role != message.RoleAssistant || last.Name != a.name {
		return
	}
	for i, b := range last.Content {
		if tc, ok := b.(message.ToolCallBlock); ok && tc.ID == callID {
			tc.State = newState
			last.Content[i] = tc
			return
		}
	}
}

// saveToContext merges content blocks into the last assistant message in
// context (if it belongs to this agent), or creates a new one. Audio
// DataBlocks are filtered out. Usage is accumulated across calls.
func (a *UnifiedAgent) saveToContext(blocks []message.ContentBlock, usage *model.ChatUsage) {
	persisted := filterAudioBlocks(blocks)
	if len(persisted) == 0 && usage == nil {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.state.Context) > 0 {
		last := a.state.Context[len(a.state.Context)-1]
		if last.Role == message.RoleAssistant && last.Name == a.name {
			last.Content = append(last.Content, persisted...)
			if usage != nil {
				if last.Usage == nil {
					last.Usage = &message.Usage{
						InputTokens:  usage.InputTokens,
						OutputTokens: usage.OutputTokens,
					}
				} else {
					last.Usage.InputTokens += usage.InputTokens
					last.Usage.OutputTokens += usage.OutputTokens
				}
			}
			return
		}
	}

	msg := message.AssistantMsg(a.name, persisted)
	msg.ID = a.state.ReplyID // tie message ID to reply session
	if usage != nil {
		msg.Usage = &message.Usage{
			InputTokens:  usage.InputTokens,
			OutputTokens: usage.OutputTokens,
		}
	}
	a.state.Context = append(a.state.Context, msg)
}
