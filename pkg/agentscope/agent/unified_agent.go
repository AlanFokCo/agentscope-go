package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/exception"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/middleware"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/skill"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

const (
	defaultUnifiedMaxIters = 20
	defaultTriggerRatio    = 0.8
)

// UnifiedAgent is the v2 agent that uses native tool calling and supports streaming.
// It aligns with the Python AgentScope v2.0+ single Agent class design.
type UnifiedAgent struct {
	name         string
	systemPrompt string
	model        model.ChatModel
	toolkit      *tool.Toolkit
	state        *AgentState
	middlewares  []middleware.Middleware
	reactCfg     ReactConfig
	modelCfg     ModelConfig
	contextCfg   *ContextConfig
	readCache    *tool.ReadCache
	engine       *permission.Engine
	skills       []skill.Skill

	confirmCh  chan event.UserConfirmResultEvent
	externalCh chan event.ExternalExecutionResultEvent
	mu         sync.Mutex
	offloader  Offloader
}

// ReactConfig controls the ReAct reasoning-acting loop.
type ReactConfig struct {
	MaxIters     int
	StopOnReject bool
}

// ModelConfig controls model call behavior.
type ModelConfig struct {
	MaxRetries    int
	RetryDelay    time.Duration
	FallbackModel model.ChatModel
}

// AgentState holds conversation state for a session.
type AgentState struct {
	SessionID string
	Context   []*message.Msg
	Summary   string
	ReplyID   string
	CurIter   int

	PermissionCtx   *permission.Context `json:"permission_context,omitempty"`
	ToolCtx         *ToolStateContext   `json:"tool_context,omitempty"`
	TasksCtx        *TasksStateContext  `json:"tasks_context,omitempty"`
	MiddlewareState map[string]any      `json:"middleware_context,omitempty"`
	ReadCacheData   json.RawMessage     `json:"read_cache_data,omitempty"`
}

// ToolStateContext captures which tool groups are active for serialization.
type ToolStateContext struct {
	ActivatedGroups []string `json:"activated_groups"`
}

// TasksStateContext captures in-flight tasks for serialization.
type TasksStateContext struct {
	Tasks []TaskState `json:"tasks"`
}

// TaskState is the serializable form of a task.
type TaskState struct {
	ID          string   `json:"id"`
	Subject     string   `json:"subject"`
	Description string   `json:"description,omitempty"`
	State       string   `json:"state"`
	Owner       string   `json:"owner,omitempty"`
	Blocks      []string `json:"blocks,omitempty"`
	BlockedBy   []string `json:"blocked_by,omitempty"`
}

// AgentOption configures a UnifiedAgent.
type AgentOption func(*UnifiedAgent)

// WithToolkit sets the agent's toolkit.
func WithToolkit(tk *tool.Toolkit) AgentOption {
	return func(a *UnifiedAgent) { a.toolkit = tk }
}

// WithReactConfig sets the ReAct loop configuration.
func WithReactConfig(cfg ReactConfig) AgentOption {
	return func(a *UnifiedAgent) { a.reactCfg = cfg }
}

// WithModelConfig sets model call configuration.
func WithModelConfig(cfg ModelConfig) AgentOption {
	return func(a *UnifiedAgent) { a.modelCfg = cfg }
}

// WithState sets an initial agent state (for session recovery).
func WithState(state *AgentState) AgentOption {
	return func(a *UnifiedAgent) { a.state = state }
}

// WithMiddlewares sets the middleware chain for the agent.
// Middlewares are applied in order: middlewares[0] is outermost.
func WithMiddlewares(mws ...middleware.Middleware) AgentOption {
	return func(a *UnifiedAgent) { a.middlewares = mws }
}

// WithContextConfig enables structured context compression.
// When set, the agent compresses its context window when token count exceeds
// the trigger ratio, using a model-generated structured summary.
// Also creates a ReadCache for file caching if none is already set.
func WithContextConfig(cfg ContextConfig) AgentOption {
	return func(a *UnifiedAgent) {
		c := cfg.withDefaults()
		a.contextCfg = &c
		if a.readCache == nil {
			a.readCache = tool.NewReadCache(0, 0)
		}
	}
}

