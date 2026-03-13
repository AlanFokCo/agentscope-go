## Contributing to agentscope-go

Thank you for considering contributing to `agentscope-go`!

This project aims to be a **production-grade, Go-idiomatic multi-agent LLM framework**,
mirroring the core concepts of the Python `AgentScope` project while embracing Go best
practices (`context.Context`, explicit `error` handling, small interfaces, etc.).

Before opening a pull request, please read this guide to keep the codebase clean and
consistent.

### Development workflow

1. **Fork & branch**
   - Fork the repository on GitHub.
   - Create a feature branch from `main` (for example: `feat/rag-qdrant`, `fix/react-agent-loop`).

2. **Go version**
   - The module currently targets **Go 1.25** in `go.mod`.
   - In practice, Go **1.22+** should work for most users; please keep code compatible with
     the minimum version stated in `go.mod`.

3. **Dependencies**
   - Use the standard Go module tooling:
     - Add dependencies with `go get example.com/module@version`.
     - Run `go mod tidy` before committing when you change dependencies.
   - Prefer **small, well-maintained libraries** over large frameworks.
   - New dependencies should have a clear reason (performance, robustness, or missing
     functionality in the standard library).

4. **Build & test locally**

   From the repository root:

   ```bash
   # Build core library and all examples
   go build ./...
   go build ./examples/...

   # Run tests once there are tests available
   go test ./...

   # Basic static analysis
   go vet ./...
   ```

   Some examples require external services:

   - LLMs:
     - `OPENAI_API_KEY`
     - `ANTHROPIC_API_KEY`
     - `DASHSCOPE_API_KEY` (+ optional `DASHSCOPE_BASE_URL`)
   - Qdrant (for RAG examples you may add):
     - Qdrant running locally or in the cloud.

### Code style & design

- **Go idioms first**
  - Always pass `context.Context` as the first argument for anything that may block or call
    external services.
  - Return `(T, error)` rather than panicking.
  - Keep exported APIs small and composable; favor interfaces that describe behavior, not
    data.

- **Logging**
  - Use the global `agentscope.Log()` (`logrus.Logger`) for internal logs.
  - Choose levels carefully:
    - `Debug` for noisy internal details (request/response metadata, retries, etc.).
    - `Info` for high-level lifecycle events (initialization, upserts, important actions).
    - `Warn` for recoverable problems (retryable errors, degraded behavior).
    - `Error` only when an operation ultimately fails.

- **HTTP / external calls**
  - Prefer the shared `pkg/agentscope/internal/httpx` helpers for outbound JSON APIs
    (LLMs, A2A HTTP, etc.) instead of reimplementing HTTP logic.
  - Make retries, timeouts, and error messages consistent with existing code.

- **RAG / vector DB integrations**
  - Implement new vector backends behind the `rag.Index` and/or `rag.KnowledgeBase`
    interfaces.
  - If adding a new backend (e.g. another vector DB), mirror the Qdrant implementation:
    - Provide a low-level index that assumes vectors are pre-computed.
    - Optionally provide a higher-level “text index” that uses an `Embedder`.

- **Tracing**
  - Use the `tracing.Tracer` interface and `tracing.SetupTracing` to integrate tracing.
  - For OpenTelemetry, prefer wiring through `tracing.OTELTracer` instead of using the
    OTEL SDK directly in business logic.

### Documentation & examples

- Keep `README.md` up to date whenever you:
  - Add a new major feature.
  - Introduce a new example under `examples/`.
  - Change public APIs.

- For new features:
  - Prefer to add a **small, focused example** under `examples/` that demonstrates how to
    use the new capability in a realistic way.
  - If the feature maps to a Python AgentScope capability, briefly mention the mapping in
    `docs/migration_from_python.md`.

### Commit & PR guidelines

- **Commits**
  - Keep commits focused and logically grouped.
  - Use clear, descriptive messages (for example:
    - `feat(rag): add qdrant text index`
    - `fix(agent): avoid nil pointer in ReActAgent tools`
    - `docs: clarify Anthropic configuration`)

- **Pull requests**
  - Describe *what* and *why* in the PR description.
  - Mention any breaking changes explicitly and explain the migration path.
  - If the change affects public APIs, include or update examples and docs.

### Reporting issues

When filing an issue, please include:

- Go version (`go version`)
- OS / architecture
- Minimal reproduction code or steps
- Expected vs. actual behavior
- Any relevant logs (with secrets removed)

This helps maintainers triage and fix problems more quickly.

---

Thank you again for helping improve `agentscope-go`! Contributions of all kinds are
welcome: bug reports, documentation improvements, examples, and new backend integrations.

