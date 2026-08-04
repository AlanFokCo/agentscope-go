# Deployment

## Agent Service

agentscope-go includes a built-in HTTP Agent Service for deploying agents as web services.

### Basic Setup

```go
svc := service.New(&service.Config{
    Addr:           ":8080",
    AllowedOrigins: []string{"*"},
}, chatModel, func(name, prompt string, _ model.ChatModel) *agent.UnifiedAgent {
    return agent.NewUnifiedAgent(name, prompt, chatModel,
        agent.WithToolkit(tool.NewEnhancedToolkit()),
    )
})
svc.ListenAndServe()
```

### REST Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/session` | Create a new session |
| GET | `/api/sessions` | List all sessions |
| POST | `/api/chat/{id}` | Send a message (sync) |
| GET | `/api/chat/{id}/stream` | Stream chat via SSE |
| POST | `/api/chat/{id}/confirm` | Confirm HITL request |
| GET | `/api/models` | List available models |

### Full Application

For production deployments with multi-session management, credentials, scheduling, and workspace isolation:

```go
app, _ := app.CreateApp(&app.AppConfig{
    Addr:           ":8080",
    AllowedOrigins: []string{"https://your-frontend.com"},
    Storage:        redisStorage,
    MessageBus:     messagebus.NewInMemoryMessageBus(),
})
```

## Workspace Sandboxing

For untrusted tool execution, use isolated workspaces:

| Workspace | Isolation Level | Use Case |
|-----------|----------------|----------|
| `LocalWorkspace` | Directory-scoped | Development, trusted agents |
| `DockerWorkspace` | Container-level | Production, semi-trusted |
| `E2BWorkspace` | Cloud sandbox | Full isolation, untrusted code |
| `K8sWorkspace` | Kubernetes Pod | Production clusters, multi-tenant |
| `OpenSandboxWorkspace` | Cloud sandbox API | Remote sandbox-as-a-service |
| `DaytonaWorkspace` | Dev environment | Daytona-managed dev containers |
| `AppleContainerWorkspace` | Apple Container | macOS-native lightweight containers |
| `BubblewrapWorkspace` | Linux bwrap | Minimal Linux sandboxing without Docker |

### Docker Workspace

```go
ws, _ := workspace.NewDockerWorkspace(&workspace.DockerConfig{
    Image:      "python:3.11-slim",
    WorkingDir: "/workspace",
})
backend := workspace.NewDockerBackend(ws)
bashTool := tool.BashToolWithBackend(backend)
```

### Kubernetes Workspace

Run agent tool execution inside ephemeral Kubernetes Pods:

```go
ws, _ := workspace.NewK8sWorkspace(&workspace.K8sConfig{
    Namespace:  "agent-sandbox",
    Image:      "python:3.11-slim",
    KubeConfig: "/path/to/kubeconfig",
})
backend := workspace.NewK8sBackend(ws)
bashTool := tool.BashToolWithBackend(backend)
```

### OpenSandbox Workspace

Use the OpenSandbox API for fully managed cloud sandboxes:

```go
ws, _ := workspace.NewOpenSandboxWorkspace(&workspace.OpenSandboxConfig{
    APIKey:  os.Getenv("OPENSANDBOX_API_KEY"),
    BaseURL: "https://api.opensandbox.dev",
    Image:   "python:3.11",
})
```

### Daytona Workspace

Leverage Daytona for development-oriented sandbox environments:

```go
ws, _ := workspace.NewDaytonaWorkspace(&workspace.DaytonaConfig{
    ServerURL: "https://daytona.example.com",
    APIKey:    os.Getenv("DAYTONA_API_KEY"),
    Image:     "ubuntu:22.04",
})
```

### Apple Container Workspace

On macOS, use Apple's Container framework for lightweight native isolation:

```go
ws, _ := workspace.NewAppleContainerWorkspace(&workspace.AppleContainerConfig{
    Image:      "swift:latest",
    WorkingDir: "/workspace",
})
```

### Bubblewrap Workspace

Minimal Linux sandboxing via `bwrap` without needing Docker:

```go
ws, _ := workspace.NewBubblewrapWorkspace(&workspace.BubblewrapConfig{
    AllowedPaths: []string{"/usr", "/lib", "/tmp"},
    ReadOnlyRoot: true,
    UnshareNet:   true,
})
```

## WASM Sandbox

Execute untrusted code as WebAssembly modules with strict resource limits. No container runtime needed — just a WASM runtime binary (`wasmtime`, `wasmer`, or `wasm3`).

```go
rt, _ := wasm.NewCLIRuntime("")  // auto-discover wasmtime/wasmer/wasm3
sandbox := wasm.NewSandbox(wasm.SandboxConfig{
    Runtime:     rt,
    MaxMemory:   64 * 1024 * 1024, // 64MB
    MaxDuration: 10 * time.Second,
    MaxFuel:     1_000_000,        // instruction count limit
})

result, _ := sandbox.Run(ctx, "plugin.wasm", []byte(`{"input": "hello"}`))
fmt.Println(string(result.Stdout))
```

