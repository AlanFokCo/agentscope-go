package types

// ReplyFinishedReason is the reason a reply finished (parity with Python
// agentscope's types.ReplyFinishedReason).
type ReplyFinishedReason string

const (
	ReplyCompleted      ReplyFinishedReason = "completed"
	ReplyInterrupted    ReplyFinishedReason = "interrupted"
	ReplyExceedMaxIters ReplyFinishedReason = "exceed_max_iters"
	ReplyError          ReplyFinishedReason = "error"
)

// ReplyErrorType classifies a fatal error that terminated a reply. Not
// model-specific: the status-derived members apply to any upstream service
// reached during a reply (chat model, embedding, TTS, MCP).
type ReplyErrorType string

const (
	ErrorAuthentication ReplyErrorType = "authentication" // 401
	ErrorPermission     ReplyErrorType = "permission"     // 403
	ErrorRateLimit      ReplyErrorType = "rate_limit"     // 429
	ErrorInvalidRequest ReplyErrorType = "invalid_request"
	ErrorUpstream       ReplyErrorType = "upstream" // 5xx
	ErrorConnection     ReplyErrorType = "connection"
	ErrorInternal       ReplyErrorType = "internal"
	ErrorSetup          ReplyErrorType = "setup"
	ErrorUnknown        ReplyErrorType = "unknown"
)

// ReplyErrorInfo is a structured, UI-facing description of a fatal reply
// error.
type ReplyErrorInfo struct {
	Type    ReplyErrorType `json:"type"`
	Message string         `json:"message"`
}
