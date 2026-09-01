# AGENTS.md

Handoff guide for coding agents (and humans) working on **agentscope-go**. Read this first, then `CLAUDE.md` for the full architecture and `STABILITY.md` for what's shipped vs. open.

## What this is

A Go port of the Python [AgentScope](https://github.com/agentscope-ai/agentscope) multi-agent LLM framework.

- **Module path: `github.com/alanfokco/agentscope-go/v2`** (v2+ line). Imports use `github.com/alanfokco/agentscope-go/v2/pkg/agentscope/...`. Latest tag: **`v2.0.9`**.
- Library under `pkg/agentscope/`; runnable demos under `examples/`.
- `go.mod` says `go 1.25.0` — keep code **Go 1.25+ compatible** (the minimum version declared in `go.mod`).
- Python reference (for design parity) at `/Users/alanfokco/Github/agentscope/`.

## Build / test / lint

```bash
go build ./... && go build ./examples/...
go vet ./...
go test -race -count=1 ./...                       # CI runs with -race
golangci-lint run ./...                             # v2; go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go test ./pkg/agentscope/tool -run TestName -v      # single test
```

CI (`.github/workflows/ci.yml`) is a 3-OS matrix — **ubuntu / macos / windows** — plus lint, a loop benchmark, a coverage step, and a 30s fuzz smoke. Windows exercises the PowerShell/Cmd paths, so:
- shell-specific Unix tests guard with `if runtime.GOOS == "windows" { t.Skip("requires Unix shell") }`;
- sandbox/workspace-relative paths use the `path` package (forward slash), **not** `filepath` (which is `\` on Windows).

## Commit / deploy workflow (READ THIS — it's non-obvious)

Git lives on **builder** (`root@builder:/opt/Projects/agentscope-go`), not in the local checkout. Never `git commit`/`push` locally.

1. Edit locally.
2. **Check builder is clean first**: `ssh root@builder 'cd /opt/Projects/agentscope-go && git branch --show-current && git status --short'`. The maintainer sometimes works directly on builder — don't rsync over uncommitted edits, and note which branch is checked out (commits land on whatever is checked out).
3. `rsync` only the files you changed (see below), then verify on builder:
   `ssh root@builder 'cd /opt/Projects/agentscope-go && export PATH=$PATH:/usr/local/go/bin && go build ./... && go vet ./... && go test -race -count=1 ./...'`
4. `git add ... && git commit -F <msgfile> && git push` **on builder**.

Gotchas that will bite you:
- **Local git HEAD lags builder.** Don't trust local `git diff`/`status` to enumerate your changes. Rsync your edited files, then `git status` on builder shows the true diff against HEAD.
- **`vendor/` is `.gitignore`d.** Adding a dependency: on builder run `go get <pkg> && go mod tidy && go mod vendor`, then commit **only `go.mod` + `go.sum`**. CI restores deps from the proxy.
- **Tag pushes/deletes need `git push --no-verify`.** The pre-push AK-leak-scan hook errors on tag refs (`invalid local oid`); the commit was already scanned on the branch push, so bypassing for tags is safe.
- **Commit messages: no "Claude"/AI mentions, no `Co-Authored-By`.** Use `git commit -F <file>` (rsync a message file) to dodge ssh quoting issues.

Rsync example (single file, preserving path):
```bash
rsync -azR pkg/agentscope/model/model.go root@builder:/opt/Projects/agentscope-go/
```

## Current state (2026-09)

Feature-complete Python parity **plus** a large production-hardening pass (see `STABILITY.md` for the full list). Highlights already shipped: bash-redirect safety, workspace jail + Docker/E2B backend routing for file/shell tools, WebFetch SSRF guard, MCP env isolation, per-tool timeout/result caps, 429/Retry-After+jitter retries, ordered fallback chain, single-probe circuit breaker, streaming-error propagation (`ChatResponse.Error`/`StopReason`), ctx-aware event emission (goroutine-leak fixes), atomic file writes, token/duration budget enforcement, HTTP hardening + `/healthz`+`/readyz`+`/metrics`, a Prometheus metrics provider, JSON-Schema tool-input validation, MultiEdit + ApplyPatch tools, and fuzz targets. All green under `-race` and golangci-lint.

Recent additions (2026-08):
- **Process-group isolation** (`proc_unix.go`/`proc_windows.go`): child processes are killed as a group on timeout, preventing orphans/fork-bombs.
- **Interpreter attack detection** (`CheckInterpreterAttack`): blocks dangerous API calls hidden inside `python -c`, `node -e`, `perl -e`, etc.
- **Write hardening**: 10 MB size cap, atomic writes (`fsutil.WriteFileAtomic`), executable-extension bypass-immune ASK.
- **Sandbox Policy enforcement** (`orchestrator.enforceSandboxPolicy`): the `sandbox.Policy` struct now actually controls tool execution — FSReadOnly blocks writes, AllowExec=false blocks bash, NetDisabled blocks WebFetch, DenyPaths blocks file access.
- **Audit logging** (`audit/`): structured `audit.Logger` interface with InMemory/File/Multi/Nop implementations; orchestrator records every tool execution, permission denial, and policy decision.
- **Sandbox execution events** (`event/`): `tool_exec_start`, `tool_exec_end`, `tool_policy_denied` — visibility into what happens inside the execution layer.
- **Eval harness** (`replay/eval.go`): `Scorer` interface with 5 built-in scorers (ExactMatch, Contains, JSONField, TextContains, Composite), `EvalTape()` runner, `AssertTape(t, ...)` go-test helper for regression testing.
- **Hard spend cap** (`middleware/cost_tracker.go`): `WithMaxCostUSD(limit)` pre-flight budget enforcement + `WithExchangeRate("CNY", 7.2)` for multi-currency display.
- **Output guardrails** (`middleware/guardrail.go`): `GuardrailMiddleware` with Block/Redact/Warn actions + 4 built-in rules (KeywordBlock, KeywordRedact, MaxLength, Custom).
- **Reranker** (`rag/rerank.go`): `Reranker` interface + `RerankedIndex` wrapper for precision-improving two-stage retrieval.
- **RedisFullStorage** (`storage/redis_full.go`): full `FullStorage` implementation over Redis (28 methods) with reverse-index message lookup.
- **SecretStr adoption**: `UnmarshalJSON` + `ResolveAPIKey()` helper; `SecretAPIKey` dual-field across all 22 config structs.
- **`exception` → `errors` migration**: tool error types moved to `errors/tool_errors.go`; `AgentError.Is()` matches sentinels by Code; `AgentError.AgentMessage()` bridges LLM-facing/operator-facing errors. `exception/` package removed.

- **K8s workspace hardening** (`workspace/k8s.go`): `PodSecurityContext` (RunAsNonRoot/User/Group/FSGroup), `ResourceRequirements` (CPU/Memory limits+requests), `ServiceAccountName`, Labels/Annotations, `PodTTLSeconds` (activeDeadlineSeconds anti-leak), `ImagePullPolicy`, `DisableServiceAccount`, `SecretToken` (SecretStr). Bug fixes: duplicate timeout, GNU find portability, `buildPodManifest()` testability extraction.
- **K8s cluster tools** (`workspace/k8s_tools.go`): `NewKubectlGetTool` (15 resource types, secrets BLOCKED), `NewKubectlLogTool` (tail/since/container). Read-only, 30s timeout, kubectl shell-out (no client-go dep).

All originally-planned STABILITY.md items are now complete.

Recent additions (2026-09) — **harness engineering batch** (evaluation, regression defense, cost governance, resilience, crash recovery):
- **Flight recorder** (`replay/`): ring + per-entry size limits, atomic dump-on-error tapes, redaction hook; entries carry `reply_id`/`usage`. Reply IDs correlate through MiddleContext into recorders, audit entries, and tracing spans (`tracing.LateAttributer`).
- **`event/streamcheck`**: single implementation of event-stream invariants; `agenttest` delegates to it; opt-in `middleware.NewStreamValidator` for development.
- **Provider contract wall** (`providercontract/`, test-only): 6 provider harnesses asserting usage accounting, streaming lifecycle, truncation surfacing, ctx-cancel, error taxonomy, thinking wire formats.
- **Golden replay seeds** (`agent/testdata/golden/`): regenerate with `-golden-update`.
- **Runtime defenses**: `middleware.NewRepetitionBreaker` (tool-call spin detection; per-reply streaks), `middleware.NewReplyWatchdog` (wall-clock + idle timeouts), cost governance (`model.ResolvePrice` overlay, `CostLedger` + `NewCostTracking`, `NewReplyCostBudget` with soft warning + hard `ErrBudgetExceeded` stop).
- **Evaluation kit** (`replay/evalkit/`): YAML task suites, pinned-sampling runner, scorers incl. LLM judge with caching, multi-turn tasks, Markdown suite reports, A/B `Compare`.
- **Run logs**: `middleware.NewRunJSONL` + `replay.ParseRunLog`/`DiffRunLogs` (LCS alignment with truncation flag); `examples/replayview` + `examples/rundiff`.
- **Crash recovery**: `agent.WithStateSaver` checkpoints at batch boundaries/park points (and right after resumed calls execute); `agent.LoadCheckpoint` resumes and re-drives pending HITL/external handshakes. Contract: a crash mid-batch re-executes the whole batch (not exactly-once).
- **Fault injection** (`agenttest/faults/`) and **bench v2** (`Battery` + `Baseline` + `CheckBaseline` regression detection); `model.WithSeed` pass-through for OpenAI-family providers.
- **Console** (`console/`, Phase 1): `Renderer` (event stream → terminal, 3 verbosity levels, `LastMsg`) + `Launch` (interactive chat with tool-call confirmation and Ctrl+C interruption); `examples/console`.

## Conventions (summary; full list in CLAUDE.md)

- `context.Context` first arg; return `(T, error)` — don't panic (exceptions: `message.NewMsg`, `agent.NewUnifiedAgent` panic on programmer error).
- Interfaces + embeddable `BaseXxx` defaults; functional options (`opts ...XxxOption`).
- Streaming = `<-chan T`, deltas then final `IsLast=true`, `defer close(ch)`; sends should be ctx-aware.
- **TDD** for behavior changes: write the failing test, watch it fail, then implement.
- New example → own `examples/<name>/main.go` + add to `README.md` (CI builds all examples).
- Errors: structured `errors.AgentError` + sentinels (`errors.Is`/`As` via `AgentError.Is()` matching by `Code`); `IsRetryableError` honors the typed retryable flag; `AgentMessage()` for LLM-facing messages.
- `golangci-lint run ./...` must pass before every commit (CI gates on it).

## Quality Gate: Evaluator Adversarial Review (MANDATORY)

Before any commit and push, the following MUST be verified through an evaluator (adversarial reviewer):

1. **Code changes**: Run evaluator to adversarially review all new/modified code for:
   - Logic bugs, race conditions, nil panics
   - Missing edge cases and error handling
   - API misuse or design flaws
   - Security vulnerabilities

2. **Documentation changes**: Run evaluator to verify:
   - All code examples compile correctly against actual source
   - All API references (function names, signatures, struct fields) match reality
   - All numeric claims (counts, versions) are accurate
   - No broken links or stale references

3. **Commit criteria** — a commit is allowed ONLY when:
   - `go build ./...` passes
   - `go vet ./...` passes
   - `go test -race -count=1 ./...` passes (or affected packages)
   - `golangci-lint run ./...` shows 0 issues
   - Evaluator adversarial review returns PASS (no HIGH-severity findings)

Skipping the evaluator review is NOT acceptable. If time is constrained, at minimum run the evaluator on the specific packages modified.