// WithReadCache sets a custom ReadCache for the agent.
// The cache is injected into the context for built-in tools.
func WithReadCache(rc *tool.ReadCache) AgentOption {
	return func(a *UnifiedAgent) { a.readCache = rc }
}

// WithPermissionContext configures a permission engine for the agent.
// When set, tool calls are checked against the engine before execution.
func WithPermissionContext(ctx *permission.Context) AgentOption {
	return func(a *UnifiedAgent) {
		a.engine = permission.NewEngine(ctx)
		a.confirmCh = make(chan event.UserConfirmResultEvent, 1)
		a.externalCh = make(chan event.ExternalExecutionResultEvent, 1)
	}
}

// WithSkills sets the available skills for system prompt injection.
func WithSkills(skills []skill.Skill) AgentOption {
	return func(a *UnifiedAgent) {
		a.skills = skills
	}
}

// Offloader writes content to an external store and returns its path.
type Offloader interface {
	OffloadContent(ctx context.Context, content string, filename string) (path string, err error)
	OffloadToolResult(ctx context.Context, content string, toolCallID string) (path string, err error)
}

// WithOffloader sets the offloader used during context compression and tool
// result truncation. Compressed context and oversized tool results are
// offloaded to the workspace so they can be referenced later.
func WithOffloader(o Offloader) AgentOption {
	return func(a *UnifiedAgent) { a.offloader = o }
}

