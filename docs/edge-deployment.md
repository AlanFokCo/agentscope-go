# Edge Deployment Guide

This guide covers deploying agentscope-go on edge devices (Jetson Nano/Orin, Raspberry Pi, RISC-V boards) for IoT and embedded AI applications.

## Cross-Compilation

agentscope-go compiles to a single static binary with zero runtime dependencies. No CGO is required for any edge feature.

### ARM64 (Jetson, RPi 4/5, Apple Silicon)

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o agent ./examples/edge_offline/
```

### ARM (RPi 3, older boards)

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags="-s -w" -o agent ./examples/edge_offline/
```

### RISC-V 64-bit

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=riscv64 go build -ldflags="-s -w" -o agent ./examples/edge_offline/
```

### MIPS (routers, industrial gateways)

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=mips64le go build -ldflags="-s -w" -o agent ./examples/edge_offline/
```

## Binary Size Optimization

Target: stripped edge binary under 18MB.

| Technique | Savings |
|-----------|---------|
| `-ldflags="-s -w"` | ~30% (removes debug/symbol info) |
| Avoid importing `prometheus`, `otel`, `qdrant` | 5-8MB |
| Use `//go:build` tags for optional features | Variable |
| UPX compression (optional) | ~60% additional |

### Minimal import strategy

Edge examples deliberately avoid importing heavy packages. The MQTT adapter is behind a build tag (`//go:build mqtt`) so non-MQTT builds pay no size cost.

## Ollama + agentscope-go on Edge

### Jetson Nano/Orin (ARM64)

```bash
# Install Ollama
curl -fsSL https://ollama.com/install.sh | sh

# Pull a small model suitable for edge
ollama pull qwen2.5:0.5b    # ~400MB, runs well on 4GB RAM
ollama pull phi3:mini        # ~2.3GB, needs 8GB RAM

# Run the edge agent
./agent
```

### Raspberry Pi 4/5 (ARM64)

```bash
# Install Ollama (ARM64 builds available)
curl -fsSL https://ollama.com/install.sh | sh

# Use the smallest available model
ollama pull tinyllama         # ~637MB
ollama pull qwen2.5:0.5b     # ~400MB

# Deploy
scp agent pi@raspberrypi:/usr/local/bin/
ssh pi@raspberrypi '/usr/local/bin/agent'
```

## ConnectivityAwareModel

The `ConnectivityAwareModel` automatically routes between local and cloud models:

```go
import "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"

local, _ := model.NewOllamaChatModel(model.OllamaConfig{Model: "qwen2.5:0.5b"})
cloud, _ := model.NewOpenAIChatModel(model.OpenAIConfig{APIKey: key, Model: "gpt-4o-mini"})

cam := model.NewConnectivityAwareModel(local, cloud,
    model.WithFailureThreshold(3),     // 3 failures before switching
    model.WithRecoveryTimeout(30*time.Second), // probe cloud every 30s
)

// Use like any ChatModel — routing is automatic
resp, err := cam.Chat(ctx, msgs)
fmt.Println(cam.ActiveModel()) // "cloud" or "local"
```

### Behavior

| Circuit State | Behavior |
|--------------|----------|
| Closed | All calls go to cloud |
| Open | All calls go to local (no cloud attempts) |
| Half-Open | Single probe to cloud; if success, close circuit |

## Network Considerations

### Offline-First Design

- Agent logic runs entirely on-device
- Cloud model is an optimization, not a requirement
- Sensor data collection continues regardless of connectivity
- MQTT QoS 1/2 ensures message delivery when reconnected

### Bandwidth-Constrained Environments

- Use small local models (0.5B-3B parameters) for most queries
- Reserve cloud calls for complex reasoning
- Set aggressive failure thresholds (2-3) to minimize wasted bandwidth
- Consider MQTT QoS 0 for high-frequency sensor data

## Systemd Service

```ini
[Unit]
Description=AgentScope Edge Agent
After=network.target ollama.service
Wants=ollama.service

[Service]
Type=simple
ExecStart=/usr/local/bin/agent
Restart=always
RestartSec=5
Environment="OLLAMA_HOST=http://localhost:11434"

[Install]
WantedBy=multi-user.target
```

## Hardware Requirements

| Device | RAM | Storage | Models |
|--------|-----|---------|--------|
| Jetson Orin Nano | 8GB | 32GB+ | Up to 7B |
| Raspberry Pi 5 | 8GB | 32GB+ | Up to 3B |
| Raspberry Pi 4 | 4GB | 16GB+ | Up to 1B |
| RISC-V (StarFive) | 4GB | 16GB+ | Up to 1B |
