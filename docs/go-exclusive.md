# Go-Exclusive Features

These six features are unique to the Go implementation of AgentScope. They leverage Go's type system, concurrency primitives, and compilation model to provide capabilities that have no equivalent in the Python version.

## 1. Deterministic Replay

**Package:** `pkg/agentscope/replay`

Record all model call request/response pairs during an agent session, then replay them deterministically in CI/CD without API keys or network access. The replay middleware intercepts `OnModelCall` and either records responses (record mode) or returns pre-recorded responses (replay mode).

### Core Types

- **`Tape`** — a JSON-serializable sequence of `Entry` records with version metadata
- **`Entry`** — one recorded model call: agent name, model name, messages, tools, response, error, timestamp, duration
- **`Middleware`** — implements `middleware.Middleware` with `OnModelCall` hook

### Recording

```go
package main

import (
    "context"
    "encoding/json"
    "os"

    "github.com/alanfokco/agentscope-go/pkg/agentscope/agent"
    "github.com/alanfokco/agentscope-go/pkg/agentscope/model"
    "github.com/alanfokco/agentscope-go/pkg/agentscope/replay"
)

func main() {
    cm, _ := model.NewDashScopeChatModel(&model.DashScopeConfig{
        APIKey: os.Getenv("DASHSCOPE_API_KEY"),
        Model:  "qwen-plus",
    })

    // Create a recorder middleware
    recorder := replay.NewRecorder()

    a := agent.NewUnifiedAgent("analyst", "You are a data analyst.", cm,
        agent.WithMiddlewares(recorder),
    )

    // Run the agent — all model calls are captured
    a.Reply(context.Background(), "Analyze the Q3 revenue trends")

    // Save the tape to a file
    data, _ := json.MarshalIndent(recorder.Tape(), "", "  ")
    os.WriteFile("testdata/q3_analysis.tape.json", data, 0644)
}
```

### Replaying in Tests

```go
func TestQ3Analysis(t *testing.T) {
    // Load the pre-recorded tape
    data, _ := os.ReadFile("testdata/q3_analysis.tape.json")
    var tape replay.Tape
    json.Unmarshal(data, &tape)

    // Create a replayer — no model needed, no API key needed
    replayer := replay.NewReplayer(&tape)
    a := agent.NewUnifiedAgent("analyst", "You are a data analyst.", nil,
        agent.WithMiddlewares(replayer),
    )

    reply, err := a.Reply(context.Background(), "Analyze the Q3 revenue trends")
    require.NoError(t, err)

    text := reply.GetTextContent("\n")
    assert.NotNil(t, text)
    assert.Contains(t, *text, "revenue")
}
```

### Properties

- **No API keys in CI**: Replay mode never calls the real model
- **No network access**: Fully offline, deterministic
- **Multi-turn**: Records entire multi-step ReAct conversations
- **JSON format**: Tapes are human-readable, diffable, and versionable in Git

---

## 2. Fan-out Agent Pool

**Package:** `pkg/agentscope/runtime` (type `AgentPool`)

A work-queue that dispatches inputs to a fixed set of worker goroutines, each owning a fresh agent instance created from a factory function. Ideal for batch processing, data pipelines, and high-throughput scenarios.

### Code Example

```go
package main

import (
    "context"
    "fmt"
    "sync"

    "github.com/alanfokco/agentscope-go/pkg/agentscope/agent"
    "github.com/alanfokco/agentscope-go/pkg/agentscope/model"
    "github.com/alanfokco/agentscope-go/pkg/agentscope/runtime"
)

func main() {
    cm, _ := model.NewDashScopeChatModel(&model.DashScopeConfig{
        APIKey: "sk-...",
        Model:  "qwen-plus",
    })

    // Create a pool with 8 worker goroutines
    pool := runtime.NewAgentPool(
        func() agent.Agent {
            return agent.NewUnifiedAgent("classifier", "Classify the sentiment.", cm)
        },
        runtime.Workers(8),
        runtime.QueueSize(100),
    )

    ctx := context.Background()
    pool.Start(ctx)
    defer pool.Close()

    // Fan out 100 classification tasks
    inputs := []string{"Great product!", "Terrible service.", "It's okay I guess."}

    var wg sync.WaitGroup
    for _, input := range inputs {
        wg.Add(1)
        resultCh := pool.Submit(ctx, input)
        go func(ch <-chan runtime.PoolResult) {
            defer wg.Done()
            res := <-ch
            if res.Err == nil {
                fmt.Printf("[%s] → %s (took %v)\n",
                    res.Input, *res.Output.GetTextContent("\n"), res.Duration)
            }
        }(resultCh)
    }
    wg.Wait()
}
```

