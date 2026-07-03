// Package errors provides a typed error hierarchy for agentscope-go.
//
// AgentError supports category classification, error codes, retryable marking,
// and integrates with Go's errors.Is / errors.As via the Unwrap method.
//
// This package coexists with the legacy exception package. New code should
// use errors; old code continues to use exception until Phase 4 migration.
package errors

import (
	"errors"
	"fmt"
	"time"
)

// Category classifies the origin of an error.
type Category int

const (
	CategoryModel      Category = iota // LLM API errors
	CategoryTool                       // Tool execution errors
	CategoryPermission                 // Permission/approval denied
	CategoryContext                    // Context window errors
	CategoryConfig                     // Configuration errors
	CategoryPlatform                   // OS/platform compatibility errors
	CategoryNetwork                    // Network/connectivity errors
	CategoryResource                   // Resource/budget errors
)

var categoryNames = [...]string{
	"model",
	"tool",
	"permission",
	"context",
	"config",
	"platform",
	"network",
	"resource",
}

func (c Category) String() string {
	if int(c) < len(categoryNames) {
		return categoryNames[c]
	}
	return "unknown"
}

// AgentError is a structured error with category, code, and retryable flag.
type AgentError struct {
	Category  Category
	Code      string
	Message   string
	Cause     error
	Retryable bool

	// RetryAfter, when > 0, is the minimum delay a caller should wait before
	// retrying (populated from a provider 429/503 Retry-After header).
	RetryAfter time.Duration
}

func (e *AgentError) Error() string { return e.Message }
func (e *AgentError) Unwrap() error { return e.Cause }

// NewThrottled creates a retryable rate-limit error carrying a Retry-After delay.
func NewThrottled(retryAfter time.Duration, format string, args ...any) *AgentError {
	return &AgentError{
		Category:   CategoryModel,
		Code:       "model.rate_limited",
		Message:    fmt.Sprintf(format, args...),
		Retryable:  true,
		RetryAfter: retryAfter,
	}
}

// RetryAfterOf returns the Retry-After delay from an AgentError in the chain, if
// any. The bool reports whether a delay was found.
func RetryAfterOf(err error) (time.Duration, bool) {
	var ae *AgentError
	if errors.As(err, &ae) && ae.RetryAfter > 0 {
		return ae.RetryAfter, true
	}
	return 0, false
}

// Newf creates a new AgentError with a formatted message.
func Newf(category Category, code string, format string, args ...any) *AgentError {
	return &AgentError{
		Category: category,
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
	}
}

// Wrap creates a new AgentError wrapping an existing error.
func Wrap(cause error, category Category, code string, message string) *AgentError {
	return &AgentError{
		Category: category,
		Code:     code,
		Message:  message,
		Cause:    cause,
	}
}

// IsRetryable returns true if err is an AgentError with Retryable=true.
// It traverses the error chain via errors.As.
func IsRetryable(err error) bool {
	var ae *AgentError
	if errors.As(err, &ae) {
		return ae.Retryable
	}
	return false
}

// Sentinel errors. Use errors.Is to match.
var (
	ErrModelRateLimited  = &AgentError{Category: CategoryModel, Code: "model.rate_limited", Message: "model rate limited", Retryable: true}
	ErrModelTimeout      = &AgentError{Category: CategoryModel, Code: "model.timeout", Message: "model call timed out", Retryable: true}
	ErrModelContextLimit = &AgentError{Category: CategoryContext, Code: "context.too_long", Message: "context exceeds model limit", Retryable: false}
	ErrToolDenied        = &AgentError{Category: CategoryPermission, Code: "tool.denied", Message: "tool execution denied", Retryable: false}
	ErrToolTimeout       = &AgentError{Category: CategoryTool, Code: "tool.timeout", Message: "tool execution timed out", Retryable: false}
	ErrSandboxDenied     = &AgentError{Category: CategoryPermission, Code: "sandbox.denied", Message: "sandbox execution denied", Retryable: true}
	ErrLoopInterrupted   = &AgentError{Category: CategoryContext, Code: "loop.interrupted", Message: "loop was interrupted", Retryable: false}
	ErrLoopMaxIters      = &AgentError{Category: CategoryContext, Code: "loop.max_iters", Message: "maximum iterations reached", Retryable: false}
	ErrBudgetExceeded    = &AgentError{Category: CategoryResource, Code: "budget.exceeded", Message: "budget exceeded", Retryable: false}
)
