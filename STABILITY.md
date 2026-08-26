# API Stability & Production Hardening

This document states the stability guarantees of `agentscope-go` and tracks the
production-hardening status of the library.

## Versioning

The module path is `github.com/alanfokco/agentscope-go/v2`. The latest release
tag is `v2.0.9`. Consumers import as:

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

- ~~`RedisFullStorage`~~ **Done:** full `FullStorage` implementation over Redis,
  using the existing `RedisClient` interface (no hard dependency on go-redis).
  All 28 methods implemented: credentials, agents, sessions, schedules, messages,
  teams. Message ordering preserved via per-session index key.
- ~~`SecretStr` dual-field (Phase 2)~~ **Done:** all 8 model provider configs
  now carry `SecretAPIKey SecretStr` alongside deprecated `APIKey string`.
  Constructors use `ResolveAPIKey()` to prefer the secret field. TTS/embedding/
  workspace configs deferred to v3 (lower sensitivity).
- ~~`exception` package removal (Phase 2)~~ **Done:** all 4 importing packages
  (`agent`, `tool`, `tool/orchestrator`, `tool/orchestrator_test`) migrated to
  `errors/`. Zero imports of `exception` remain outside the alias package itself.
  The `exception` package still exists as a backward-compatible alias layer and
  will be removed after the next minor version.
- ~~OTLP setup helper~~ **Done:** shipped as `examples/tracing_otlp/` with a
  documented wiring pattern. No OTel SDK dependency added to the library.

- ~~`exception` package deletion (final)~~ **Done:** package removed entirely.
  All code uses `errors/` directly.
- ~~`SecretStr` adoption in TTS/embedding/workspace/hub configs~~ **Done:** all 14
  remaining config structs gained `SecretAPIKey model.SecretStr` with
  `ResolveAPIKey()` in constructors. Full coverage across the entire framework.
- ~~Output guardrails~~ **Done:** `GuardrailMiddleware` with 3 actions
  (Block/Redact/Warn), rule-based content filtering on model responses. Built-in
  rules: KeywordBlock, KeywordRedact, MaxLength, Custom. Hooks into `OnModelCall`.

All originally-planned production hardening items are now complete.
