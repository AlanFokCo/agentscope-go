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
  `tune`, `replay/evalkit`, `event/streamcheck`, `agenttest/faults`,
  `providercontract` (test-only), `console`, `channel`, `channel/dingtalk`,
  `hub` (built-in sources), `skill` (`Store` partitions), `middleware/memory`
  (`FileStore` + `AgenticMemoryMiddleware`).
  These are the newer v3 infrastructure layers, harness tooling, the
  console/channel frontends, and the Phase 3 hub/skill/memory additions.
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
- **Durability:** `storage.FileStorage`, runtime file-session saves, and selected
  write paths use `fsutil.WriteFileAtomic` (temp + file fsync + rename). This is
  not universal: replay FileStore, embedding cache, local backend writes, and
  local Edit/MultiEdit/ApplyPatch still use `os.WriteFile`.
- **Budgets:** token and duration budgets are enforced on the loop path.
- **Ops:** HTTP servers set `ReadHeaderTimeout`/`IdleTimeout`/`MaxHeaderBytes` and
  cap request bodies; `/healthz` + `/readyz`; graceful `Shutdown`; reference
  Dockerfile.
- **Observability:** OTEL span attributes recorded; label-aware in-memory metrics;
  sandbox execution events (`tool_exec_start`/`tool_exec_end`/`tool_policy_denied`).
- **Audit:** structured `audit.Logger` interface with InMemory/File/Multi/Nop
  implementations; the orchestrator records every tool execution, permission
  denial, and sandbox policy decision.
- **Process isolation:** on Unix, child processes run in a dedicated process group
  (`Setpgid`); timeout kills that group, reducing orphaned children. This is not a
  process-count limit, and Windows does not use this process-group mechanism.
- **Interpreter attack detection:** `CheckInterpreterAttack` detects dangerous
  API calls hidden inside `python -c`, `node -e`, `perl -e`, etc. (8 languages,
  20+ dangerous API patterns).
- **Sandbox policy checks:** the orchestrator applies selected name-based checks
  for built-in tools. These checks are not a complete isolation boundary:
  custom tools, alternate call-name casing, shell/network behavior, and resource
  limits need separate enforcement. Policy configuration alone does not route
  execution through `Sandbox.Execute`; use and validate a workspace backend.
- **Write hardening:** the local Write tool has a 10 MB input-size cap, atomic
  replacement via `fsutil.WriteFileAtomic`, and executable-extension
  bypass-immune ASK (.sh/.py/.exe etc.). Other file tools and backend paths
  have different persistence behavior.
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
  `metrics/prometheus` provider; trace integration through middleware/hooks. The current `loop.Hook` methods
  do not accept `context.Context`; caller-context propagation is API-specific.
- ~~JSON-Schema input validation~~ **Done:** `tool.ValidateInput` with fuzz target.
- ~~Module `/v2` decision~~ **Done:** module path is now `/v2`.

- **USD/CNY cost tracking:** `WithMaxCostUSD` rejects subsequent calls after
  already-accounted cost reaches the threshold (`ErrBudgetExceeded`). It does
  not reserve or estimate the next call's cost; individual or concurrent calls
  may overshoot. Missing usage/prices prevent complete accounting.
  `WithExchangeRate("CNY", 7.2)` converts the tracked total for display.
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
  `exception` aliases were a transitional step; the package has since been deleted.
- ~~`SecretStr.UnmarshalJSON`~~ **Done:** `SecretStr` can now be populated from
  JSON config files; value is stored internally, never re-exposed via Marshal.

- ~~`RedisFullStorage`~~ **Done:** full `FullStorage` implementation over Redis,
  using the existing `RedisClient` interface (no hard dependency on go-redis).
  All 28 methods implemented: credentials, agents, sessions, schedules, messages,
  teams. Message ordering preserved via per-session index key.
- ~~`SecretStr` dual-field (Phase 2)~~ **Done:** all 8 model provider configs
  now carry `SecretAPIKey SecretStr` alongside deprecated `APIKey string`.
  Constructors use `ResolveAPIKey()` to prefer the secret field. TTS/embedding/
  workspace configs were added in a later adoption pass listed below.
- ~~`exception` package removal (Phase 2)~~ **Done:** all 4 importing packages
  (`agent`, `tool`, `tool/orchestrator`, `tool/orchestrator_test`) migrated to
  `errors/`. Zero imports of `exception` remain outside the alias package itself.
  The temporary alias layer was removed in the final deletion step below.
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

- ~~Phase 3 porting batch~~ **Done:** built-in hub sources
  (`hub.GitHubMCPRegistry`, `hub.ClawHub`); per-agent workspace skill
  partitions (`skill.Store`, `/api/workspace/skill` routes implemented);
  agentic memory (`memory.FileStore` + `memory.AgenticMemoryMiddleware` +
  `app.WorkspaceAgentFactory` hook); session↔workspace sharing with
  refcounts + read-only artifact endpoints (`/api/workspace/share`,
  `/api/workspace/{id}/list_dir|read_file`). Path jail hardened:
  `LocalWorkspace` containment is separator-aware and symlink-aware;
  `BubblewrapWorkspace` containment is separator-aware.
  All code batches passed evaluator adversarial review (no HIGH findings).

## Open hardening work

- Complete sandbox enforcement across custom tools, call-name aliases, shell
  execution, network allowlists, and resource limits.
- Isolate nested session-state objects and provide an explicit restore lifecycle.
- Extend atomic persistence to the remaining direct-write paths.
- Define cost reservation/accounting if a strict monetary ceiling is required.

See [execution and session hardening](docs/adversarial-hardening.md) for the
changes and limits established by PRs #4 and #5. Historical completed entries
above describe individual features, not a claim that these open items are solved.
