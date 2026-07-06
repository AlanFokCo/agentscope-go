# API Stability & Production Hardening

This document states the stability guarantees of `agentscope-go` and tracks the
production-hardening status of the library.

## Versioning

The module path is `github.com/alanfokco/agentscope-go` (a v0/v1 path).

> **Action required (maintainer decision):** git tags `v2.x` exist, but because the
> module path lacks a `/v2` suffix, `go get ...@latest` resolves to the highest
> valid v0/v1 tag (currently `v1.1.0`) and **ignores** the v2 tags. Additionally
> `v2.0.3.1` is not valid semver. Pick one:
>
> 1. **Commit to v2:** change the module path to `github.com/alanfokco/agentscope-go/v2`,
>    update all internal import paths, and re-tag `v2.x.y`. Consumers then
>    `import ".../v2/pkg/agentscope"`.
> 2. **Stay on v1:** delete the malformed `v2.*` tags and continue tagging `v1.x.y`.
>
> Until this is resolved, pin an explicit version rather than relying on `@latest`.

## Stability tiers

- **Stable** (source-compatible within a major version): `message`, `model`,
  `agent` (UnifiedAgent), `tool`, `permission`, `formatter`, `errors`.
- **Experimental** (may change): `runtime`, `loop`, `app`, `service`, `realtime`,
  `tune`. These are the newer v3 infrastructure layers.
- **Internal** (`internal/...`): no compatibility guarantee; do not import.

## Error handling

Errors are structured `*errors.AgentError` (category + code + retryable +
optional `RetryAfter`) and are `errors.Is` / `errors.As` compatible. Match
sentinels such as `errors.ErrModelRateLimited`, `errors.ErrToolDenied`,
`errors.ErrLoopMaxIters`, `errors.ErrBudgetExceeded`. `model.IsRetryableError`
honors the typed `Retryable` flag; `errors.RetryAfterOf` extracts a throttle delay.

## Construction contract

Constructors validate inputs and **panic on programmer error** (mirroring
`message.NewMsg`): e.g. `agent.NewUnifiedAgent` panics on a nil model or empty
name. These indicate misuse, not runtime conditions — fix the call site.

## Production hardening status

Landed:

- **Security:** bash read-only classification rejects write redirects (was an
  auto-allow bypass); curl/wget removed from the read-only allowlist; opt-in
  workspace-root jail for file tools; WebFetch SSRF guard (blocks
  loopback/private/link-local incl. cloud metadata); MCP subprocesses run with a
  minimal env instead of inheriting parent secrets; credentials kept out of URLs
  and redacted in logs.
- **Reliability:** retries honor 429/`Retry-After` with ctx-aware full-jitter
  backoff; ordered `FallbackChatModel` chain; single-probe half-open circuit
  breaker; per-tool timeout + result cap.
- **Concurrency:** SSE/loop/turn/session event sends are cancellation-aware
  (goroutine-leak fixes); manager/scheduler/task snapshots avoid torn reads;
  `permission.Engine` and `pipeline.MsgHub` are mutex-guarded.
- **Streaming:** long streams are no longer truncated by the client's whole-request
  timeout; `ChatResponse` carries `Error` and `StopReason`.
- **Durability:** all file-backed writes are atomic (temp + fsync + rename).
- **Budgets:** token and duration budgets are enforced on the loop path.
- **Ops:** HTTP servers set `ReadHeaderTimeout`/`IdleTimeout`/`MaxHeaderBytes` and
  cap request bodies; `/healthz` + `/readyz`; graceful `Shutdown`; reference
  Dockerfile.
- **Observability:** OTEL span attributes recorded; label-aware in-memory metrics.
- **Testing:** fuzzers for the safety parsers and JSON decoder; CI fuzz smoke +
  coverage.

Planned (tracked):

- Full tool-in-sandbox execution. Done: `workspace.ToolBackend` adapter
  (Workspace → `tool.Backend`) and Bash routing through a configured backend.
  Remaining: file tools (read/write/edit) resolve host-absolute paths, so routing
  them through a non-local backend needs per-backend path handling.
- Shipped Prometheus/OTLP exporter + `/metrics` + trace-context propagation;
  `ctx` threaded through `loop.Hook` so spans nest under the caller.
- Durable `FullStorage` (credentials/agents/schedules), session-history persistence
  and resume, schema versioning, reference Redis adapter.
- USD spend cap (model-card pricing), JSON-Schema input validation, output
  guardrails.
- Anthropic prompt-caching write path; record/replay + eval harness; additional
  built-in tools.
- Full `SecretStr` adoption; `exception`→`errors` migration; module `/v2` decision.
