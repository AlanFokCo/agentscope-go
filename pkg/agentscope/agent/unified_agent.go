package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/audit"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	agentscope "github.com/alanfokco/agentscope-go/v2/pkg/agentscope"
	agenterrors "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/errors"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/event"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/loop"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/middleware"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/permission"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/protocol"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/skill"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/types"
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

	confirmCh      chan event.UserConfirmResultEvent
	externalCh     chan event.ExternalExecutionResultEvent
	mu             sync.Mutex
	offloader      Offloader
	stateSaver     StateSaver
	confirmStash   map[string]event.ConfirmResult
	externalStash  map[string]message.ToolResultBlock
	hookRunner     *loop.HookRunner
	stateAwareness bool
	responseFormat *model.ResponseFormat
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
// StateSchemaVersion is bumped when AgentState's serialized shape changes
// incompatibly; loaders dispatch on it (HARNESS_DESIGN F1).
const StateSchemaVersion = 1

type AgentState struct {
	SchemaVersion int `json:"schema_version,omitempty"`

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
// WithStateSaver attaches a StateSaver used for automatic checkpointing
// (HARNESS_DESIGN F1). Checkpoints are written after every tool batch and
// when the reply parks awaiting user/external input, so a crashed process
// can resume mid-conversation via LoadCheckpoint + WithState.
//
// Crash semantics: checkpoints sit at batch boundaries. A crash mid-batch
// resumes by RE-EXECUTING THE WHOLE BATCH — side effects of batch tools are
// not exactly-once; prefer read-only/idempotent tools in crash-sensitive
// deployments.
func WithStateSaver(s StateSaver) AgentOption {
	return func(a *UnifiedAgent) { a.stateSaver = s }
}

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
func WithContextConfig(cfg *ContextConfig) AgentOption {
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

// WithLoopHooks registers loop.Hook instances that receive lifecycle
// notifications (model calls, tool executions, state transitions) from
// within the agent's replyLoop. This enables MetricsHook and TracingHook
// integration without changing the agent's internal control flow.
func WithLoopHooks(hooks ...loop.Hook) AgentOption {
	return func(a *UnifiedAgent) { a.hookRunner = loop.NewHookRunner(hooks...) }
}

// WithStateAwareness enables runtime state awareness injection into the system
// prompt. When enabled, the agent's current state (tasks, tool groups,
// permission mode) is appended to the system prompt before each model call.
func WithStateAwareness(enabled bool) AgentOption {
	return func(a *UnifiedAgent) { a.stateAwareness = enabled }
}

// WithResponseFormat sets a structured output response format for the agent.
// When configured, the agent will request the model to return output conforming
// to the specified format (e.g. json_schema) on every model call.
func WithResponseFormat(rf *model.ResponseFormat) AgentOption {
	return func(a *UnifiedAgent) { a.responseFormat = rf }
}

// NewUnifiedAgent creates the v2 unified agent.
//
// It panics if name is empty or model is nil: both are programmer errors that
// would otherwise surface as an obscure nil-dereference deep inside the reply
// loop. (This mirrors message.NewMsg, which also panics on invalid construction.)
func NewUnifiedAgent(name, systemPrompt string, m model.ChatModel, opts ...AgentOption) *UnifiedAgent {
	if name == "" {
		panic("agent: NewUnifiedAgent requires a non-empty name")
	}
	if m == nil {
		panic("agent: NewUnifiedAgent requires a non-nil model")
	}
	a := &UnifiedAgent{
		name:         name,
		systemPrompt: systemPrompt,
		model:        m,
		toolkit:      tool.NewToolkit(),
		reactCfg:     ReactConfig{MaxIters: defaultUnifiedMaxIters},
		state:        &AgentState{SessionID: agentscope.GenerateID()},
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

	// Wrap replyLoop through the OnReply middleware chain (an empty chain
	// is the identity), then observe the chain's output from this
	// agent-owned forwarder: it records whether each ReplyEndEvent escapes
	// the chain (swallow detection, Python #2322) and strips the internal
	// round-boundary sentinel.
	verdict := make(chan bool, 8)
	core := func(ctx context.Context, ri middleware.ReplyInput) <-chan event.Event {
		ch := make(chan event.Event, 32)
		go a.replyLoop(ctx, ri.UserInput, ch, verdict)
		return ch
	}
	chain := middleware.BuildReplyChain(a.middlewares, core)
	chainOut := chain(ctx, middleware.ReplyInput{
		AgentName: a.name,
		UserInput: input,
	})

	out := make(chan event.Event, 32)
	go func() {
		defer close(out)
		var curReplyID string
		sawEnd := false
		for evt := range chainOut {
			switch e := evt.(type) {
			case event.ReplyStartEvent:
				curReplyID = e.ReplyID
				sawEnd = false
			case event.ReplyEndEvent:
				if e.ReplyID == curReplyID {
					sawEnd = true
				}
			case event.CustomEvent:
				if e.Name == roundBoundSentinel {
					select {
					case verdict <- sawEnd:
					default:
					}
					sawEnd = false
					continue
				}
			}
			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
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
func (a *UnifiedAgent) SubmitUserConfirm(result *event.UserConfirmResultEvent) {
	if a.confirmCh != nil {
		a.confirmCh <- *result
	}
}

// SubmitExternalResult submits results from external tool execution.
// Call this after receiving a RequireExternalExecutionEvent from the event
// stream. Results whose IDs match no pending call are parked for other
// waiters (batched results); the wait ends only on a matching result or
// context cancellation.
func (a *UnifiedAgent) SubmitExternalResult(result *event.ExternalExecutionResultEvent) {
	if a.externalCh != nil {
		a.externalCh <- *result
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

// roundBoundSentinel names the internal CustomEvent emitted after each
// ReplyEndEvent: the agent-owned forwarder outside the middleware chain
// uses it to report whether the end event escaped the chain (was not
// swallowed). Middleware MUST forward CustomEvent values whose name starts
// with "agentscope." — dropping them stalls swallow detection until the
// reply context ends.
const roundBoundSentinel = "agentscope.round_bound"

func (a *UnifiedAgent) replyLoop(ctx context.Context, input string, ch chan<- event.Event, verdict <-chan bool) {
	defer close(ch)

	hooks := a.hookRunner
	if hooks == nil {
		hooks = loop.NewHookRunner()
	}

	replyID := agentscope.GenerateID()
	a.clearConfirmStash()
	a.clearExternalStash()

	a.mu.Lock()
	a.state.ReplyID = replyID
	a.state.CurIter = 0
	a.mu.Unlock()

	// HARNESS_DESIGN A2: make the reply ID visible to middleware hooks
	// (recorders, etc.) via the MiddleContext and to audit loggers via ctx.
	if mc := middleware.GetMiddleContext(ctx); mc != nil {
		mc.Set("agent", "reply_id", replyID)
	}
	ctx = audit.WithReplyID(ctx, replyID)

	hooks.OnLoopStart()
	defer hooks.OnLoopEnd(nil)

	emit(ctx, ch, event.NewReplyStartEvent(a.state.SessionID, replyID, a.name, message.RoleAssistant))

	userMsg := message.UserMsg(a.name, input)
	a.mu.Lock()
	a.state.Context = append(a.state.Context, userMsg)
	a.mu.Unlock()

	modelCallHandler := a.buildModelCallHandler()
	actingHandler := a.buildActingHandler()

	// Swallow loop (Python #2322): an OnReply middleware may swallow the
	// ReplyEndEvent (receive it without forwarding it) to force another
	// reasoning-acting round. Detection: after emitting the end event the
	// loop emits an internal sentinel; the agent-owned forwarder outside
	// the chain reports on `verdict` whether the end event escaped. A new
	// round restarts the iteration counter, which also unblocks a swallowed
	// exceed-max-iters end. Interrupted ends cannot be swallowed.
	madeProgress := true
	for {
		reason, progress, errInfo := a.reactRound(ctx, ch, replyID, hooks, modelCallHandler, actingHandler)
		if progress {
			madeProgress = true
		}

		if reason == types.ReplyInterrupted {
			// Cannot be swallowed to continue the reply (Python parity).
			emit(ctx, ch, event.NewReplyEndEventWithReason(a.state.SessionID, replyID, reason))
			return
		}

		if errInfo != nil {
			emit(ctx, ch, event.NewReplyEndEventWithError(a.state.SessionID, replyID, errInfo.Type, errInfo.Message))
		} else {
			emit(ctx, ch, event.NewReplyEndEventWithReason(a.state.SessionID, replyID, reason))
		}

		emit(ctx, ch, event.NewCustomEvent(replyID, roundBoundSentinel, nil))
		select {
		case delivered := <-verdict:
			if delivered {
				return
			}
			// Swallowed: force another round — unless the middleware keeps
			// swallowing without any reasoning/acting in between (busy loop).
			if !madeProgress {
				logrus.WithField("agent", a.name).Error(
					"a middleware swallowed the ReplyEndEvent twice without any reasoning/acting in between; ending the reply")
				emit(ctx, ch, event.NewCustomEvent(replyID, "error.swallow_loop", map[string]any{
					"error": "a middleware swallowed the ReplyEndEvent twice without progress",
				}))
				// Keep the reply start/end pairing intact.
				emit(ctx, ch, event.NewReplyEndEventWithError(a.state.SessionID, replyID,
					types.ErrorInternal, "ReplyEndEvent swallowed repeatedly without progress"))
				return
			}
			madeProgress = false
			continue
		case <-ctx.Done():
			return
		}
	}
}

// reactRound runs one reasoning-acting cycle until an exit condition:
// final text (completed), iteration exhaustion (exceed_max_iters), model
// failure (error), context cancellation (interrupted), or a parked wait
// state. It emits every lifecycle event except ReplyEndEvent, which the
// swallow loop in replyLoop owns.
func (a *UnifiedAgent) reactRound(
	ctx context.Context,
	ch chan<- event.Event,
	replyID string,
	hooks *loop.HookRunner,
	modelCallHandler middleware.ModelCallHandler,
	actingHandler middleware.ActingHandler,
) (reason types.ReplyFinishedReason, madeProgress bool, errInfo *types.ReplyErrorInfo) {
	curState := protocol.StateReason
	finishedNormally := false

reactLoop:
	for iter := 0; iter < a.reactCfg.MaxIters; iter++ {
		if ctx.Err() != nil {
			return types.ReplyInterrupted, madeProgress, nil
		}
		a.mu.Lock()
		a.state.CurIter = iter
		a.mu.Unlock()

		// State machine: decide next action based on tool call states
		nextState, exitMsg := a.checkNextAction()

		if nextState != curState {
			hooks.OnStateTransition(curState, nextState, iter)
			curState = nextState
		}

		switch nextState {
		case protocol.StateWait:
			// Crash-recovery re-prompt (HARNESS_DESIGN F1): after a resume
			// via WithState, asking/submitted calls have no blocked waiter —
			// re-drive their handshakes. When nothing is awaiting (e.g. an
			// external system parked us), fall through to the wait exit.
			a.Checkpoint(ctx)
			if a.repromptAwaitingCalls(ctx, ch, replyID, actingHandler) {
				// Checkpoint again after the inline execution of resumed
				// calls: the pre-reprompt snapshot still holds the
				// ASKING/SUBMITTED call, and a crash before the next
				// batch-boundary checkpoint would re-execute an
				// already-executed tool on resume (HARNESS review M-2).
				a.Checkpoint(ctx)
				madeProgress = true
				continue
			}
			// Waiting for external events — emit to consumer, don't save to context
			if exitMsg != nil {
				emitContentEvents(ctx, ch, replyID, exitMsg.Content)
			}
			return types.ReplyCompleted, madeProgress, nil

		case protocol.StateAct:
			// Execute pending tool calls from prior iteration
			toolCalls := a.getExecutableToolCalls()
			batches := batchToolCalls(toolCalls, a.toolkit, a.wouldRequireHITL)
			for _, batch := range batches {
				if batch.concurrent && len(batch.calls) > 1 {
					for _, tc := range batch.calls {
						hooks.BeforeToolExec(curState, iter, tc.Name)
					}
					a.executeConcurrentBatch(ctx, ch, replyID, batch.calls, actingHandler)
					for _, tc := range batch.calls {
						hooks.AfterToolExec(curState, iter, tc.Name, nil)
					}
				} else {
					for _, tc := range batch.calls {
						hooks.BeforeToolExec(curState, iter, tc.Name)
						a.executeAndRecord(ctx, ch, replyID, &tc, actingHandler)
						hooks.AfterToolExec(curState, iter, tc.Name, nil)
					}
				}
			}
			madeProgress = true
			// Checkpoint at the batch boundary (HARNESS_DESIGN F1): a crash
			// after this point resumes from the recorded results, never
			// mid-batch.
			a.Checkpoint(ctx)
			// Transition back to Reason after acting
			hooks.OnStateTransition(curState, protocol.StateReason, iter)
			curState = protocol.StateReason
			continue

		case protocol.StateReason:
			// Compress context before model call. Detect whether compression
			// actually happened by observing state.Summary change, and emit a
			// "compacted" CustomEvent so consumers can surface it (e.g. lathe's
			// TUI / stream-json). This avoids changing compressContext's return
			// signature or the middleware CompressHandler API.
			a.mu.Lock()
			prevSummary := a.state.Summary
			a.mu.Unlock()
			if err := a.compressContext(ctx); err != nil {
				logrus.WithError(err).WithField("agent", a.name).Warn("context compression failed")
			}
			a.mu.Lock()
			compacted := a.state.Summary != prevSummary && a.state.Summary != ""
			a.mu.Unlock()
			if compacted {
				emit(ctx, ch, event.NewCustomEvent(replyID, "compacted", nil))
			}

			modelMsgs := a.prepareModelInput(ctx)
			schemas := a.toolkit.GetToolSchemas()

			hooks.BeforeModelCall(curState, iter)
			emit(ctx, ch, event.NewModelCallStartEvent(replyID, ""))

			resp, err := modelCallHandler(ctx, &middleware.ModelCallInput{
				AgentName: a.name,
				Messages:  modelMsgs,
				Tools:     schemas,
			})
			if err != nil {
				hooks.AfterModelCall(curState, iter, err)
				logrus.WithError(err).Error("agent: model call failed")
				emit(ctx, ch, event.NewModelCallEndEvent(replyID, 0, 0))
				if ctx.Err() != nil {
					return types.ReplyInterrupted, madeProgress, nil
				}
				info := classifyReplyError(err)
				return types.ReplyError, madeProgress, &info
			}

			hooks.AfterModelCall(curState, iter, nil)
			madeProgress = true

			var inputTok, outputTok, cacheCreate, cacheRead int
			if resp.Usage != nil {
				inputTok = resp.Usage.InputTokens
				outputTok = resp.Usage.OutputTokens
				cacheCreate = resp.Usage.CacheCreationInputTokens
				cacheRead = resp.Usage.CacheInputTokens
			}
			emit(ctx, ch, event.NewModelCallEndEventWithCache(replyID, inputTok, outputTok, cacheCreate, cacheRead))

			// Transition to Inspect after model call
			hooks.OnStateTransition(curState, protocol.StateInspect, iter)
			curState = protocol.StateInspect

			// Save response to context (merges into last assistant msg)
			a.saveToContext(resp.Content, resp.Usage)

			// Emit streaming events for consumers
			emitContentEvents(ctx, ch, replyID, resp.Content)

			// Inspect: check for tool calls
			toolCalls := extractToolCalls(resp.Content)
			if len(toolCalls) == 0 {
				// No tool calls — transition to Exit
				hooks.OnStateTransition(curState, protocol.StateExit, iter)
				finishedNormally = true
				break reactLoop
			}

			// Tool calls found — transition to Act
			hooks.OnStateTransition(curState, protocol.StateAct, iter)
			curState = protocol.StateAct

			// Execute tool calls
			batches := batchToolCalls(toolCalls, a.toolkit, a.wouldRequireHITL)
			for _, batch := range batches {
				if batch.concurrent && len(batch.calls) > 1 {
					for _, tc := range batch.calls {
						hooks.BeforeToolExec(curState, iter, tc.Name)
					}
					a.executeConcurrentBatch(ctx, ch, replyID, batch.calls, actingHandler)
					for _, tc := range batch.calls {
						hooks.AfterToolExec(curState, iter, tc.Name, nil)
					}
				} else {
					for _, tc := range batch.calls {
						hooks.BeforeToolExec(curState, iter, tc.Name)
						a.executeAndRecord(ctx, ch, replyID, &tc, actingHandler)
						hooks.AfterToolExec(curState, iter, tc.Name, nil)
					}
				}
			}

			// Checkpoint at the batch boundary (HARNESS_DESIGN F1).
			a.Checkpoint(ctx)
			// After acting, transition back to Reason
			hooks.OnStateTransition(curState, protocol.StateReason, iter)
			curState = protocol.StateReason
		}

		if iter == a.reactCfg.MaxIters-1 {
			emit(ctx, ch, event.NewExceedMaxItersEvent(replyID, a.name))
		}
	}

	if !finishedNormally {
		// Iteration exhaustion (Python parity: EXCEED_MAX_ITERS). The end
		// is swallowable like any non-interrupted end; the next round
		// restarts the iteration counter.
		return types.ReplyExceedMaxIters, madeProgress, nil
	}
	return types.ReplyCompleted, madeProgress, nil
}

// classifyReplyError maps a model-call failure onto the structured
// reply-error taxonomy (parity with Python's ErrorType).
func classifyReplyError(err error) types.ReplyErrorInfo {
	t := types.ErrorUnknown
	switch {
	case errors.Is(err, agenterrors.ErrModelRateLimited):
		t = types.ErrorRateLimit
	case errors.Is(err, agenterrors.ErrModelTimeout):
		t = types.ErrorConnection
	case errors.Is(err, agenterrors.ErrModelContextLimit):
		t = types.ErrorInvalidRequest
	}
	var ae *agenterrors.AgentError
	if errors.As(err, &ae) && t == types.ErrorUnknown {
		if ae.Retryable {
			t = types.ErrorUpstream
		}
	}
	return types.ReplyErrorInfo{Type: t, Message: agenterrors.GetAgentMessage(err)}
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
	core := func(ctx context.Context, input *middleware.ModelCallInput) (*model.ChatResponse, error) {
		var opts []model.CallOption
		if len(input.Tools) > 0 {
			opts = append(opts, model.WithTools(input.Tools))
		}
		if input.ToolChoice != nil {
			opts = append(opts, model.WithToolChoice(input.ToolChoice))
		}
		opts = ApplyResponseFormat(opts, a.responseFormat)
		return a.callModel(ctx, input.Messages, opts)
	}
	if len(a.middlewares) == 0 {
		return core
	}
	return middleware.BuildModelCallChain(a.middlewares, core)
}

// buildActingHandler returns an ActingHandler wrapped with middleware.
func (a *UnifiedAgent) buildActingHandler() middleware.ActingHandler {
	core := func(ctx context.Context, input *middleware.ActingInput) (*tool.ToolResponse, error) {
		return a.toolkit.CallToolFromBlock(ctx, &input.ToolCall)
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
	tc *message.ToolCallBlock,
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
			emit(ctx, ch, event.NewRequireUserConfirmEvent(replyID, []message.ToolCallBlock{*tc}))

			// Block waiting for user confirmation
			confirmed, resultTC := a.waitForConfirmation(ctx, tc.ID)
			if !confirmed {
				a.updateToolCallState(tc.ID, message.ToolCallFinished)
				return a.emitToolResult(ctx, ch, replyID, tc, message.ToolResultDenied,
					"Permission denied by user")
			}
			a.updateToolCallState(tc.ID, message.ToolCallAllowed)
			*tc = resultTC
		}
	}

	// Check if this is an external tool — pause and wait for external result.
	t := a.toolkit.Get(tc.Name)
	if t != nil && t.IsExternalTool() {
		a.updateToolCallState(tc.ID, message.ToolCallSubmitted)
		emit(ctx, ch, event.NewToolResultStartEvent(replyID, tc.ID, tc.Name))
		emit(ctx, ch, event.NewRequireExternalExecutionEvent(replyID, []message.ToolCallBlock{*tc}))
		result := a.waitForExternalResult(ctx, tc.ID)
		if result == nil {
			// ToolResultStart was already emitted when the call was
			// submitted; emit only the delta/end so consumers never see a
			// duplicate start for the same call (upstream #2167).
			const reason = "External execution timed out, canceled, or no matching result was submitted"
			emit(ctx, ch, event.NewToolResultTextDeltaEvent(replyID, tc.ID, reason))
			emit(ctx, ch, event.NewToolResultEndEvent(replyID, tc.ID, message.ToolResultError))
			return message.ToolResultError, reason
		}
		outputText := result.GetOutputText()
		emit(ctx, ch, event.NewToolResultTextDeltaEvent(replyID, tc.ID, outputText))
		emit(ctx, ch, event.NewToolResultEndEvent(replyID, tc.ID, result.State))
		return result.State, outputText
	}

	return a.executeTool(ctx, ch, replyID, tc, actingHandler)
}

func (a *UnifiedAgent) waitForConfirmation(ctx context.Context, toolCallID string) (bool, message.ToolCallBlock) {
	// A batch confirmation consumed by an earlier waiter may already carry our
	// call (HARNESS review M2): check the stash first.
	if confirmed, tc, found := a.takeStashedConfirm(toolCallID); found {
		return confirmed, tc
	}
	for {
		select {
		case result := <-a.confirmCh:
			var match *event.ConfirmResult
			for i := range result.ConfirmResults {
				cr := &result.ConfirmResults[i]
				if cr.ToolCall.ID == toolCallID {
					match = cr
					continue
				}
				// Not ours: keep it for the other waiter(s) instead of
				// dropping it on the floor (also stash entries AFTER our
				// match — a batch answer must not lose later results).
				a.stashConfirm(cr)
			}
			if match != nil {
				if match.Confirmed {
					// Add any user-provided rules to the engine
					for _, r := range match.Rules {
						if rule, ok := r.(permission.Rule); ok {
							a.engine.AddRule(rule)
						}
					}
					return true, match.ToolCall
				}
				return false, match.ToolCall
			}
			// Event answered no known call; keep waiting.
		case <-ctx.Done():
			return false, message.ToolCallBlock{}
		}
	}
}

// stashLimit bounds the parked-event stashes: a stream of malformed
// SubmitUserConfirm / SubmitExternalResult events with fabricated IDs must
// not grow a stash unbounded within one reply (HARNESS review L-4).
// Entries over the limit are dropped with a warning; stashes hold batched
// answers for concurrent waiters of ONE reply, so the bound is generous.
const stashLimit = 1024

// stashConfirm parks a confirmation meant for another call so its waiter can
// pick it up (batch ConfirmResults support).
func (a *UnifiedAgent) stashConfirm(cr *event.ConfirmResult) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.confirmStash == nil {
		a.confirmStash = map[string]event.ConfirmResult{}
	}
	if _, exists := a.confirmStash[cr.ToolCall.ID]; !exists && len(a.confirmStash) >= stashLimit {
		logrus.WithField("tool_call_id", cr.ToolCall.ID).Warn("agent: confirmation stash limit reached, dropping entry")
		return
	}
	a.confirmStash[cr.ToolCall.ID] = *cr
}

// takeStashedConfirm retrieves and removes a parked confirmation.
func (a *UnifiedAgent) takeStashedConfirm(toolCallID string) (bool, message.ToolCallBlock, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cr, ok := a.confirmStash[toolCallID]
	if !ok {
		return false, message.ToolCallBlock{}, false
	}
	delete(a.confirmStash, toolCallID)
	return cr.Confirmed, cr.ToolCall, true
}

// clearConfirmStash drops leftover parked confirmations; called at the start
// of every reply so stale events cannot leak across replies.
func (a *UnifiedAgent) clearConfirmStash() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.confirmStash = nil
}

func (a *UnifiedAgent) waitForExternalResult(ctx context.Context, toolCallID string) *message.ToolResultBlock {
	if a.externalCh == nil {
		return nil
	}
	// A batched result consumed by an earlier waiter may already carry our
	// call (HARNESS review M-1): check the stash first.
	if tr, found := a.takeStashedExternalResult(toolCallID); found {
		return &tr
	}
	for {
		select {
		case result := <-a.externalCh:
			var match *message.ToolResultBlock
			for i := range result.ExecutionResults {
				tr := &result.ExecutionResults[i]
				if tr.ID == toolCallID {
					match = tr
					continue
				}
				// Not ours: keep it for the other waiter(s) instead of
				// dropping it on the floor (same mechanism as the
				// confirmation stash).
				a.stashExternalResult(tr)
			}
			if match != nil {
				return match
			}
			// Event answered no known call; keep waiting.
		case <-ctx.Done():
			return nil
		}
	}
}

// stashExternalResult parks an external result meant for another call so its
// waiter can pick it up (batch ExecutionResults support, HARNESS review M-1).
func (a *UnifiedAgent) stashExternalResult(tr *message.ToolResultBlock) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.externalStash == nil {
		a.externalStash = map[string]message.ToolResultBlock{}
	}
	if _, exists := a.externalStash[tr.ID]; !exists && len(a.externalStash) >= stashLimit {
		logrus.WithField("tool_call_id", tr.ID).Warn("agent: external result stash limit reached, dropping entry")
		return
	}
	a.externalStash[tr.ID] = *tr
}

// takeStashedExternalResult retrieves and removes a parked external result.
func (a *UnifiedAgent) takeStashedExternalResult(toolCallID string) (message.ToolResultBlock, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	tr, ok := a.externalStash[toolCallID]
	if !ok {
		return message.ToolResultBlock{}, false
	}
	delete(a.externalStash, toolCallID)
	return tr, true
}

// clearExternalStash drops leftover parked external results; called at the
// start of every reply so stale events cannot leak across replies.
func (a *UnifiedAgent) clearExternalStash() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.externalStash = nil
}

func (a *UnifiedAgent) executeTool(
	ctx context.Context,
	ch chan<- event.Event,
	replyID string,
	tc *message.ToolCallBlock,
	actingHandler middleware.ActingHandler,
) (message.ToolResultState, string) {
	t := a.toolkit.Get(tc.Name)
	if st, ok := t.(tool.StreamingTool); ok {
		var resultState message.ToolResultState
		var outputText string
		core := func(coreCtx context.Context, _ *middleware.ActingInput) (*tool.ToolResponse, error) {
			resultState, outputText = a.executeStreamingTool(coreCtx, ch, replyID, tc, st)
			return &tool.ToolResponse{
				Content: []message.ContentBlock{message.TextBlock{Type: "text", Text: outputText}},
				State:   resultState,
			}, nil
		}
		handler := core
		if len(a.middlewares) > 0 {
			handler = middleware.BuildActingChain(a.middlewares, core)
		}
		_, _ = handler(ctx, &middleware.ActingInput{AgentName: a.name, ToolCall: *tc})
		return resultState, outputText
	}

	toolResp, execErr := actingHandler(ctx, &middleware.ActingInput{
		AgentName: a.name,
		ToolCall:  *tc,
	})

	var resultState message.ToolResultState
	var outputText string
	switch {
	case execErr != nil:
		if _, ok := execErr.(agenterrors.DeveloperError); ok {
			logrus.WithError(execErr).Error("agent: developer error in tool execution")
		}
		resultState = message.ToolResultError
		outputText = agenterrors.GetAgentMessage(execErr)
	case toolResp != nil:
		resultState = toolResp.State
		for _, b := range toolResp.Content {
			if tb, ok := b.(message.TextBlock); ok {
				outputText += tb.Text
			}
		}
	default:
		resultState = message.ToolResultSuccess
	}

	outputText = a.truncateToolResult(ctx, outputText, tc.ID)
	var toolMeta map[string]any
	if toolResp != nil {
		toolMeta = toolResp.Metadata
	}
	return a.emitToolResult(ctx, ch, replyID, tc, resultState, outputText, toolMeta)
}

// executeStreamingTool runs a StreamingTool and emits incremental events.
func (a *UnifiedAgent) executeStreamingTool(
	ctx context.Context,
	ch chan<- event.Event,
	replyID string,
	tc *message.ToolCallBlock,
	st tool.StreamingTool,
) (message.ToolResultState, string) {
	input, parseErr := tc.ParseInput()
	if parseErr != nil {
		return a.emitToolResult(ctx, ch, replyID, tc, message.ToolResultError,
			fmt.Sprintf("parse tool input: %v", parseErr))
	}

	streamCh, startErr := st.ExecuteStream(ctx, input)
	if startErr != nil {
		return a.emitToolResult(ctx, ch, replyID, tc, message.ToolResultError,
			fmt.Sprintf("start streaming tool: %v", startErr))
	}

	emit(ctx, ch, event.NewToolResultStartEvent(replyID, tc.ID, tc.Name))

	var finalState message.ToolResultState
	var finalText string

	for chunk := range streamCh {
		if chunk.IsFinal {
			finalState = chunk.State
			for _, b := range chunk.Content {
				if tb, ok := b.(message.TextBlock); ok {
					finalText += tb.Text
				}
			}
		} else {
			for _, b := range chunk.Content {
				if tb, ok := b.(message.TextBlock); ok {
					emit(ctx, ch, event.NewToolResultTextDeltaEvent(replyID, tc.ID, tb.Text))
				}
			}
		}
	}

	if finalState == "" {
		finalState = message.ToolResultSuccess
	}

	finalText = a.truncateToolResult(ctx, finalText, tc.ID)
	emit(ctx, ch, event.NewToolResultEndEvent(replyID, tc.ID, finalState))
	return finalState, finalText
}

func (a *UnifiedAgent) truncateToolResult(ctx context.Context, outputText, toolCallID string) string {
	if a.contextCfg != nil && a.contextCfg.ToolResultLimit > 0 {
		truncated, wasTruncated := TruncateToolResult(outputText, a.contextCfg.ToolResultLimit)
		if wasTruncated && a.offloader != nil {
			path, offErr := a.offloader.OffloadContent(ctx, outputText,
				fmt.Sprintf("tool_result_%s.txt", toolCallID))
			if offErr == nil {
				truncated += fmt.Sprintf(
					"\n<system-reminder>The remaining content has been offloaded to '%s'.</system-reminder>",
					path,
				)
			}
		}
		return truncated
	}
	return outputText
}

func (a *UnifiedAgent) emitToolResult(
	ctx context.Context,
	ch chan<- event.Event,
	replyID string,
	tc *message.ToolCallBlock,
	state message.ToolResultState,
	text string,
	metadata ...map[string]any,
) (message.ToolResultState, string) {
	emit(ctx, ch, event.NewToolResultStartEvent(replyID, tc.ID, tc.Name))
	if text != "" {
		emit(ctx, ch, event.NewToolResultTextDeltaEvent(replyID, tc.ID, text))
	}
	end := event.NewToolResultEndEvent(replyID, tc.ID, state)
	if len(metadata) > 0 && len(metadata[0]) > 0 {
		end.Metadata = metadata[0] // e.g. Edit/Write "diff" (M6b)
	}
	emit(ctx, ch, end)
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

	// Inject runtime state awareness if enabled
	if a.stateAwareness {
		prompt = InjectStateAwareness(prompt, a.state)
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

// wouldRequireHITL reports whether executing tc would block for human-in-the-loop
// confirmation or external execution — in which case it must not run in a
// concurrent batch (see batchToolCalls). It mirrors the permission decision made
// in executeToolCallWithPermission.
func (a *UnifiedAgent) wouldRequireHITL(tc *message.ToolCallBlock) bool {
	t := a.toolkit.Get(tc.Name)
	if t == nil {
		return false
	}
	if t.IsExternalTool() {
		return true
	}
	if a.engine == nil || tc.State == message.ToolCallAllowed {
		return false
	}
	input, err := tc.ParseInput()
	if err != nil {
		return true // will be handled (error/ask) on the sequential path
	}
	decision, err := a.engine.CheckPermission(t, input)
	if err != nil {
		return true
	}
	// Ask/Passthrough both block waiting for a user confirmation.
	return decision.Behavior == permission.BehaviorAsk || decision.Behavior == permission.BehaviorPassthrough
}

// batchToolCalls groups consecutive concurrency-safe tool calls into concurrent
// batches. Non-concurrent-safe calls form single-item sequential batches. A tool
// that would block on human-in-the-loop confirmation (or external execution) is
// also kept sequential: concurrent goroutines cannot each wait on the agent's
// single confirm/external channel without losing or crossing confirmations, so
// such tools must run one at a time on the (correct) sequential path.
func batchToolCalls(calls []message.ToolCallBlock, tk *tool.Toolkit, blocksHITL func(*message.ToolCallBlock) bool) []toolBatch {
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
		if safe && blocksHITL != nil && blocksHITL(&calls[i]) {
			safe = false // needs HITL/external → force sequential
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
	tc *message.ToolCallBlock,
	actingHandler middleware.ActingHandler,
) {
	start := event.NewToolCallStartEvent(replyID, tc.ID, tc.Name)
	start.ToolCallInput = tc.Input
	emit(ctx, ch, start)
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
		go func(idx int, tc *message.ToolCallBlock) {
			defer wg.Done()
			state, text := a.executeToolCallWithPermission(ctx, nil, replyID, tc, actingHandler)
			results[idx] = toolResult{index: idx, state: state, text: text}
		}(i, &tc)
	}
	wg.Wait()

	// Emit events and merge results into context in original order
	for i, tc := range calls {
		start := event.NewToolCallStartEvent(replyID, tc.ID, tc.Name)
		start.ToolCallInput = tc.Input
		emit(ctx, ch, start)
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