Key properties:
- **Memory-limited**: Hard cap on heap allocation
- **Time-limited**: Execution timeout
- **CPU-limited**: Fuel (instruction count) budget
- **Portable**: Same `.wasm` binary runs on any OS/arch
- **No network by default**: Modules cannot access the network unless explicitly granted WASI permissions

## Hot-Reload Configuration

Update agent configuration at runtime without restarting. The `hotreload` package watches files for changes and notifies handlers.

### File Watcher

```go
w := hotreload.NewWatcher(hotreload.WatcherConfig{
    PollInterval: 2 * time.Second,
})

w.Watch("config/agent.json", func(evt hotreload.ChangeEvent, data []byte) error {
    log.Printf("Config changed at %s", evt.Timestamp)
    // Parse and apply new config
    return nil
})

w.Start(ctx)
defer w.Stop()
```

### Typed Config Reloader

For type-safe config with automatic JSON unmarshaling:

```go
type AgentConfig struct {
    SystemPrompt string   `json:"system_prompt"`
    MaxIters     int      `json:"max_iters"`
    Model        string   `json:"model"`
    Tools        []string `json:"tools"`
}

reloader, _ := hotreload.NewReloader[AgentConfig]("config/agent.json", w,
    hotreload.WithOnChange(func(old, new_ *AgentConfig) {
        log.Printf("Prompt changed: %q -> %q", old.SystemPrompt, new_.SystemPrompt)
    }),
)

// Read the current config (lock-free atomic pointer)
cfg := reloader.Get()
```

## Agent Pool (High-Throughput Deployment)

Fan out work across a pool of agent workers for high-throughput batch processing:

```go
pool := runtime.NewAgentPool(
    func() agent.Agent {
        return agent.NewUnifiedAgent("worker", "You are a data processor.", cm,
            agent.WithToolkit(tool.NewEnhancedToolkit()),
        )
    },
    runtime.Workers(8),
    runtime.QueueSize(100),
)
pool.Start(ctx)
defer pool.Close()

// Submit work items
for _, item := range workItems {
    result := pool.Submit(ctx, item)
    go func(r <-chan runtime.PoolResult) {
        res := <-r
        if res.Err != nil {
            log.Printf("Error: %v", res.Err)
        } else {
            log.Printf("Result: %s", res.Output.GetTextContent("\n"))
        }
    }(result)
}
```

Each worker owns its own agent instance — no shared state, no locking overhead.

## Deterministic Replay for CI/CD

Record agent interactions once, then replay them deterministically in CI without API keys or network access.

### Recording

```go
recorder := replay.NewRecorder()
a := agent.NewUnifiedAgent("bot", "You are a test agent.", cm,
    agent.WithMiddlewares(recorder),
)

// Run the agent normally — all model calls are recorded
a.Reply(ctx, "Summarize the Q3 report")

// Save the tape
data, _ := json.Marshal(recorder.Tape())
os.WriteFile("testdata/q3_summary.tape.json", data, 0644)
```

### Replaying in Tests

```go
func TestQ3Summary(t *testing.T) {
    data, _ := os.ReadFile("testdata/q3_summary.tape.json")
    var tape replay.Tape
    json.Unmarshal(data, &tape)

    replayer := replay.NewReplayer(&tape)
    a := agent.NewUnifiedAgent("bot", "You are a test agent.", nil,  // no model needed!
        agent.WithMiddlewares(replayer),
    )

    reply, err := a.Reply(context.Background(), "Summarize the Q3 report")
    require.NoError(t, err)
    assert.Contains(t, *reply.GetTextContent("\n"), "revenue")
}
```

No API keys, no network, fully deterministic.

## Scheduled Tasks

Run agent tasks on a schedule:

```go
scheduler := schedule.NewInMemoryScheduler()

scheduler.Schedule(ctx, &schedule.Task{
    Name:     "daily-report",
    Interval: 24 * time.Hour,
}, func(ctx context.Context, task *schedule.Task) error {
    _, err := agent.Reply(ctx, "Generate the daily summary report.")
    return err
})
```

## Production Checklist

- [ ] Set `permission.ModeDefault` or `ModeAcceptEdits` (never `Bypass` in production)
- [ ] Use `DockerWorkspace`, `K8sWorkspace`, or `E2BWorkspace` for tool execution
- [ ] Configure `ClientOptions.Timeout` for your expected response times
- [ ] Set up `TracingMiddleware` with OpenTelemetry exporter
- [ ] Use `ReplyBudgetControlMiddleware` to cap token spending
- [ ] Rotate API keys and use `model.SecretStr` to prevent key leakage in logs
- [ ] Put the Agent Service behind authentication (it has no built-in auth)
- [ ] Use Redis-backed storage and message bus for multi-instance deployments
- [ ] Configure `access` policies for multi-tenant resource sharing
- [ ] Enable `hotreload` to update agent configs without downtime
- [ ] Use `replay` tapes in CI to test agent behavior deterministically
- [ ] Use `AgentPool` with appropriate worker counts for batch workloads

## See Also

- [Architecture](architecture.md) — Package structure and design
- [Go-Exclusive Features](go-exclusive.md) — Replay, Pool, Hot-reload, WASM, TCP Mesh, Bench
- [Examples](examples.md) — Runnable demos for all deployment patterns
