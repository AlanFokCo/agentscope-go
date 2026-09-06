# Deployment

## Agent Service

agentscope-go includes a built-in HTTP Agent Service for deploying agents as web services.

### Basic Setup

```go
svc := service.New(service.Config{
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

These routes belong to `service.Service`. The separate `app` package has its own route layout.

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/session` | Create a new session |
| GET | `/api/sessions` | List all sessions |
| POST | `/api/chat` | Sync chat; JSON body includes `session_id` and `message` |
| GET | `/api/chat/stream` | SSE chat; query parameters `session_id` and `message` |
| POST | `/api/confirm` | HITL confirmation; JSON body includes `session_id` and `tool_calls` |
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
| `E2BWorkspace` | Cloud sandbox | Remote sandbox for tool execution |
| `K8sWorkspace` | Kubernetes Pod | Production clusters, multi-tenant |
| `OpenSandboxWorkspace` | Cloud sandbox API | Remote sandbox-as-a-service |
| `DaytonaWorkspace` | Dev environment | Daytona-managed dev containers |
| `AppleContainerWorkspace` | Apple Container | macOS-native lightweight containers |
| `BubblewrapWorkspace` | Linux bwrap | Minimal Linux sandboxing without Docker |

### Docker Workspace

```go
ws, err := workspace.NewDockerWorkspace(ctx, &workspace.DockerWorkspaceConfig{
    Image:   "python:3.11-slim",
    WorkDir: "/workspace",
})
if err != nil { log.Fatal(err) }
defer ws.Close(context.Background())
backend := workspace.NewToolBackend(ws)
ctx = tool.WithBackend(ctx, backend)
// Pass ctx to tool execution; only tools that consume the backend use this workspace.
```

### Kubernetes Workspace

Run agent tool execution inside hardened ephemeral Kubernetes Pods:

```go
runAsNonRoot := true
runAsUser := int64(1000)
ws, err := workspace.NewK8sWorkspace(&workspace.K8sConfig{
    Namespace:             "agent-sandbox",
    PodName:               "agent-workspace",
    Image:                 "ubuntu:22.04",
    APIServer:             "https://kubernetes.default.svc",
    SecretToken:           model.NewSecretStr(os.Getenv("K8S_TOKEN")),
    PodTTLSeconds:         3600,  // activeDeadlineSeconds: stop after 1h; Close deletes the Pod
    DisableServiceAccount: true,  // no SA token inside pod
    SecurityContext: &workspace.PodSecurityContext{
        RunAsNonRoot: &runAsNonRoot,
        RunAsUser:    &runAsUser,
    },
    Resources: &workspace.ResourceRequirements{
        CPULimit:      "2000m",
        MemoryLimit:   "1Gi",
        CPURequest:    "200m",
        MemoryRequest: "256Mi",
    },
    Labels: map[string]string{
        "app.kubernetes.io/managed-by": "agentscope",
    },
})
if err != nil { log.Fatal(err) }
defer ws.Close()
backend := workspace.NewToolBackend(ws)
ctx = tool.WithBackend(ctx, backend)
```

### Kubernetes Cluster Tools

Read-only tools for querying existing clusters (no mutation, secrets blocked):

```go
getTool := workspace.NewKubectlGetTool("/path/to/kubeconfig")
logTool := workspace.NewKubectlLogTool("/path/to/kubeconfig")
tk := tool.NewToolkit(getTool, logTool)
```

`kubectl_get` supports: pods, deployments, services, configmaps, events, nodes, namespaces, ingresses, jobs, cronjobs, statefulsets, daemonsets, replicasets, pvc, hpa. Secrets are explicitly blocked.

### OpenSandbox Workspace

Use the OpenSandbox API for fully managed cloud sandboxes:

```go
ws, _ := workspace.NewOpenSandboxWorkspace(workspace.OpenSandboxConfig{
    APIKey:   os.Getenv("OPENSANDBOX_API_KEY"),
    BaseURL:  "https://api.opensandbox.dev",
    Template: "python:3.11",
})
```

### Daytona Workspace

Leverage Daytona for development-oriented sandbox environments:

```go
ws, _ := workspace.NewDaytonaWorkspace(workspace.DaytonaConfig{
    BaseURL:     "https://daytona.example.com",
    APIKey:      os.Getenv("DAYTONA_API_KEY"),
    WorkspaceID: "my-workspace",
})
```

### Apple Container Workspace

On macOS, use Apple's Container framework for lightweight native isolation:

```go
ws, _ := workspace.NewAppleContainerWorkspace(workspace.AppleContainerConfig{
    Image: "swift:latest",
    Name:  "agent-sandbox",
})
```

### Bubblewrap Workspace

Minimal Linux sandboxing via `bwrap` without needing Docker:

```go
ws, _ := workspace.NewBubblewrapWorkspace(workspace.BubblewrapConfig{
    RootDir:      "/tmp/agent-sandbox",
    AllowNetwork: false,
})
```

