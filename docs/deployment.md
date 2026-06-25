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

```go
ws, _ := workspace.NewDockerWorkspace(&workspace.DockerConfig{
    Image:      "python:3.11-slim",
    WorkingDir: "/workspace",
})
backend := workspace.NewDockerBackend(ws)
bashTool := tool.BashToolWithBackend(backend)
```

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
- [ ] Use `DockerWorkspace` or `E2BWorkspace` for tool execution
- [ ] Configure `ClientOptions.Timeout` for your expected response times
- [ ] Set up `TracingMiddleware` with OpenTelemetry exporter
- [ ] Use `ReplyBudgetControlMiddleware` to cap token spending
- [ ] Rotate API keys and use `model.SecretStr` to prevent key leakage in logs
- [ ] Put the Agent Service behind authentication (it has no built-in auth)
- [ ] Use Redis-backed storage and message bus for multi-instance deployments
