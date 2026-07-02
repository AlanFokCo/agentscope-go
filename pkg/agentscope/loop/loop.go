// pkg/agentscope/loop/loop.go
package loop

import (
	"context"
	"fmt"

	agentscope "github.com/alanfokco/agentscope-go/pkg/agentscope"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/protocol"
)

// Loop is the universal agent loop. It drives the Reason-Inspect-Act state
// machine, emitting events over a channel for streaming consumers.
type Loop struct {
	cfg *Config
}

// New creates a Loop with the given options applied on top of defaults.
func New(opts ...Option) *Loop {
	cfg := defaultConfig()
	for _, o := range opts {
		o(cfg)
	}
	if cfg.ContextManager == nil {
		cfg.ContextManager = NewDefaultContextManager()
	}
	if cfg.Hooks == nil {
		cfg.Hooks = NewHookRunner()
	}
	return &Loop{cfg: cfg}
}

// Run starts the loop in a background goroutine and returns an event channel.
// The channel is closed when the loop finishes. Callers should range over the
// channel to receive all events.
func (l *Loop) Run(ctx context.Context, input string) <-chan event.Event {
	ch := make(chan event.Event, 64)
	go l.run(ctx, input, ch)
	return ch
}

// RunSync runs the loop synchronously and returns the final ChatResponse.
// It drains the event channel internally, extracting the final response from
// a CustomEvent with Name == "loop.final_response".
func (l *Loop) RunSync(ctx context.Context, input string) (*model.ChatResponse, error) {
	var (
		finalResp *model.ChatResponse
		loopErr   error
	)
	for ev := range l.Run(ctx, input) {
		if ce, ok := ev.(event.CustomEvent); ok {
			switch ce.Name {
			case "loop.final_response":
				if resp, ok := ce.Value["response"].(*model.ChatResponse); ok {
					finalResp = resp
				}
			case "loop.error":
				if errVal, ok := ce.Value["error"]; ok {
					if e, ok := errVal.(error); ok {
						loopErr = e
					} else {
						loopErr = fmt.Errorf("%v", errVal)
					}
				}
			}
		}
	}
	if loopErr != nil {
		return nil, loopErr
	}
	return finalResp, nil
}