### Properties

- **No shared state**: Each worker owns its own agent instance
- **Bounded concurrency**: Worker count and queue size are configurable
- **Back-pressure**: Submitters block when the queue is full
- **Graceful shutdown**: `Close()` drains pending work

---

## 3. Hot-Reload Config

**Package:** `pkg/agentscope/hotreload`

Watch configuration files for changes and apply updates at runtime without restarting the process. Built on polling (not inotify) for cross-platform compatibility.

### Code Example

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/alanfokco/agentscope-go/pkg/agentscope/hotreload"
)

type AgentConfig struct {
    SystemPrompt string   `json:"system_prompt"`
    MaxIters     int      `json:"max_iters"`
    Model        string   `json:"model"`
    Temperature  float64  `json:"temperature"`
}

func main() {
    ctx := context.Background()

    // Create a file watcher with 2-second polling
    w := hotreload.NewWatcher(hotreload.WatcherConfig{
        PollInterval: 2 * time.Second,
    })

    // Create a typed config reloader
    reloader, _ := hotreload.NewReloader[AgentConfig]("config/agent.json", w,
        hotreload.WithOnChange(func(old, new_ *AgentConfig) {
            fmt.Printf("Config updated: model %s → %s\n", old.Model, new_.Model)
        }),
    )

    w.Start(ctx)
    defer w.Stop()

    // Use the config — reads are lock-free via atomic pointer
    for {
        cfg := reloader.Get()
        fmt.Printf("Current model: %s, prompt: %s\n", cfg.Model, cfg.SystemPrompt)
        time.Sleep(5 * time.Second)
    }
}
```

### Properties

- **Generics**: `Reloader[T]` works with any config struct
- **Lock-free reads**: Uses `atomic.Pointer[T]` for zero-contention access
- **Custom parsers**: Default is JSON, override with `WithParser()` for YAML/TOML
- **Multi-file**: Watch multiple files with different handlers
- **Cross-platform**: Polling-based, works on Linux, macOS, Windows

---

## 4. WASM Sandbox

**Package:** `pkg/agentscope/wasm`

Execute WebAssembly modules in a strict sandbox with memory, time, and instruction-count limits. Uses CLI runtimes (`wasmtime`, `wasmer`, `wasm3`) via auto-discovery.

### Code Example

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/alanfokco/agentscope-go/pkg/agentscope/wasm"
)

func main() {
    // Auto-discover available WASM runtime
    rt, _ := wasm.NewCLIRuntime("")  // tries wasmtime, wasm3, wasmer in order

    sandbox := wasm.NewSandbox(wasm.SandboxConfig{
        Runtime:     rt,
        MaxMemory:   32 * 1024 * 1024, // 32MB heap limit
        MaxDuration: 5 * time.Second,   // 5s timeout
        MaxFuel:     500_000,           // instruction count budget
    })

    // Execute a WASM module
    result, err := sandbox.Run(context.Background(),
        "plugins/transform.wasm",
        []byte(`{"text": "hello world"}`),
    )
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }

    fmt.Printf("Output: %s\n", result.Stdout)
    fmt.Printf("Exit code: %d, Duration: %v\n", result.ExitCode, result.Duration)
}
```

### Properties

- **Memory-limited**: Hard cap prevents heap exhaustion
- **Time-limited**: Kills runaway modules
- **CPU-limited**: Fuel budget (wasmtime) prevents infinite loops
- **No network**: Modules cannot access the network by default
- **Portable**: Same `.wasm` binary runs on any OS/architecture
- **No container runtime**: Lighter than Docker, no daemon needed

---

## 5. TCP Agent Mesh

**Package:** `pkg/agentscope/a2a/grpc`

Bidirectional agent-to-agent communication over TCP using newline-delimited JSON messages. Designed for building distributed agent networks where agents on different machines communicate directly.

### Code Example

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    mesh "github.com/alanfokco/agentscope-go/pkg/agentscope/a2a/grpc"
)

