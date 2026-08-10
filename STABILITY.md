# API Stability & Production Hardening

This document states the stability guarantees of `agentscope-go` and tracks the
production-hardening status of the library.

## Versioning

The module path is `github.com/alanfokco/agentscope-go/v2`. The latest release
tag is `v2.1.0`. Consumers import as:

```go
import "github.com/alanfokco/agentscope-go/v2/pkg/agentscope"
```

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
- **Observability:** OTEL span attributes recorded; label-aware in-memory metrics;
  sandbox execution events (`tool_exec_start`/`tool_exec_end`/`tool_policy_denied`).
- **Audit:** structured `audit.Logger` interface with InMemory/File/Multi/Nop
  implementations; the orchestrator records every tool execution, permission
  denial, and sandbox policy decision.
- **Process isolation:** child processes run in a dedicated process group
  (`Setpgid`); timeout kills the entire group, preventing orphans and fork-bombs.
- **Interpreter attack detection:** `CheckInterpreterAttack` detects dangerous
  API calls hidden inside `python -c`, `node -e`, `perl -e`, etc. (8 languages,
  20+ dangerous API patterns).
- **Sandbox policy enforcement:** `sandbox.Policy` is now actually enforced by
  the orchestrator — FSReadOnly blocks writes, AllowExec=false blocks bash,
  NetDisabled blocks WebFetch, DenyPaths blocks file access.
- **Write hardening:** 10 MB write-size cap, atomic writes via `fsutil.WriteFileAtomic`,
  executable-extension bypass-immune ASK (.sh/.py/.exe etc.).
- **Testing:** fuzzers for the safety parsers and JSON decoder; CI fuzz smoke +
  coverage.

- **Edge & Embedded Intelligence:** ConnectivityAwareModel (cloud/local routing
  via circuit breaker), PubSub interface + MQTT adapter (build tag: mqtt),
  Device framework (Serial/GPIO/CAN/I2C pure-Go drivers + DeviceTool + Watchdog +
  SensorMiddleware), cross-arch CI (arm64/arm/mips64le/riscv64), binary ~6MB.
Recently landed (since initial hardening):

- ~~Full tool-in-sandbox execution.~~ **Done:** `workspace.ToolBackend` adapter
  (Workspace → `tool.Backend`); Bash, read, write, and edit all route through a
  configured backend using workspace-relative paths. Wire with
  `tool.WithBackend(ctx, workspace.NewToolBackend(ws))` for real Docker/E2B
  isolation; the rich local path (streaming/cwd/read-cache) is the default.
- ~~Prometheus exporter + `/metrics` + trace-context propagation~~ **Done:**
  `metrics/prometheus` provider; `ctx` threaded through `loop.Hook` so spans
  nest under the caller.
- ~~JSON-Schema input validation~~ **Done:** `tool.ValidateInput` with fuzz target.
- ~~Module `/v2` decision~~ **Done:** module path is now `/v2`.

- ~~USD/CNY spend cap~~ **Done:** `CostTrackerMiddleware` now supports
  `WithMaxCostUSD` hard cap (pre-flight budget check, returns `ErrBudgetExceeded`)
  and `WithExchangeRate("CNY", 7.2)` for display-currency conversion.
- ~~Record/replay eval harness~~ **Done:** `replay.Scorer` interface with 5
  built-in scorers (ExactMatch, Contains, JSONField, TextContains, Composite),
  `EvalTape()` runner producing `EvalReport`, and `AssertTape(t, ...)` go-test
  helper for regression testing agent behavior against recorded tapes.
- ~~Anthropic prompt-caching write path~~ **Done:** `AnthropicConfig.PromptCaching`
  + `applyPromptCaching()` + cache token tracking in both streaming and
  non-streaming paths. (Needs live-API integration test.)
- ~~`exception`→`errors` migration (Phase 1)~~ **Done:** Tool error types
  (`ToolNotFoundError`, `ToolJSONDecodeError`, etc.) moved to `errors/`;
  `errors.AgentError` gained `AgentMsg`/`AgentMessage()` for LLM-facing messages;
  `exception` package is now type aliases with deprecation notices.
- ~~`SecretStr.UnmarshalJSON`~~ **Done:** `SecretStr` can now be populated from
  JSON config files; value is stored internally, never re-exposed via Marshal.

Planned (tracked):

- Durable `FullStorage`: `RedisFullStorage` reference implementation of the
  existing `FullStorage` interface. (Schema versioning deferred — only v1 exists.)
- `SecretStr` adoption (Phase 2): dual-field pattern (`APIKeySecret SecretStr`
  alongside deprecated `APIKey string`) across all 9 model providers + TTS +
  embedding + workspace configs. Full removal of `APIKey string` deferred to v3.
- `exception` package removal (Phase 2): complete the migration by removing the
  `exception` package after deprecation cycle (target: 2 minor versions).
- OTLP setup helper: optional `tracing/otlphelper.SetupOTLP(endpoint)` convenience
  function (not a full exporter package — avoids OTel SDK dependency bloat).
  Alternatively ship as `examples/tracing_otlp/`.
- Output guardrails (content filtering / safety checks on model responses).