// run is the core goroutine that drives the state machine.
func (l *Loop) run(ctx context.Context, input string, ch chan<- event.Event) {
	defer close(ch)

	// Check for context cancellation early.
	if err := ctx.Err(); err != nil {
		l.emitError(ch, "", err)
		return
	}

	if l.cfg.ModelCaller == nil {
		l.emitError(ch, "", fmt.Errorf("loop: ModelCaller is required"))
		return
	}

	replyID := agentscope.GenerateID()
	sessionID := ""

	l.cfg.Hooks.OnLoopStart()
	defer func() {
		l.cfg.Hooks.OnLoopEnd(nil)
	}()

	ch <- event.NewReplyStartEvent(sessionID, replyID, "", message.RoleAssistant)

	// Append user message to context.
	userMsg := message.UserMsg("user", input)
	l.cfg.ContextManager.Append(userMsg)

	// Optionally prepend system prompt.
	l.prependSystemPrompt()

	// Collect tool schemas if a provider is configured.
	var toolSchemas []model.ToolSchema
	if l.cfg.SchemaProvider != nil {
		toolSchemas = l.cfg.SchemaProvider.GetToolSchemas()
	}

	state := protocol.StateReason
	var lastResp *model.ChatResponse
	iter := 0

	for {
		if err := ctx.Err(); err != nil {
			l.backfillMissingResults()
			ch <- event.NewReplyEndEvent(sessionID, replyID)
			l.emitError(ch, replyID, err)
			return
		}

		switch state {
		case protocol.StateReason:
			if iter >= l.cfg.MaxIters {
				// Max iterations reached.
				l.backfillMissingResults()
				ch <- event.NewExceedMaxItersEvent(replyID, "")
				if lastResp != nil {
					ch <- event.NewCustomEvent(replyID, "loop.final_response", map[string]any{
						"response": lastResp,
					})
				}
				ch <- event.NewReplyEndEvent(sessionID, replyID)
				return
			}
			iter++
			// Compress context if needed.
			_ = l.cfg.ContextManager.Compress(ctx)

			// Model call.
			l.cfg.Hooks.BeforeModelCall(state, iter)
			ch <- event.NewModelCallStartEvent(replyID, "")

			msgs := l.cfg.ContextManager.Messages()
			resp, err := l.cfg.ModelCaller.Call(ctx, msgs, toolSchemas)

			if err != nil {
				l.cfg.Hooks.AfterModelCall(state, iter, err)
				inputTokens, outputTokens := 0, 0
				ch <- event.NewModelCallEndEvent(replyID, inputTokens, outputTokens)

				action := ErrorActionBreak
				if l.cfg.ErrorHandler != nil {
					action = l.cfg.ErrorHandler(err)
				}
				switch action {
				case ErrorActionRetry:
					continue
				case ErrorActionContinue:
					state = l.transition(state, protocol.StateReason, iter)
					continue
				default: // ErrorActionBreak
					l.backfillMissingResults()
					ch <- event.NewReplyEndEvent(sessionID, replyID)
					l.emitError(ch, replyID, err)
					return
				}
			}

			inputTokens, outputTokens := 0, 0
			if resp.Usage != nil {
				inputTokens = resp.Usage.InputTokens
				outputTokens = resp.Usage.OutputTokens
			}

			l.cfg.Hooks.AfterModelCall(state, iter, nil)
			ch <- event.NewModelCallEndEvent(replyID, inputTokens, outputTokens)

			// Emit content block events.
			l.emitContentEvents(ch, replyID, resp.Content)

			// Append assistant response to context.
			assistantMsg := resp.ToMsg("")
			l.cfg.ContextManager.Append(assistantMsg)

			lastResp = resp
			state = l.transition(state, protocol.StateInspect, iter)

		case protocol.StateInspect:
			if lastResp == nil {
				state = l.transition(state, protocol.StateExit, iter)
				continue
			}
			result := InspectResponse(lastResp.Content)

			// Check custom exit condition.
			if l.cfg.ExitCondition != nil && l.cfg.ExitCondition(lastResp) {
				state = l.transition(state, protocol.StateExit, iter)
				continue
			}

			switch result {
			case InspectHasTools:
				if l.cfg.ToolExecutor != nil {
					state = l.transition(state, protocol.StateAct, iter)
				} else {
					state = l.transition(state, protocol.StateExit, iter)
				}
			case InspectNeedsHITL:
				state = l.transition(state, protocol.StateWait, iter)
			default: // InspectNoTools
				state = l.transition(state, protocol.StateExit, iter)
			}

		case protocol.StateAct:
			// Extract tool calls from the last assistant message.
			calls := l.extractToolCalls(lastResp.Content)
			if len(calls) == 0 {
				state = l.transition(state, protocol.StateReason, iter)
				continue
			}

			// Execute tools.
			for _, call := range calls {
				l.cfg.Hooks.BeforeToolExec(state, iter, call.Name)
			}

			results := l.cfg.ToolExecutor.BatchExecute(ctx, calls)

			// Process results and emit events.
			var resultBlocks []message.ContentBlock
			for _, tr := range results {
				l.cfg.Hooks.AfterToolExec(state, iter, tr.Call.Name, tr.Err)

				ch <- event.NewToolResultStartEvent(replyID, tr.Call.ID, tr.Call.Name)

				var resultBlock message.ToolResultBlock
				if tr.Err != nil {
					resultBlock = message.ToolResultBlock{
						Type:   "tool_result",
						ID:     tr.Call.ID,
						Name:   tr.Call.Name,
						Output: []message.ContentBlock{message.TextBlock{Type: "text", Text: tr.Err.Error()}},
						State:  message.ToolResultError,
					}
					ch <- event.NewToolResultTextDeltaEvent(replyID, tr.Call.ID, tr.Err.Error())
				} else if tr.Response != nil {
					resultBlock = message.ToolResultBlock{
						Type:     "tool_result",
						ID:       tr.Call.ID,
						Name:     tr.Call.Name,
						Output:   tr.Response.Content,
						State:    tr.Response.State,
						Metadata: tr.Response.Metadata,
					}
					text := resultBlock.GetOutputText()
					if text != "" {
						ch <- event.NewToolResultTextDeltaEvent(replyID, tr.Call.ID, text)
					}
				} else {
					resultBlock = message.ToolResultBlock{
						Type:   "tool_result",
						ID:     tr.Call.ID,
						Name:   tr.Call.Name,
						Output: []message.ContentBlock{message.TextBlock{Type: "text", Text: "no response"}},
						State:  message.ToolResultError,
					}
				}

				ch <- event.NewToolResultEndEvent(replyID, tr.Call.ID, resultBlock.State)
				resultBlocks = append(resultBlocks, resultBlock)
			}

			// Append tool results as a single message.
			// Use NewMsg directly — UserMsg rejects ToolResultBlock content.
			toolMsg := message.NewMsg("", message.RoleUser, resultBlocks)
			l.cfg.ContextManager.Append(toolMsg)

			state = l.transition(state, protocol.StateReason, iter)

		case protocol.StateWait:
			// HITL: emit reply end and return; the caller will resume.
			l.backfillMissingResults()
			ch <- event.NewReplyEndEvent(sessionID, replyID)
			return

		case protocol.StateExit:
			l.backfillMissingResults()
			if lastResp != nil {
				ch <- event.NewCustomEvent(replyID, "loop.final_response", map[string]any{
					"response": lastResp,
				})
			}
			ch <- event.NewReplyEndEvent(sessionID, replyID)
			return
		}
	}

}

