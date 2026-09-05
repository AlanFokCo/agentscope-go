# Execution and session hardening

This batch tightens the execution, WASM, TCP, and experimental runtime paths.

- `tool.Orchestrator` implements `loop.ToolExecutor` directly. Tool lookup and
  permission checks use the same resolved instance; toolkit registration rejects
  case-insensitive name collisions, and schema enumeration is deterministic.
- `UnifiedAgentRunner` applies the agent permission engine and acting middleware.
  The loop bridge returns a tool error for confirmation-required or external
  execution. Use `UnifiedAgent.ReplyStream` for interactive confirmation.
- Synchronous Bash execution reports a nonzero exit as a tool error, including
  execution through a configured backend.
- WASM requests propagate fuel, memory, directory grants, and output limits.
  The CLI implementation enforces these settings through Wasmtime; Wasmer and
  wasm3 return `ErrUnsupportedLimits` with the default resource limits. Combined
  stdout/stderr capture defaults to 1 MiB and reports `OutputTruncated`.
  Directory grants expose host paths to the module; grant only intended paths.
- `a2a/grpc` uses newline-delimited JSON over TCP. It is not the gRPC wire
  protocol. Transport I/O observes context cancellation, and client/server close
  releases idle connections. Active request and stream IDs share one namespace.
  Use a fresh ID when retrying a canceled request: late responses cannot be
  distinguished from a new request using the same ID on this wire protocol.
- `runtime.SessionEngine` serializes turns and preserves the context manager
  configured through `loop.WithContextManager`, including restored history.
  The loop options are resolved once when the engine is constructed. A turn
  waits for the loop to finish after a budget stop before saving history or
  allowing the next turn to run. Session state includes conversation messages;
  file-store saves use atomic replacement.

## Scope and remaining limits

This batch does not establish a complete isolation boundary for arbitrary
custom tools. Configure a workspace backend for isolated shell/file execution.
The TCP stream consumer must continue draining its channel or close its client;
stream-context cancellation alone does not retire an established stream.
Session-state snapshots copy the message slice but share message objects, and
session-store saves do not provide an automatic restore/resume constructor.

Validation uses the PR's GitHub Actions build, vet, race tests, and lint jobs,
alongside adversarial review. This document describes behavior, not a claim that
every remaining repository-wide audit finding has been resolved.