// NewUnifiedAgent creates the v2 unified agent.
func NewUnifiedAgent(name, systemPrompt string, m model.ChatModel, opts ...AgentOption) *UnifiedAgent {
	a := &UnifiedAgent{
		name:         name,
		systemPrompt: systemPrompt,
		model:        m,
		toolkit:      tool.NewToolkit(),
		reactCfg:     ReactConfig{MaxIters: defaultUnifiedMaxIters},
		state:        &AgentState{SessionID: uuid.New().String()},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Name returns the agent name.
func (a *UnifiedAgent) Name() string { return a.name }

// Reply processes input and returns the final assistant message (synchronous).
func (a *UnifiedAgent) Reply(ctx context.Context, input string) (*message.Msg, error) {
	ch, err := a.ReplyStream(ctx, input)
	if err != nil {
		return nil, err
	}

	// Consume all events
	for range ch {
	}

	// After stream ends, find the last assistant message with text content
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := len(a.state.Context) - 1; i >= 0; i-- {
		msg := a.state.Context[i]
		if msg.Role == message.RoleAssistant && msg.Name == a.name {
			return msg, nil
		}
	}
	return nil, fmt.Errorf("agent %s: no response generated", a.name)
}

// ReplyStream processes input and returns a channel of events (streaming).
func (a *UnifiedAgent) ReplyStream(ctx context.Context, input string) (<-chan event.Event, error) {
	if input == "" {
		return nil, fmt.Errorf("agent %s: empty input", a.name)
	}

	// Attach MiddleContext to the Go context for middleware state storage.
	mc := middleware.MiddleContext{}
	ctx = middleware.WithMiddleContext(ctx, mc)

	if a.readCache != nil {
		ctx = tool.WithReadCache(ctx, a.readCache)
	}

	if len(a.middlewares) == 0 {
		ch := make(chan event.Event, 32)
		go a.replyLoop(ctx, input, ch)
		return ch, nil
	}

	// Wrap replyLoop through the OnReply middleware chain.
	core := func(ctx context.Context, ri middleware.ReplyInput) <-chan event.Event {
		ch := make(chan event.Event, 32)
		go a.replyLoop(ctx, ri.UserInput, ch)
		return ch
	}
	chain := middleware.BuildReplyChain(a.middlewares, core)
	return chain(ctx, middleware.ReplyInput{
		AgentName: a.name,
		UserInput: input,
	}), nil
}

// Observe injects external messages into the agent's context.
// It validates each message: system-role messages and messages containing
// tool_call, tool_result, or thinking blocks are rejected.
func (a *UnifiedAgent) Observe(ctx context.Context, msgs []*message.Msg) error {
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if m.Role == message.RoleSystem {
			return fmt.Errorf("observe: system-role messages are not allowed")
		}
		if len(m.GetContentBlocks(message.ContentBlockToolCall)) > 0 {
			return fmt.Errorf("observe: messages containing tool_call blocks are not allowed")
		}
		if len(m.GetContentBlocks(message.ContentBlockToolResult)) > 0 {
			return fmt.Errorf("observe: messages containing tool_result blocks are not allowed")
		}
		if len(m.GetContentBlocks(message.ContentBlockThinking)) > 0 {
			return fmt.Errorf("observe: messages containing thinking blocks are not allowed")
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	for _, m := range msgs {
		if m != nil {
			a.state.Context = append(a.state.Context, m)
		}
	}
	return nil
}

// SubmitUserConfirm submits a user confirmation result for pending tool calls.
// Call this after receiving a RequireUserConfirmEvent from the event stream.
func (a *UnifiedAgent) SubmitUserConfirm(result event.UserConfirmResultEvent) {
	if a.confirmCh != nil {
		a.confirmCh <- result
	}
}

// SubmitExternalResult submits results from external tool execution.
// Call this after receiving a RequireExternalExecutionEvent from the event stream.
func (a *UnifiedAgent) SubmitExternalResult(result event.ExternalExecutionResultEvent) {
	if a.externalCh != nil {
		a.externalCh <- result
	}
}

// PermissionEngine returns the agent's permission engine, or nil if not configured.
func (a *UnifiedAgent) PermissionEngine() *permission.Engine {
	return a.engine
}

// ReadCache returns the agent's file read cache, or nil if not configured.
func (a *UnifiedAgent) ReadCache() *tool.ReadCache {
	return a.readCache
}

func (a *UnifiedAgent) replyLoop(ctx context.Context, input string, ch chan<- event.Event) {
	defer close(ch)

	replyID := uuid.New().String()

	a.mu.Lock()
	a.state.ReplyID = replyID
	a.state.CurIter = 0
	a.mu.Unlock()

	emit(ctx, ch, event.NewReplyStartEvent(a.state.SessionID, replyID, a.name, message.RoleAssistant))

	userMsg := message.UserMsg(a.name, input)
	a.mu.Lock()
	a.state.Context = append(a.state.Context, userMsg)
	a.mu.Unlock()

	modelCallHandler := a.buildModelCallHandler()
	actingHandler := a.buildActingHandler()

reactLoop:
	for iter := 0; iter < a.reactCfg.MaxIters; iter++ {
		a.mu.Lock()
		a.state.CurIter = iter
		a.mu.Unlock()

		// State machine: decide next action based on tool call states
		action, exitMsg := a.checkNextAction()

		switch action {
		case actionExit:
			// Waiting for external events — emit to consumer, don't save to context
			if exitMsg != nil {
				emitContentEvents(ctx, ch, replyID, exitMsg.Content)
			}
			emit(ctx, ch, event.NewReplyEndEvent(a.state.SessionID, replyID))
			return

		case actionActing:
			// Execute pending tool calls from prior iteration
			toolCalls := a.getExecutableToolCalls()
			batches := batchToolCalls(toolCalls, a.toolkit)
			for _, batch := range batches {
				if batch.concurrent && len(batch.calls) > 1 {
					a.executeConcurrentBatch(ctx, ch, replyID, batch.calls, actingHandler)
				} else {
					for _, tc := range batch.calls {
						a.executeAndRecord(ctx, ch, replyID, tc, actingHandler)
					}
				}
			}
			continue

		case actionReasoning:
			// Compress context before model call
			if err := a.compressContext(ctx); err != nil {
				logrus.WithError(err).WithField("agent", a.name).Warn("context compression failed")
			}

			modelMsgs := a.prepareModelInput(ctx)
			schemas := a.toolkit.GetToolSchemas()

			emit(ctx, ch, event.NewModelCallStartEvent(replyID, ""))

			resp, err := modelCallHandler(ctx, middleware.ModelCallInput{
				AgentName: a.name,
				Messages:  modelMsgs,
				Tools:     schemas,
			})
			if err != nil {
				logrus.WithError(err).Error("agent: model call failed")
				emit(ctx, ch, event.NewModelCallEndEvent(replyID, 0, 0))
				break reactLoop
			}

			var inputTok, outputTok int
			if resp.Usage != nil {
				inputTok = resp.Usage.InputTokens
				outputTok = resp.Usage.OutputTokens
			}
			emit(ctx, ch, event.NewModelCallEndEvent(replyID, inputTok, outputTok))

			// Save response to context (merges into last assistant msg)
			a.saveToContext(resp.Content, resp.Usage)

			// Emit streaming events for consumers
			emitContentEvents(ctx, ch, replyID, resp.Content)

			// If no tool calls, this is the final answer
			toolCalls := extractToolCalls(resp.Content)
			if len(toolCalls) == 0 {
				break reactLoop
			}

			// Execute tool calls
			batches := batchToolCalls(toolCalls, a.toolkit)
			for _, batch := range batches {
				if batch.concurrent && len(batch.calls) > 1 {
					a.executeConcurrentBatch(ctx, ch, replyID, batch.calls, actingHandler)
				} else {
					for _, tc := range batch.calls {
						a.executeAndRecord(ctx, ch, replyID, tc, actingHandler)
					}
				}
			}
		}

		if iter == a.reactCfg.MaxIters-1 {
			emit(ctx, ch, event.NewExceedMaxItersEvent(replyID, a.name))
		}
	}

	emit(ctx, ch, event.NewReplyEndEvent(a.state.SessionID, replyID))
}

// emitContentEvents emits streaming events for content blocks.
func emitContentEvents(ctx context.Context, ch chan<- event.Event, replyID string, content []message.ContentBlock) {
	for _, b := range content {
		switch blk := b.(type) {
		case message.TextBlock:
			emit(ctx, ch, event.NewTextBlockStartEvent(replyID, blk.ID))
			emit(ctx, ch, event.NewTextBlockDeltaEvent(replyID, blk.ID, blk.Text))
			emit(ctx, ch, event.NewTextBlockEndEvent(replyID, blk.ID))
		case message.ThinkingBlock:
			emit(ctx, ch, event.NewThinkingBlockStartEvent(replyID, blk.ID))
			emit(ctx, ch, event.NewThinkingBlockDeltaEvent(replyID, blk.ID, blk.Thinking))
			emit(ctx, ch, event.NewThinkingBlockEndEvent(replyID, blk.ID))
		case message.DataBlock:
			if src, ok := blk.Source.(message.Base64Source); ok {
				emit(ctx, ch, event.NewDataBlockStartEvent(replyID, blk.ID, src.MediaType))
				emit(ctx, ch, event.NewDataBlockDeltaEvent(replyID, blk.ID, src.Data, src.MediaType))
				emit(ctx, ch, event.NewDataBlockEndEvent(replyID, blk.ID))
			}
		}
	}
}

// buildModelCallHandler returns a ModelCallHandler wrapped with middleware.
func (a *UnifiedAgent) buildModelCallHandler() middleware.ModelCallHandler {
	core := func(ctx context.Context, input middleware.ModelCallInput) (*model.ChatResponse, error) {
		var opts []model.CallOption
		if len(input.Tools) > 0 {
			opts = append(opts, model.WithTools(input.Tools))
		}
		if input.ToolChoice != nil {
			opts = append(opts, model.WithToolChoice(input.ToolChoice))
		}
		return a.callModel(ctx, input.Messages, opts)
	}
	if len(a.middlewares) == 0 {
		return core
	}
	return middleware.BuildModelCallChain(a.middlewares, core)
}

// buildActingHandler returns an ActingHandler wrapped with middleware.
func (a *UnifiedAgent) buildActingHandler() middleware.ActingHandler {
	core := func(ctx context.Context, input middleware.ActingInput) (*tool.ToolResponse, error) {
		return a.toolkit.CallToolFromBlock(ctx, input.ToolCall)
	}
	if len(a.middlewares) == 0 {
		return core
	}
	return middleware.BuildActingChain(a.middlewares, core)
}

// executeToolCallWithPermission checks permissions before executing a tool call.
// Returns the result state and output text.
func (a *UnifiedAgent) executeToolCallWithPermission(
	ctx context.Context,
	ch chan<- event.Event,
	replyID string,
	tc message.ToolCallBlock,
	actingHandler middleware.ActingHandler,
) (message.ToolResultState, string) {
	if a.engine != nil && tc.State != message.ToolCallAllowed {
		t := a.toolkit.Get(tc.Name)
		if t == nil {
			return a.emitToolResult(ctx, ch, replyID, tc, message.ToolResultError,
				fmt.Sprintf("tool %q not found or not active", tc.Name))
		}

		input, err := tc.ParseInput()
		if err != nil {
			return a.emitToolResult(ctx, ch, replyID, tc, message.ToolResultError,
				fmt.Sprintf("parse tool input: %v", err))
		}

		decision, err := a.engine.CheckPermission(t, input)
		if err != nil {
			return a.emitToolResult(ctx, ch, replyID, tc, message.ToolResultError,
				fmt.Sprintf("permission check error: %v", err))
		}

		switch decision.Behavior {
		case permission.BehaviorDeny:
			return a.emitToolResult(ctx, ch, replyID, tc, message.ToolResultDenied,
				decision.Message)

		case permission.BehaviorAsk, permission.BehaviorPassthrough:
			a.updateToolCallState(tc.ID, message.ToolCallAsking)
			tc.SuggestedRules = rulesToAny(decision.SuggestedRules)
			emit(ctx, ch, event.NewRequireUserConfirmEvent(replyID, []message.ToolCallBlock{tc}))

			// Block waiting for user confirmation
			confirmed, resultTC := a.waitForConfirmation(ctx, tc.ID)
			if !confirmed {
				a.updateToolCallState(tc.ID, message.ToolCallFinished)
				return a.emitToolResult(ctx, ch, replyID, tc, message.ToolResultDenied,
					"Permission denied by user")
			}
			a.updateToolCallState(tc.ID, message.ToolCallAllowed)
			tc = resultTC
		}
	}

	// Check if this is an external tool — pause and wait for external result.
	t := a.toolkit.Get(tc.Name)
	if t != nil && t.IsExternalTool() {
		a.updateToolCallState(tc.ID, message.ToolCallSubmitted)
		emit(ctx, ch, event.NewToolResultStartEvent(replyID, tc.ID, tc.Name))
		emit(ctx, ch, event.NewRequireExternalExecutionEvent(replyID, []message.ToolCallBlock{tc}))
		result := a.waitForExternalResult(ctx, tc.ID)
		if result == nil {
			return a.emitToolResult(ctx, ch, replyID, tc, message.ToolResultError,
				"External execution timed out or cancelled")
		}
		outputText := result.GetOutputText()
		emit(ctx, ch, event.NewToolResultTextDeltaEvent(replyID, tc.ID, outputText))
		emit(ctx, ch, event.NewToolResultEndEvent(replyID, tc.ID, result.State))
		return result.State, outputText
	}

	return a.executeTool(ctx, ch, replyID, tc, actingHandler)
}

func (a *UnifiedAgent) waitForConfirmation(ctx context.Context, toolCallID string) (bool, message.ToolCallBlock) {
	select {
	case result := <-a.confirmCh:
		for _, cr := range result.ConfirmResults {
			if cr.ToolCall.ID == toolCallID {
				if cr.Confirmed {
					// Add any user-provided rules to the engine
					for _, r := range cr.Rules {
						if rule, ok := r.(permission.Rule); ok {
							a.engine.AddRule(rule)
						}
					}
					return true, cr.ToolCall
				}
				return false, cr.ToolCall
			}
		}
		return false, message.ToolCallBlock{}
	case <-ctx.Done():
		return false, message.ToolCallBlock{}
	}
}

func (a *UnifiedAgent) waitForExternalResult(ctx context.Context, toolCallID string) *message.ToolResultBlock {
	if a.externalCh == nil {
		return nil
	}
	select {
	case result := <-a.externalCh:
		for _, tr := range result.ExecutionResults {
			if tr.ID == toolCallID {
				return &tr
			}
		}
		return nil
	case <-ctx.Done():
		return nil
	}
}

func (a *UnifiedAgent) executeTool(
	ctx context.Context,
	ch chan<- event.Event,
	replyID string,
	tc message.ToolCallBlock,
	actingHandler middleware.ActingHandler,
) (message.ToolResultState, string) {
	toolResp, execErr := actingHandler(ctx, middleware.ActingInput{
		AgentName: a.name,
		ToolCall:  tc,
	})

	var resultState message.ToolResultState
	var outputText string
	if execErr != nil {
		// DeveloperErrors propagate up; AgentErrors become tool results for the LLM
		if _, ok := execErr.(exception.DeveloperError); ok {
			logrus.WithError(execErr).Error("agent: developer error in tool execution")
		}
		resultState = message.ToolResultError
		outputText = exception.GetAgentMessage(execErr)
	} else if toolResp != nil {
		resultState = toolResp.State
		for _, b := range toolResp.Content {
			if tb, ok := b.(message.TextBlock); ok {
				outputText += tb.Text
			}
		}
	} else {
		resultState = message.ToolResultSuccess
	}

	if a.contextCfg != nil && a.contextCfg.ToolResultLimit > 0 {
		truncated, wasTruncated := TruncateToolResult(outputText, a.contextCfg.ToolResultLimit)
		if wasTruncated && a.offloader != nil {
			path, offErr := a.offloader.OffloadContent(ctx, outputText,
				fmt.Sprintf("tool_result_%s.txt", tc.ID))
			if offErr == nil {
				truncated += fmt.Sprintf(
					"\n<system-reminder>The remaining content has been offloaded to '%s'.</system-reminder>",
					path,
				)
			}
		}
		outputText = truncated
	}

	return a.emitToolResult(ctx, ch, replyID, tc, resultState, outputText)
}

func (a *UnifiedAgent) emitToolResult(
	ctx context.Context,
	ch chan<- event.Event,
	replyID string,
	tc message.ToolCallBlock,
	state message.ToolResultState,
	text string,
) (message.ToolResultState, string) {
	emit(ctx, ch, event.NewToolResultStartEvent(replyID, tc.ID, tc.Name))
	if text != "" {
		emit(ctx, ch, event.NewToolResultTextDeltaEvent(replyID, tc.ID, text))
	}
	emit(ctx, ch, event.NewToolResultEndEvent(replyID, tc.ID, state))
	return state, text
}

func rulesToAny(rules []permission.Rule) []any {
	if len(rules) == 0 {
		return nil
	}
	out := make([]any, len(rules))
	for i, r := range rules {
		out[i] = r
	}
	return out
}

func (a *UnifiedAgent) prepareModelInput(ctx context.Context) []*message.Msg {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Apply OnSystemPrompt pipeline through middleware
	prompt := a.systemPrompt
	if len(a.middlewares) > 0 {
		prompt = middleware.ApplySystemPromptPipeline(ctx, a.middlewares, a.name, prompt)
	}

	// Append skill instructions if skills are configured
	if len(a.skills) > 0 {
		instructions := skill.FormatSkillInstructions(a.skills)
		if instructions != "" {
			prompt = prompt + "\n\n" + instructions
		}
	}

	sysMsg := message.SystemMsg(a.name, prompt)
	msgs := make([]*message.Msg, 0, len(a.state.Context)+2)
	msgs = append(msgs, sysMsg)

	if a.state.Summary != "" {
		msgs = append(msgs, message.UserMsg(a.name, "[Previous context summary]: "+a.state.Summary))
	}

	msgs = append(msgs, a.state.Context...)
	return msgs
}

func (a *UnifiedAgent) callModel(ctx context.Context, msgs []*message.Msg, opts []model.CallOption) (*model.ChatResponse, error) {
	retries := a.modelCfg.MaxRetries
	if retries <= 0 {
		retries = 1
	}
	delay := a.modelCfg.RetryDelay
	if delay == 0 {
		delay = time.Second
	}

	var lastErr error
	for i := 0; i < retries; i++ {
		if i > 0 {
			time.Sleep(delay)
		}
		resp, err := a.model.Chat(ctx, msgs, opts...)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	if a.modelCfg.FallbackModel != nil {
		resp, err := a.modelCfg.FallbackModel.Chat(ctx, msgs, opts...)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

func extractToolCalls(content []message.ContentBlock) []message.ToolCallBlock {
	var calls []message.ToolCallBlock
	for _, b := range content {
		if tc, ok := b.(message.ToolCallBlock); ok {
			calls = append(calls, tc)
		}
	}
	return calls
}

func emit(ctx context.Context, ch chan<- event.Event, evt event.Event) {
	if ch == nil {
		return
	}
	select {
	case ch <- evt:
	case <-ctx.Done():
	}
}

// --- Concurrent tool execution ---

type toolBatch struct {
	calls      []message.ToolCallBlock
	concurrent bool
}

type toolResult struct {
	index int
	state message.ToolResultState
	text  string
}

// batchToolCalls groups consecutive concurrency-safe tool calls into concurrent
// batches. Non-concurrent-safe calls form single-item sequential batches.
func batchToolCalls(calls []message.ToolCallBlock, tk *tool.Toolkit) []toolBatch {
	if len(calls) <= 1 {
		return []toolBatch{{calls: calls, concurrent: false}}
	}

	var batches []toolBatch
	var curBatch []message.ToolCallBlock
	curConcurrent := false

	for i, tc := range calls {
		safe := true
		if t := tk.Get(tc.Name); t != nil {
			safe = t.IsConcurrencySafe()
		}

		if i == 0 {
			curConcurrent = safe
			curBatch = []message.ToolCallBlock{tc}
			continue
		}

		if safe == curConcurrent && safe {
			curBatch = append(curBatch, tc)
		} else {
			batches = append(batches, toolBatch{calls: curBatch, concurrent: curConcurrent})
			curBatch = []message.ToolCallBlock{tc}
			curConcurrent = safe
		}
	}
	if len(curBatch) > 0 {
		batches = append(batches, toolBatch{calls: curBatch, concurrent: curConcurrent})
	}
	return batches
}

// executeAndRecord runs a single tool call, emits events, and merges the result into context.
func (a *UnifiedAgent) executeAndRecord(
	ctx context.Context,
	ch chan<- event.Event,
	replyID string,
	tc message.ToolCallBlock,
	actingHandler middleware.ActingHandler,
) {
	emit(ctx, ch, event.NewToolCallStartEvent(replyID, tc.ID, tc.Name))
	emit(ctx, ch, event.NewToolCallEndEvent(replyID, tc.ID))

	resultState, outputText := a.executeToolCallWithPermission(ctx, ch, replyID, tc, actingHandler)

	resultBlock := message.ToolResultBlock{
		Type:   "tool_result",
		ID:     tc.ID,
		Name:   tc.Name,
		Output: outputText,
		State:  resultState,
	}
	a.saveToContext([]message.ContentBlock{resultBlock}, nil)
	a.updateToolCallState(tc.ID, message.ToolCallFinished)
}

// executeConcurrentBatch runs all tool calls in the batch concurrently, then
// emits events and appends results in the original call order.
func (a *UnifiedAgent) executeConcurrentBatch(
	ctx context.Context,
	ch chan<- event.Event,
	replyID string,
	calls []message.ToolCallBlock,
	actingHandler middleware.ActingHandler,
) {
	results := make([]toolResult, len(calls))
	var wg sync.WaitGroup
	wg.Add(len(calls))

	for i, tc := range calls {
		go func(idx int, tc message.ToolCallBlock) {
			defer wg.Done()
			state, text := a.executeToolCallWithPermission(ctx, nil, replyID, tc, actingHandler)
			results[idx] = toolResult{index: idx, state: state, text: text}
		}(i, tc)
	}
	wg.Wait()

	// Emit events and merge results into context in original order
	for i, tc := range calls {
		emit(ctx, ch, event.NewToolCallStartEvent(replyID, tc.ID, tc.Name))
		emit(ctx, ch, event.NewToolCallEndEvent(replyID, tc.ID))

		r := results[i]
		emit(ctx, ch, event.NewToolResultStartEvent(replyID, tc.ID, tc.Name))
		if r.text != "" {
			emit(ctx, ch, event.NewToolResultTextDeltaEvent(replyID, tc.ID, r.text))
		}
		emit(ctx, ch, event.NewToolResultEndEvent(replyID, tc.ID, r.state))

		resultBlock := message.ToolResultBlock{
			Type:   "tool_result",
			ID:     tc.ID,
			Name:   tc.Name,
			Output: r.text,
			State:  r.state,
		}
		a.saveToContext([]message.ContentBlock{resultBlock}, nil)
		a.updateToolCallState(tc.ID, message.ToolCallFinished)
	}
}

// filterAudioBlocks removes audio DataBlocks from content to prevent
// audio data from bloating persisted context.
func filterAudioBlocks(content []message.ContentBlock) []message.ContentBlock {
	filtered := make([]message.ContentBlock, 0, len(content))
	for _, b := range content {
		if db, ok := b.(message.DataBlock); ok {
			mt := db.GetMediaType()
			if strings.HasPrefix(mt, "audio/") {
				continue
			}
		}
		filtered = append(filtered, b)
	}
	return filtered
}