// prependSystemPrompt adds the system prompt as the first message if configured.
// This uses a type assertion on DefaultContextManager, which is intentionally
// fragile — external ContextManager implementations manage their own prompts.
func (l *Loop) prependSystemPrompt() {
	if l.cfg.SystemPrompt == "" {
		return
	}
	dcm, ok := l.cfg.ContextManager.(*DefaultContextManager)
	if !ok {
		return
	}
	sysMsg := message.SystemMsg("system", l.cfg.SystemPrompt)
	dcm.mu.Lock()
	dcm.messages = append([]*message.Msg{sysMsg}, dcm.messages...)
	dcm.mu.Unlock()
}

// extractToolCalls extracts ToolCallBlocks from content blocks.
func (l *Loop) extractToolCalls(content []message.ContentBlock) []message.ToolCallBlock {
	var calls []message.ToolCallBlock
	for _, b := range content {
		if tc, ok := b.(message.ToolCallBlock); ok {
			if tc.State == message.ToolCallPending || tc.State == message.ToolCallAllowed {
				calls = append(calls, tc)
			}
		}
	}
	return calls
}

// backfillMissingResults scans all messages for ToolCallBlocks without
// matching ToolResultBlocks and appends error results for them.
func (l *Loop) backfillMissingResults() {
	msgs := l.cfg.ContextManager.Messages()

	// Collect all tool call IDs and result IDs.
	callIDs := make(map[string]message.ToolCallBlock)
	resultIDs := make(map[string]bool)

	for _, m := range msgs {
		for _, b := range m.Content {
			switch blk := b.(type) {
			case message.ToolCallBlock:
				if blk.State != message.ToolCallAsking && blk.State != message.ToolCallSubmitted {
					callIDs[blk.ID] = blk
				}
			case message.ToolResultBlock:
				resultIDs[blk.ID] = true
			}
		}
	}

	// Find missing results and backfill.
	var missing []message.ContentBlock
	for id, call := range callIDs {
		if !resultIDs[id] {
			missing = append(missing, message.ToolResultBlock{
				Type:   "tool_result",
				ID:     id,
				Name:   call.Name,
				Output: []message.ContentBlock{message.TextBlock{Type: "text", Text: "tool execution was interrupted"}},
				State:  message.ToolResultError,
			})
		}
	}

	if len(missing) > 0 {
		backfillMsg := message.NewMsg("", message.RoleUser, missing)
		l.cfg.ContextManager.Append(backfillMsg)
	}
}

// emitContentEvents emits text and thinking block events for model response content.
func (l *Loop) emitContentEvents(ch chan<- event.Event, replyID string, content []message.ContentBlock) {
	for _, b := range content {
		switch blk := b.(type) {
		case message.TextBlock:
			blockID := agentscope.GenerateID()
			ch <- event.NewTextBlockStartEvent(replyID, blockID)
			if blk.Text != "" {
				ch <- event.NewTextBlockDeltaEvent(replyID, blockID, blk.Text)
			}
			ch <- event.NewTextBlockEndEvent(replyID, blockID)
		case message.ThinkingBlock:
			blockID := agentscope.GenerateID()
			ch <- event.NewThinkingBlockStartEvent(replyID, blockID)
			if blk.Thinking != "" {
				ch <- event.NewThinkingBlockDeltaEvent(replyID, blockID, blk.Thinking)
			}
			ch <- event.NewThinkingBlockEndEvent(replyID, blockID)
		case message.ToolCallBlock:
			ch <- event.NewToolCallStartEvent(replyID, blk.ID, blk.Name)
			if blk.Input != "" {
				ch <- event.NewToolCallDeltaEvent(replyID, blk.ID, blk.Input)
			}
			ch <- event.NewToolCallEndEvent(replyID, blk.ID)
		}
	}
}

func (l *Loop) transition(from, to protocol.LoopState, iter int) protocol.LoopState {
	l.cfg.Hooks.OnStateTransition(from, to, iter)
	return to
}

// emitError sends a custom error event.
func (l *Loop) emitError(ch chan<- event.Event, replyID string, err error) {
	ch <- event.NewCustomEvent(replyID, "loop.error", map[string]any{
		"error": err,
	})
}
