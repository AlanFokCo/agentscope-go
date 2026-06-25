package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/middleware"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

const defaultContextSize = 128000

var defaultSummarySchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"task_overview": {
			"type": "string",
			"description": "The user's core request and success criteria. Any clarifications or constraints they specified."
		},
		"current_state": {
			"type": "string",
			"description": "What has been completed so far. Files created, modified, or analyzed (with paths if relevant). Key outputs or artifacts produced."
		},
		"important_discoveries": {
			"type": "string",
			"description": "Technical constraints or requirements uncovered. Decisions made and their rationale. Errors encountered and how they were resolved. What approaches were tried that didn't work (and why)."
		},
		"next_steps": {
			"type": "string",
			"description": "Specific actions needed to complete the task. Any blockers or open questions to resolve. Priority order if multiple steps remain."
		},
		"context_to_preserve": {
			"type": "string",
			"description": "User preferences or style requirements. Domain-specific details that aren't obvious. Any promises made to the user."
		}
	},
	"required": ["task_overview", "current_state", "important_discoveries", "next_steps", "context_to_preserve"]
}`)

const defaultCompressionPrompt = "<system-hint>You have been working on the task described above " +
	"but have not yet completed it. " +
	"Now write a continuation summary that will allow you to resume " +
	"work efficiently in a future context window where the " +
	"conversation history will be replaced with this summary. " +
	"Your summary should be structured, concise, and actionable." +
	"</system-hint>"

const defaultSummaryTemplate = "<system-info>Here is a summary of your previous work\n" +
	"# Task Overview\n" +
	"{task_overview}\n\n" +
	"# Current State\n" +
	"{current_state}\n\n" +
	"# Important Discoveries\n" +
	"{important_discoveries}\n\n" +
	"# Next Steps\n" +
	"{next_steps}\n\n" +
	"# Context to Preserve\n" +
	"{context_to_preserve}" +
	"</system-info>"

// ContextConfig controls when and how the agent compresses its context window.
type ContextConfig struct {
	TriggerRatio      float64         // Token ratio to trigger compression (default 0.8, max 0.9).
	ReserveRatio      float64         // Ratio of tokens to keep as recent context (default 0.1).
	ContextSize       int             // Override context window size (0 = auto-detect from model).
	CompressionPrompt string          // Prompt guiding the model to generate a summary.
	SummaryTemplate   string          // Template with {field} placeholders for the structured summary.
	SummarySchema     json.RawMessage // JSON Schema for structured output (matches SummarySchema fields).
	ToolResultLimit   int             // Max token estimate for individual tool results (default 50000).
}

func (c *ContextConfig) withDefaults() ContextConfig {
	cfg := *c
	if cfg.TriggerRatio <= 0 || cfg.TriggerRatio >= 0.9 {
		cfg.TriggerRatio = 0.8
	}
	if cfg.ReserveRatio <= 0 || cfg.ReserveRatio >= 0.9 {
		cfg.ReserveRatio = 0.1
	}
	if cfg.CompressionPrompt == "" {
		cfg.CompressionPrompt = defaultCompressionPrompt
	}
	if cfg.SummaryTemplate == "" {
		cfg.SummaryTemplate = defaultSummaryTemplate
	}
	if cfg.SummarySchema == nil {
		cfg.SummarySchema = defaultSummarySchema
	}
	if cfg.ToolResultLimit <= 0 {
		cfg.ToolResultLimit = 50000
	}
	return cfg
}

// compressContext checks if the context exceeds the token threshold and, if so,
// generates a structured summary of old messages using the model.
// It runs through the OnCompressContext middleware chain if middlewares are configured.
func (a *UnifiedAgent) compressContext(ctx context.Context) error {
	if a.contextCfg == nil {
		return nil
	}

	if len(a.middlewares) == 0 {
		cfgCopy := *a.contextCfg
		return a.compressContextImpl(ctx, &cfgCopy)
	}

	core := func(ctx context.Context, input middleware.CompressInput) error {
		cfg := *a.contextCfg
		cfg.TriggerRatio = input.TriggerRatio
		cfg.ReserveRatio = input.ReserveRatio
		return a.compressContextImpl(ctx, &cfg)
	}
	chain := middleware.BuildCompressChain(a.middlewares, core)
	return chain(ctx, middleware.CompressInput{
		AgentName:    a.name,
		TriggerRatio: a.contextCfg.TriggerRatio,
		ReserveRatio: a.contextCfg.ReserveRatio,
	})
}

func (a *UnifiedAgent) compressContextImpl(ctx context.Context, cfg *ContextConfig) error {
	ctxSize := cfg.ContextSize
	if ctxSize == 0 {
		ctxSize = model.ResolveContextSize(a.model, defaultContextSize)
	}

	modelMsgs := a.prepareModelInput(ctx)
	toolSchemas := a.toolkit.GetToolSchemas()
	estimatedTokens := a.model.CountTokens(modelMsgs, toolSchemas)

	threshold := int(float64(ctxSize) * cfg.TriggerRatio)
	if estimatedTokens < threshold {
		return nil
	}

	a.mu.Lock()
	contextLen := len(a.state.Context)
	a.mu.Unlock()

	if contextLen == 0 {
		return fmt.Errorf("agent %s: system prompt (and summary) exceed compression threshold (%d tokens), cannot compress", a.name, threshold)
	}

	logrus.WithFields(logrus.Fields{
		"agent":     a.name,
		"tokens":    estimatedTokens,
		"threshold": threshold,
	}).Info("context compression triggered")

	reserveTokens := int(float64(ctxSize) * cfg.ReserveRatio)
	msgsToCompress, msgsToReserve := a.splitContextForCompression(reserveTokens, toolSchemas)

	if len(msgsToCompress) == 0 {
		logrus.WithField("agent", a.name).Warn("reserve ratio too large, falling back to reserve_ratio=0")
		msgsToCompress, msgsToReserve = a.splitContextForCompression(0, toolSchemas)
	}

	a.mu.Lock()
	systemPrompt := a.systemPrompt
	if len(a.middlewares) > 0 {
		systemPrompt = middleware.ApplySystemPromptPipeline(ctx, a.middlewares, a.name, systemPrompt)
	}
	summary := a.state.Summary
	a.mu.Unlock()

	compressionMsgs := buildCompressionMessages(systemPrompt, summary, msgsToCompress, cfg.CompressionPrompt)

	compressionToolSchema := []model.ToolSchema{
		{
			Type: "function",
			Function: model.ToolFunction{
				Name:        "generate_structured_output",
				Description: "Call this function to generate structured output required by the user.",
				Parameters:  cfg.SummarySchema,
			},
		},
	}
	compTokens := a.model.CountTokens(compressionMsgs, compressionToolSchema)
	contextOverflow := compTokens > ctxSize

	result, err := model.GenerateStructuredOutput(ctx, a.model, compressionMsgs, cfg.SummarySchema)
	if err != nil {
		if contextOverflow {
			logrus.WithField("agent", a.name).Warn("compression context overflow, removing oldest messages and retrying")
			result, err = a.retryCompressWithFewer(ctx, compressionMsgs, msgsToCompress, cfg, ctxSize, compressionToolSchema)
			if err != nil {
				return fmt.Errorf("agent %s: context compression failed after overflow retry: %w", a.name, err)
			}
		} else {
			return fmt.Errorf("agent %s: context compression failed: %w", a.name, err)
		}
	}

	newSummary, err := formatSummary(cfg.SummaryTemplate, result)
	if err != nil {
		return fmt.Errorf("agent %s: failed to format summary: %w", a.name, err)
	}

	// Offload the compressed context to workspace if offloader is set
	if a.offloader != nil {
		path, offErr := a.offloader.OffloadContent(ctx, newSummary, "compressed_context.txt")
		if offErr != nil {
			logrus.WithError(offErr).WithField("agent", a.name).Warn("failed to offload compressed context")
		} else {
			newSummary += fmt.Sprintf(
				"\n<system-reminder>The compressed context is offloaded to '%s'.</system-reminder>",
				path,
			)
		}
	}

	a.mu.Lock()
	a.state.Summary = newSummary
	a.state.Context = msgsToReserve
	a.mu.Unlock()

	if a.readCache != nil {
		cleanReadCacheForReserved(a.readCache, msgsToReserve)
	}

	logrus.WithField("agent", a.name).Info("context compression finished")
	return nil
}

func (a *UnifiedAgent) retryCompressWithFewer(
	ctx context.Context,
	baseMsgs []*message.Msg,
	msgsToCompress []*message.Msg,
	cfg *ContextConfig,
	ctxSize int,
	compressionToolSchema []model.ToolSchema,
) (json.RawMessage, error) {
	a.mu.Lock()
	systemPrompt := a.systemPrompt
	if len(a.middlewares) > 0 {
		systemPrompt = middleware.ApplySystemPromptPipeline(ctx, a.middlewares, a.name, systemPrompt)
	}
	summary := a.state.Summary
	a.mu.Unlock()

	triggerThreshold := int(float64(ctxSize) * cfg.TriggerRatio)

	for i := 1; i <= len(msgsToCompress); i++ {
		msgs := buildCompressionMessages(systemPrompt, summary, msgsToCompress[i:], cfg.CompressionPrompt)
		tokens := a.model.CountTokens(msgs, compressionToolSchema)
		if tokens < triggerThreshold {
			return model.GenerateStructuredOutput(ctx, a.model, msgs, cfg.SummarySchema)
		}
	}
	return nil, fmt.Errorf("cannot reduce context below threshold")
}

// splitContextForCompression splits state.Context into messages to compress and messages to reserve.
// It walks backward from the end, accumulating messages until the reserved portion
// reaches the token budget, keeping tool call/result pairs together.
func (a *UnifiedAgent) splitContextForCompression(reserveTokenBudget int, tools []model.ToolSchema) ([]*message.Msg, []*message.Msg) {
	a.mu.Lock()
	ctxMsgs := make([]*message.Msg, len(a.state.Context))
	copy(ctxMsgs, a.state.Context)
	systemPrompt := a.systemPrompt
	summary := a.state.Summary
	a.mu.Unlock()

	baseMsgs := []*message.Msg{message.SystemMsg(a.name, systemPrompt)}
	if summary != "" {
		baseMsgs = append(baseMsgs, message.UserMsg(a.name, "[Previous context summary]: "+summary))
	}

	if reserveTokenBudget <= 0 {
		return copyMsgs(ctxMsgs), nil
	}

	splitIdx := len(ctxMsgs)
	for i := len(ctxMsgs) - 1; i >= 0; i-- {
		candidate := make([]*message.Msg, len(baseMsgs))
		copy(candidate, baseMsgs)
		candidate = append(candidate, ctxMsgs[i:]...)
		tokens := a.model.CountTokens(candidate, tools)
		if tokens >= reserveTokenBudget {
			splitIdx = i + 1
			break
		}
		if i == 0 {
			return nil, copyMsgs(ctxMsgs)
		}
	}

	if splitIdx >= len(ctxMsgs) {
		splitIdx = adjustSplitForToolPairs(ctxMsgs, len(ctxMsgs)-1)
	} else {
		splitIdx = adjustSplitForToolPairs(ctxMsgs, splitIdx)
	}

	return copyMsgs(ctxMsgs[:splitIdx]), copyMsgs(ctxMsgs[splitIdx:])
}

// adjustSplitForToolPairs ensures tool result messages aren't separated from their
// corresponding tool call messages. It pushes the split point forward if needed
// so that orphan tool results (whose tool call is in the compressed portion)
// move into the compressed portion as well.
func adjustSplitForToolPairs(msgs []*message.Msg, splitIdx int) int {
	callIDs := make(map[string]bool)
	resultPositions := make(map[string]int)

	for i := splitIdx; i < len(msgs); i++ {
		for _, b := range msgs[i].GetContentBlocks(message.ContentBlockToolCall) {
			if tc, ok := b.(message.ToolCallBlock); ok {
				callIDs[tc.ID] = true
			}
		}
		for _, b := range msgs[i].GetContentBlocks(message.ContentBlockToolResult) {
			if tr, ok := b.(message.ToolResultBlock); ok {
				resultPositions[tr.ID] = i
			}
		}
	}

	maxOrphanIdx := -1
	for id, pos := range resultPositions {
		if !callIDs[id] && pos > maxOrphanIdx {
			maxOrphanIdx = pos
		}
	}

	if maxOrphanIdx < 0 {
		return splitIdx
	}
	return maxOrphanIdx + 1
}

// TruncateToolResult truncates a tool result string if it exceeds the token limit.
// Returns the (possibly truncated) text and whether truncation occurred.
func TruncateToolResult(text string, tokenLimit int) (string, bool) {
	estimatedTokens := len(text) / 4
	if estimatedTokens <= tokenLimit {
		return text, false
	}
	charLimit := tokenLimit * 4
	if charLimit >= len(text) {
		return text, false
	}
	return text[:charLimit] + "\n<<<TRUNCATED>>>", true
}

// SplitToolResultForCompression performs token-aware binary search to split
// a tool result into (reserved, offloaded) portions. It uses the model's
// CountTokens to find the largest prefix that fits under tokenLimit.
// Falls back to character-based truncation if no model is available.
func SplitToolResultForCompression(text string, tokenLimit int, counter TokenCounter) (reserved, offloaded string, wasSplit bool) {
	if counter == nil {
		r, split := TruncateToolResult(text, tokenLimit)
		if split {
			return r, text[len(r)-len("\n<<<TRUNCATED>>>"):], true
		}
		return text, "", false
	}

	probe := []*message.Msg{message.UserMsg("_", text)}
	tokens := counter.CountTokens(probe, nil)
	if tokens <= tokenLimit {
		return text, "", false
	}

	// Binary search for the split point
	lo, hi := 0, len(text)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		probe = []*message.Msg{message.UserMsg("_", text[:mid])}
		t := counter.CountTokens(probe, nil)
		if t <= tokenLimit {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	if lo == 0 {
		lo = 1
	}
	return text[:lo] + "\n<<<TRUNCATED>>>", text[lo:], true
}

// TokenCounter is the subset of model.ChatModel needed for token counting.
type TokenCounter interface {
	CountTokens(msgs []*message.Msg, tools []model.ToolSchema) int
}

// TruncateToolResultBlocks truncates individual blocks within a multi-block tool result.
// Text blocks are truncated by character limit. Data blocks with Base64Source
// have their data replaced with a placeholder.
func TruncateToolResultBlocks(blocks []message.ContentBlock, tokenLimit int) []message.ContentBlock {
	totalTokens := 0
	for _, b := range blocks {
		switch blk := b.(type) {
		case message.TextBlock:
			totalTokens += len(blk.Text) / 4
		case message.DataBlock:
			if src, ok := blk.Source.(message.Base64Source); ok {
				totalTokens += len(src.Data) * 3 / 16 // base64 → raw → tokens
			}
		}
	}

	if totalTokens <= tokenLimit {
		return blocks
	}

	result := make([]message.ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		switch blk := b.(type) {
		case message.TextBlock:
			charLimit := tokenLimit * 4
			if len(blk.Text) > charLimit {
				blk.Text = blk.Text[:charLimit] + "\n<<<TRUNCATED>>>"
			}
			result = append(result, blk)
		case message.DataBlock:
			// Replace large base64 data with a placeholder
			if src, ok := blk.Source.(message.Base64Source); ok && len(src.Data) > 1000 {
				blk.Source = message.Base64Source{
					Type:      "base64",
					Data:      "",
					MediaType: src.MediaType,
				}
				result = append(result, blk)
			} else {
				result = append(result, blk)
			}
		default:
			result = append(result, b)
		}
	}
	return result
}

// splitMessageAtBlock splits a message into two messages at the given block index.
// The first message contains blocks [0, blockIdx), the second [blockIdx, end).
func splitMessageAtBlock(msg *message.Msg, blockIdx int) (*message.Msg, *message.Msg) {
	if blockIdx <= 0 || blockIdx >= len(msg.Content) {
		return msg, nil
	}
	first := *msg
	first.Content = make([]message.ContentBlock, blockIdx)
	copy(first.Content, msg.Content[:blockIdx])

	second := *msg
	second.Content = make([]message.ContentBlock, len(msg.Content)-blockIdx)
	copy(second.Content, msg.Content[blockIdx:])

	return &first, &second
}

func buildCompressionMessages(systemPrompt, summary string, msgsToCompress []*message.Msg, compressionPrompt string) []*message.Msg {
	msgs := make([]*message.Msg, 0, len(msgsToCompress)+3)
	msgs = append(msgs, message.SystemMsg("system", systemPrompt))
	if summary != "" {
		msgs = append(msgs, message.UserMsg("user", summary))
	}
	msgs = append(msgs, msgsToCompress...)
	msgs = append(msgs, message.UserMsg("user", compressionPrompt))
	return msgs
}

func formatSummary(template string, structuredOutput json.RawMessage) (string, error) {
	var fields map[string]string
	if err := json.Unmarshal(structuredOutput, &fields); err != nil {
		return "", fmt.Errorf("unmarshal structured output: %w", err)
	}

	result := template
	for key, value := range fields {
		result = strings.ReplaceAll(result, "{"+key+"}", value)
	}
	return result, nil
}

func copyMsgs(msgs []*message.Msg) []*message.Msg {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]*message.Msg, len(msgs))
	copy(out, msgs)
	return out
}

// cleanReadCacheForReserved drops cached files not referenced by Read tool calls
// in the reserved messages.
func cleanReadCacheForReserved(rc *tool.ReadCache, reservedMsgs []*message.Msg) {
	keepPaths := make(map[string]bool)

	for _, m := range reservedMsgs {
		for _, b := range m.GetContentBlocks(message.ContentBlockToolCall) {
			tc, ok := b.(message.ToolCallBlock)
			if !ok || tc.Name != "Read" {
				continue
			}
			var args struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal([]byte(tc.Input), &args); err == nil && args.FilePath != "" {
				keepPaths[args.FilePath] = true
			}
		}
	}

	rc.CleanFileCache(keepPaths)
}