## WASM Sandbox

The CLI implementation enforces fuel, linear-memory, timeout, directory-grant, and output-capture settings through Wasmtime. Wasmer and wasm3 may be discovered, but execution with the default resource limits returns `ErrUnsupportedLimits`. Select Wasmtime explicitly and check both errors and result status. Empty `AllowedPaths` grants no host directories.

```go
rt, err := wasm.NewCLIRuntime("wasmtime")
if err != nil { log.Fatal(err) }
sandbox := wasm.NewSandbox(wasm.SandboxConfig{
    Runtime:        rt,
    MaxMemory:      64 * 1024 * 1024,
    MaxDuration:    5 * time.Second,
    MaxOutputBytes: 1024 * 1024,
})
result, err := sandbox.Run(ctx, "tools/transform.wasm", []byte(`{"text":"hello"}`))
if err != nil { log.Fatal(err) }
if result.ExitCode != 0 || result.OutputTruncated {
    log.Fatalf("WASM exit=%d, output truncated=%v", result.ExitCode, result.OutputTruncated)
}
fmt.Println(string(result.Stdout))
```

Key properties:
- **Memory limit**: Bounds WASM linear memory; it does not cap all host/runtime process memory
- **Time-limited**: Execution timeout
- **CPU-limited**: Fuel (instruction count) budget
- **Portability**: Requires a compatible module, WASI imports, and installed runtime on the target
- **Network settings**: The CLI adapter does not pass flags enabling guest network access

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

reloader, _ := hotreload.NewReloader[AgentConfig](w, "config/agent.json",
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
defer pool.Close()

// Submit work items
for _, item := range workItems {
    result, _ := pool.Submit(ctx, item)
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

Each worker owns an agent instance. Dependencies captured by the factory may still be shared and must support concurrent use. Agent history also persists across jobs handled by the same worker.

## Deterministic Replay for CI/CD

Record model responses once, then replay them in tape order. Replay skips model API calls but does not validate prompt equality or intercept tool side effects. Use mock or isolated tools for offline CI.

### Recording

```go
recorder := replay.NewRecorder()
a := agent.NewUnifiedAgent("bot", "You are a test agent.", cm,
    agent.WithMiddlewares(recorder),
)
if _, err := a.Reply(ctx, "Summarize the Q3 report"); err != nil { log.Fatal(err) }
store, err := replay.NewFileStore("testdata")
if err != nil { log.Fatal(err) }
if err := store.Save(ctx, "q3_summary", recorder.Tape()); err != nil { log.Fatal(err) }
```

### Replaying in Tests

```go
package example

import (
    "context"
    "strings"
    "testing"

    "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/agent"
    "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/agenttest"
    "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/replay"
)

func TestQ3Summary(t *testing.T) {
    ctx := context.Background()
    store, err := replay.NewFileStore("testdata")
    if err != nil { t.Fatal(err) }
    tape, err := store.Load(ctx, "q3_summary")
    if err != nil { t.Fatal(err) }
    placeholder := agenttest.NewMockModel()
    replayer := replay.NewReplayer(tape)
    a := agent.NewUnifiedAgent("bot", "You are a test agent.", placeholder,
        agent.WithMiddlewares(replayer),
    )
    reply, err := a.Reply(ctx, "Summarize the Q3 report")
    if err != nil { t.Fatal(err) }
    text := reply.GetTextContent("\n")
    if text == nil || !strings.Contains(*text, "revenue") {
        t.Fatalf("unexpected reply: %v", text)
    }
    if len(placeholder.Calls()) != 0 { t.Fatal("replay called the placeholder model") }
}
```

The test uses a non-nil offline mock because `NewUnifiedAgent` rejects nil models. Only model responses are replayed; real tools, if configured, can still execute.

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
- [ ] Use `ReplyBudgetControlMiddleware` to cap token spending; use `CostTrackerMiddleware` with `WithMaxCostUSD` to stop subsequent calls after accounted cost reaches the threshold (single or concurrent calls can overshoot)
- [ ] Rotate API keys and use `model.SecretStr` to prevent key leakage in logs
- [ ] Put the Agent Service behind authentication (it has no built-in auth)
- [ ] Use Redis-backed storage and message bus for multi-instance deployments
- [ ] Configure `access` policies for multi-tenant resource sharing
- [ ] Enable `hotreload` to update agent configs without downtime
- [ ] Use `replay` tapes in CI to test agent behavior deterministically
- [ ] Configure `GuardrailMiddleware` for output content filtering
- [ ] Use `AgentPool` with appropriate worker counts for batch workloads

## See Also

- [Architecture](architecture.md) — Package structure and design
- [Go Runtime Features](go-exclusive.md) — Replay, Pool, Hot-reload, WASM, TCP Mesh, Bench
- [Examples](examples.md) — Runnable demos for all deployment patterns