func main() {
    ctx := context.Background()

    // Start a server
    server, _ := mesh.NewServer(":9090")
    server.OnMessage(func(msg *mesh.Message) *mesh.Message {
        fmt.Printf("Received from %s: %s\n", msg.From, string(msg.Payload))
        return &mesh.Message{
            ID:      "resp-1",
            From:    "server-agent",
            To:      msg.From,
            Method:  "reply",
            Payload: json.RawMessage(`{"answer": "42"}`),
        }
    })
    go server.Listen(ctx)
    defer server.Close()

    // Connect a client
    time.Sleep(100 * time.Millisecond) // wait for server
    client, _ := mesh.NewClient(server.Addr())
    defer client.Close()

    // Send a message
    client.Send(ctx, &mesh.Message{
        ID:      "req-1",
        From:    "client-agent",
        To:      "server-agent",
        Method:  "ask",
        Payload: json.RawMessage(`{"question": "meaning of life"}`),
    })

    // Receive the response
    resp, _ := client.Receive(ctx)
    fmt.Printf("Response: %s\n", string(resp.Payload))
}
```

### Properties

- **Simple protocol**: Newline-delimited JSON over TCP
- **Streaming support**: `IsStream`/`StreamEnd` flags for multi-message responses
- **Bidirectional**: Both sides can send and receive
- **1MB max message**: Configurable buffer size
- **No HTTP overhead**: Direct TCP for low-latency agent communication
- **Complementary to A2A HTTP**: Use TCP mesh for internal clusters, HTTP A2A for cross-network

---

## 6. Agent Load Testing

**Package:** `pkg/agentscope/bench`

Define load test scenarios with configurable concurrency, duration, iterations, and ramp-up. The runner produces reports with throughput, latency percentiles, and error breakdowns.

### Code Example

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/alanfokco/agentscope-go/pkg/agentscope/agent"
    "github.com/alanfokco/agentscope-go/pkg/agentscope/bench"
    "github.com/alanfokco/agentscope-go/pkg/agentscope/model"
)

func main() {
    cm, _ := model.NewDashScopeChatModel(&model.DashScopeConfig{
        APIKey: "sk-...",
        Model:  "qwen-plus",
    })

    runner := bench.NewRunner()
    report, _ := runner.Run(context.Background(), &bench.Scenario{
        Name:           "simple-qa",
        Concurrency:    10,
        Duration:       60 * time.Second,
        RampUpDuration: 10 * time.Second,
        Run: func(ctx context.Context, iteration int) error {
            a := agent.NewUnifiedAgent("bot", "Answer briefly.", cm)
            _, err := a.Reply(ctx, fmt.Sprintf("Question %d: What is Go?", iteration))
            return err
        },
    })

    fmt.Printf("Scenario: %s\n", report.Scenario)
    fmt.Printf("Throughput: %.2f iter/s\n", report.Throughput)
    fmt.Printf("Latency p50=%v p95=%v p99=%v\n",
        report.Latencies.P50, report.Latencies.P95, report.Latencies.P99)
    fmt.Printf("Success: %d, Failures: %d\n", report.Successes, report.Failures)
}
```

### Report Fields

| Field | Type | Description |
|-------|------|-------------|
| `Throughput` | `float64` | Iterations per second |
| `Latencies.P50` | `time.Duration` | Median latency |
| `Latencies.P95` | `time.Duration` | 95th percentile latency |
| `Latencies.P99` | `time.Duration` | 99th percentile latency |
| `Successes` | `int64` | Number of successful iterations |
| `Failures` | `int64` | Number of failed iterations |
| `Errors` | `map[string]int64` | Error message to count breakdown |

### Properties

- **Duration-based or iteration-based**: Set `Duration`, `Iterations`, or both
- **Ramp-up**: Gradually increase concurrency over `RampUpDuration`
- **Setup/Teardown**: Optional hooks for test fixture management
- **Concurrent-safe**: Atomic counters, no shared mutable state

---

## Quick Reference

| Feature | Package | Key Type | Primary Use Case |
|---------|---------|----------|------------------|
| Deterministic Replay | `replay` | `Middleware`, `Tape` | CI/CD testing without API keys |
| Fan-out Agent Pool | `runtime` | `AgentPool` | Batch processing, high throughput |
| Hot-Reload Config | `hotreload` | `Watcher`, `Reloader[T]` | Live config updates without restart |
| WASM Sandbox | `wasm` | `Sandbox`, `CLIRuntime` | Untrusted code execution |
| TCP Agent Mesh | `a2a/grpc` | `Server`, `Client`, `Transport` | Distributed agent networks |
| Agent Load Testing | `bench` | `Runner`, `Scenario`, `Report` | Performance benchmarking |

## See Also

- [Architecture](architecture.md) — Package structure for all Go-exclusive packages
- [Deployment](deployment.md) — Production deployment with pools, replay, and hot-reload
- [Middleware](middleware.md) — Replay middleware hook details
- [Examples](examples.md) — Runnable demos for each feature
