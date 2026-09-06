

<p align="center">
  <img
    src="https://img.alicdn.com/imgextra/i1/O1CN01nTg6w21NqT5qFKH1u_!!6000000001621-55-tps-550-550.svg"
    alt="AgentScope Logo"
    width="200"
  />
</p>

<h3 align="center">Crea Agentes de IA Listos para Producción en Go</h3>

<p align="center">
  <a href="https://github.com/agentscope-ai/agentscope">🐍 Python</a>
  &nbsp;|&nbsp;
  <a href="https://github.com/agentscope-ai/agentscope-java">☕ Java</a>
  &nbsp;|&nbsp;
  <a href="README.md">English</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/license-Apache--2.0-blue" alt="License" />
  <img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go" alt="Go 1.25+" />
  <a href="https://pkg.go.dev/github.com/alanfokco/agentscope-go/v2/pkg/agentscope"><img src="https://pkg.go.dev/badge/github.com/alanfokco/agentscope-go/v2/pkg/agentscope.svg" alt="Go Reference" /></a>
</p>

---

AgentScope Go es la implementación en Go del framework multi-agente para LLMs [AgentScope](https://github.com/agentscope-ai/agentscope). Proporciona APIs idiomáticas de Go: interfaces, `context.Context`, retorno explícito de `error`, opciones funcionales —, al tiempo que ofrece capacidades que van más allá del proyecto en Python.

---

## ¿Por qué agentscope-go?

| Propiedad | Qué significa para ti |
|----------|----------------------|
| **Despliegue como binario único** | Compila un ejecutable Go para la plataforma elegida. El enlace estático depende de CGO y de las dependencias; los backends opcionales como Wasmtime o Docker requieren su propio runtime. |
| **Ejecución concurrente** | Goroutines y canales permiten trabajo paralelo. `AgentPool` limita trabajadores y trabajos en cola; dimensiona memoria y concurrencia mediante pruebas de carga. |
| **Biblioteca integrable** | `go get` e importa en cualquier servicio Go existente. Sin procesos separados, sin sidecar, sin sobrecarga de IPC. Tu servidor HTTP, servicio gRPC o herramienta CLI gana capacidades de agente dentro del mismo proceso. |
| **Contratos explícitos** | Las interfaces detectan incompatibilidades de tipos al compilar. Los errores de ejecución, valores nil, problemas de concurrencia y límites de aislamiento todavía requieren validación; consulta [estabilidad y limitaciones](STABILITY.md). |

---

## Capacidades del runtime Go

Estas secciones describen capacidades de este repositorio Go; no constituyen una comparación entre versiones del proyecto Python.

### Estudio Web UI (`webui/`)

Interfaz web integrada para la interacción con agentes. Cero dependencias externas: la SPA se compila en el binario mediante `go:embed`. Soporta chat en streaming con bloques de pensamiento, visualización de llamadas a herramientas, confirmación humana en el bucle (HITL), gestión de sesiones y exploración de modelos.

```go
svc := service.New(cfg, cm, factory)
handler := svc.HandlerWithWebUI(service.WebUIConfig{Enable: true})
http.ListenAndServe(":8080", handler)
// Open http://localhost:8080 in your browser
```

### Reproducción Determinista (`replay/`)

Graba respuestas del modelo y reprodúcelas en el orden de la cinta sin llamar a su API. No se valida la igualdad de los prompts ni se reproducen los efectos de las herramientas: usa herramientas simuladas o aisladas. `NewUnifiedAgent` requiere un modelo no nil; el ejemplo usa `agenttest` de `pkg/agentscope/agenttest`.

```go
// cm is an initialized, non-nil ChatModel used for recording.
recorder := replay.NewRecorder()
a := agent.NewUnifiedAgent("bot", "...", cm, agent.WithMiddlewares(recorder))
if _, err := a.Reply(ctx, "plan a trip to Tokyo"); err != nil {
    log.Fatal(err)
}
store, err := replay.NewFileStore("testdata")
if err != nil { log.Fatal(err) }
if err := store.Save(ctx, "trip", recorder.Tape()); err != nil { log.Fatal(err) }

// Replay model responses with an offline, non-nil model placeholder.
tape, err := store.Load(ctx, "trip")
if err != nil { log.Fatal(err) }
replayer := replay.NewReplayer(tape)
placeholder := agenttest.NewMockModel()
replayed := agent.NewUnifiedAgent("bot", "...", placeholder, agent.WithMiddlewares(replayer))
if _, err := replayed.Reply(ctx, "plan a trip to Tokyo"); err != nil { log.Fatal(err) }
```

### Piscina de Agentes con Fan-out (`runtime/`)

Procesa N sesiones concurrentes con goroutines de trabajo acotadas y backpressure. Cada trabajador posee su propia instancia de agente: sin estado mutable compartido.

```go
pool := runtime.NewAgentPool(
    func() agent.Agent {
        return agent.NewUnifiedAgent("worker", "...", cm, agent.WithToolkit(tk))
    },
    runtime.Workers(16),
    runtime.QueueSize(256),
)
defer pool.Close()

resultCh, _ := pool.Submit(ctx, "Summarize this document...")
result := <-resultCh
fmt.Println(result.Output.GetTextContent("\n"))
```

### Configuración con Hot-Reload (`hotreload/`)

Actualizaciones de configuración sin tiempo de inactividad con genéricos tipados. Los cambios de archivo se detectan mediante polling; la nueva configuración se intercambia de forma atómica.

```go
type AgentCfg struct {
    Model       string  `json:"model"`
    Temperature float64 `json:"temperature"`
    MaxTokens   int     `json:"max_tokens"`
}

watcher := hotreload.NewWatcher(hotreload.WatcherConfig{PollInterval: 2 * time.Second})
reloader, _ := hotreload.NewReloader[AgentCfg](watcher, "config/agent.json",
    hotreload.WithOnChange(func(old, new_ *AgentCfg) {
        log.Printf("model changed: %s -> %s", old.Model, new_.Model)
    }),
)
watcher.Start(ctx)

// Always reads the latest config — no restart needed
cfg := reloader.Get()
```

### Sandbox WASM (`wasm/`)

Ejecuta módulos WASM con límites de combustible, memoria lineal, tiempo, directorios y captura de salida mediante Wasmtime. Wasmer y wasm3 pueden detectarse, pero la ejecución con los límites predeterminados devuelve `ErrUnsupportedLimits`. El inicio y la compatibilidad dependen del runtime instalado.

```go
rt, err := wasm.NewCLIRuntime("wasmtime")
if err != nil { log.Fatal(err) }
sandbox := wasm.NewSandbox(wasm.SandboxConfig{
    Runtime:        rt,
    MaxMemory:      64 * 1024 * 1024,
    MaxDuration:    5 * time.Second,
    MaxOutputBytes: 1024 * 1024,
})
result, err := sandbox.Run(ctx, "tools/transform.wasm", inputJSON)
if err != nil { log.Fatal(err) }
if result.ExitCode != 0 || result.OutputTruncated {
    log.Fatalf("WASM exit=%d, output truncated=%v", result.ExitCode, result.OutputTruncated)
}
fmt.Println(string(result.Stdout))
```

### Malla de Agentes por TCP (`a2a/grpc/`)

Comunicación TCP con JSON delimitado por líneas; este paquete no implementa el protocolo gRPC. La respuesta debe conservar el ID de la solicitud. En `Client.Stream`, solo recibir `StreamEnd` confirma éxito: la cancelación, desconexión o saturación del búfer puede cerrar el canal sin completar la respuesta.

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
server, err := grpc.NewServer("127.0.0.1:0")
if err != nil { log.Fatal(err) }
defer server.Close()
server.OnMessage(func(msg *grpc.Message) *grpc.Message {
    return &grpc.Message{ID: msg.ID, From: "router", To: msg.From, Payload: msg.Payload}
})
go func() {
    if err := server.Listen(ctx); err != nil { log.Print(err) }
}()

client, err := grpc.NewClient(server.Addr())
if err != nil { log.Fatal(err) }
defer client.Close()
resp, err := client.Send(ctx, &grpc.Message{
    ID: "request-1", From: "agent-alpha", To: "agent-beta", Method: "analyze",
    Payload: json.RawMessage(`{"task":"analyze"}`),
})
if err != nil { log.Fatal(err) }
fmt.Println(string(resp.Payload))
```

### Pruebas de Carga para Agentes (`bench/`)

Framework de pruebas de carga integrado con informes de latencia P50/P95/P99. Define escenarios con concurrencia, ramp-up y duración configurables.

```go
runner := bench.NewRunner()
report, _ := runner.Run(ctx, &bench.Scenario{
    Name:        "rag-query-load",
    Concurrency: 20,
    Duration:    30 * time.Second,
    Run: func(ctx context.Context, iter int) error {
        _, err := agent.Reply(ctx, queries[iter%len(queries)])
        return err
    },
})
fmt.Printf("P50=%v P95=%v P99=%v throughput=%.1f/s\n",
    report.Latencies.P50, report.Latencies.P95, report.Latencies.P99, report.Throughput)
```

---

## Conjunto Completo de Características

### 9 Proveedores de Modelos

Todos los proveedores soportan `Chat`, `ChatStream` (SSE), `CountTokens` y llamadas nativas a herramientas:

| Proveedor | Constructor | Modelos de Ejemplo |
|----------|-------------|----------------|
| **OpenAI** | `model.NewOpenAIChatModel` | gpt-4o, gpt-4.1, gpt-5.5, o3, o4-mini |
| **OpenAI Responses** | `model.NewOpenAIResponseModel` | gpt-4.1, o3 (API de Responses) |
| **Anthropic** | `model.NewAnthropicChatModel` | claude-opus-4-8, claude-sonnet-4-6 |
| **DashScope** | `model.NewDashScopeChatModel` | qwen3.5-plus, qwen3.7-max |
| **DeepSeek** | `model.NewDeepSeekChatModel` | deepseek-chat, deepseek-v4-pro |
| **Google Gemini** | `model.NewGeminiChatModel` | gemini-2.5-pro, gemini-3.1-pro |
| **Ollama** | `model.NewOllamaChatModel` | llama4, qwen3-14b (local) |
| **Moonshot** | `model.NewMoonshotChatModel` | kimi-k2.6, moonshot-v1-128k |
| **xAI** | `model.NewXAIChatModel` | grok-3, grok-4.3 |

78 fichas de modelo con tamaños de contexto, capacidades y estado se incluyen mediante `//go:embed`.

Características adicionales de modelos: `FallbackChatModel` (conmutación automática primario→respaldo), `ClientOptions` (timeout/headers/transporte HTTP personalizados), pensamiento extendido con tokens de presupuesto, streaming de subtítulos de audio (PCM→WAV).

### 8 Backends de Espacio de Trabajo

Entornos de ejecución aislados para sandboxing de herramientas:

| Backend | Paquete | Notas |
|---------|---------|-------|
| **Local** | `workspace/local.go` | Ejecución directa en sistema de archivos |
| **Docker** | `workspace/docker.go` | Aislamiento basado en contenedores |
| **E2B** | `workspace/e2b.go` | Sandbox en la nube (e2b.dev) |
| **Apple Container** | `workspace/applecontainer.go` | VM ligera nativa de macOS |
| **Bubblewrap** | `workspace/bubblewrap.go` | Sandbox de espacio de nombres de usuario en Linux (bwrap) |
| **Daytona** | `workspace/daytona.go` | API de espacios de trabajo Daytona |
| **OpenSandbox** | `workspace/opensandbox.go` | Entorno en la nube OpenSandbox |
| **Kubernetes** | `workspace/k8s.go` | Ejecución basada en Pods en clústeres K8s |

### 5 Almacenes Vectoriales para RAG

| Almacén | Archivo | Notas |
|-------|------|-------|
| **InMemory** | `rag/rag.go` | Cero dependencias, adecuado para corpora pequeños |
| **Qdrant** | `rag/qdrant_index.go` | DB vectorial de producción con filtrado |
| **Elasticsearch** | `rag/elasticsearch.go` | Búsqueda híbrida de texto completo + vectorial |
| **MongoDB** | `rag/mongodb.go` | Atlas Vector Search |
| **Milvus** | `rag/milvus.go` | DB vectorial de alto rendimiento |

### 5 Analizadores de Documentos

| Formato | Archivo | Notas |
|--------|------|-------|
| **Texto Plano** | `rag/parser/text.go` | Texto UTF-8 con segmentación configurable |
| **PDF** | `rag/parser/pdf.go` | Extracción de texto de documentos PDF |
| **Word** | `rag/parser/word.go` | Análisis de .docx |
| **Excel** | `rag/parser/excel.go` | Extracción de hojas .xlsx |
| **PowerPoint** | `rag/parser/ppt.go` | Extracción de texto de diapositivas .pptx |

### 4 Backends de Almacenamiento

| Backend | Archivo | Notas |
|---------|------|-------|
| **InMemory** | `storage/storage.go` | Rápido, efímero |
| **File** | `storage/full_storage.go` | Persistencia en archivos JSON |
| **Redis** | `storage/redis.go` | Distribuido, soporte TTL |
| **SQL** | `storage/sql.go` | PostgreSQL/MySQL/SQLite vía `database/sql` |

### Sistema de Hub

Registro unificado para componentes instalables:

| Componente | Descripción |
|-----------|-------------|
| **MCP Hub** | Explora, busca e instala servidores MCP desde un registro remoto |
| **Skill Hub** | Descubre e instala habilidades de agente reutilizables |
| **Registry** | Agregación multi-hub con búsqueda unificada entre fuentes |

### Control de Acceso (`access/`)

Compartición de recursos entre usuarios, grupos y organizaciones:

- 4 niveles de permiso: Ninguno, Lectura, Escritura, Admin
- 3 tipos de principal: Usuario, Grupo, Organización
- 4 tipos de recurso: Credencial, Agente, Base de Conocimiento, Sesión
- Verificador basado en políticas con atajo de propiedad
- `ListAccessible` para descubrimiento de recursos filtrado por permisos

### 7 Ganchos de Middleware

Arquitectura en cadena de cebolla: cada gancho envuelve al siguiente en la cadena:

| Gancho | Propósito |
|------|---------|
| `OnReply` | Envuelve todo el ciclo de vida de la respuesta (más externo) |
| `OnReasoning` | Envuelve cada paso de razonamiento en el bucle ReAct |
| `OnModelCall` | Envuelve cada llamada a la API del modelo |
| `OnActing` | Envuelve cada ejecución de herramienta |
| `OnSystemPrompt` | Transforma el prompt del sistema (modo pipeline) |
| `OnCompressContext` | Envuelve la compresión de contexto |
| `OnCheckPermission` | Wrapper de permisos; requiere integrar explícitamente `BuildCheckPermissionChain` |

Middlewares integrados: TracingMiddleware, TTSMiddleware, ReplyBudgetControlMiddleware, LongTermMemoryMiddleware, CostTrackerMiddleware, MetricsMiddleware.

### 3 Proveedores de TTS

| Proveedor | Características |
|----------|----------|
| **DashScope** | Estándar + streaming en tiempo real CosyVoice |
| **OpenAI** | API de TTS de OpenAI con salida WAV en streaming |
| **Gemini** | TTS de Google Gemini |

### Herramientas Integradas

Kit de herramientas de agente de código listo para producción:

- **Bash / Read / Write / Edit / Glob / Grep** — Sistema de archivos + shell completo con detección de inyección a nivel AST, protección de rutas peligrosas, reconocimiento de comandos de solo lectura
- **Gestión de Tareas** — `task_create`, `task_get`, `task_list`, `task_update` con seguimiento de dependencias bidireccional
- **Salida Estructurada** — `GenerateStructuredOutput` fuerza respuestas compatibles con JSON Schema mediante llamadas de herramienta sintéticas con reintentos automáticos
- **Memoria a Largo Plazo** — Middleware de memoria entre sesiones con 3 modos (estático, controlado por agente, ambos), respaldado por búsqueda de similitud vectorial o API REST de mem0

### Arquitectura del Agente

- **Bucle ReAct** — Razonamiento-acción autónomo con iteraciones máximas configurables
- **Interrupción Segura** — Pausa la ejecución en cualquier punto, preservando todo el contexto
- **Humano en el Bucle** — Inyecta correcciones vía sistema de eventos (`RequireUserConfirm` / `RequireExternalExecution`)
- **Motor de Permisos** — 5 modos de permiso con coincidencia de reglas por herramienta y verificaciones de seguridad inmunes a saltos
- **Compresión de Contexto** — Resumen estructurado automático cuando el contexto excede los umbrales

### Protocolos de Integración

| Protocolo | Descripción |
|----------|-------------|
| **MCP** | Cliente MCP completo (Stdio + HTTP/SSE) con descubrimiento automático de herramientas |
| **A2A HTTP** | Agente-a-Agente sobre HTTP vía `A2AAgent` + `HTTPClient` |
| **A2A gRPC/TCP** | Malla bidireccional de baja latencia (TCP + JSON delimitado por líneas) |
| **AG-UI** | Protocolo de servicio de agente para integración de frontend |
| **Agent Teams** | Coordinación Líder/Trabajador con proyección de eventos HITL entre sesiones |
| **Pipeline & MsgHub** | Combinadores secuenciales `Then`/`If` + enrutamiento de mensajes multi-agente |

### Observabilidad y Operaciones

- **Tracing** — `TracingMiddleware` con convenciones semánticas de OpenTelemetry, spans anidados
- **Métricas** — Interfaces `Counter`/`Histogram` con `InMemoryProvider` y `MetricsHook`
- **Seguimiento de Presupuesto** — Límites de turno/tokens/duración/concurrencia con `BudgetTracker`
- **Resiliencia** - Envolturas de breaker de circuito + limitador de tasa para `ChatModel`
- **Embedding** — 4 proveedores (OpenAI, DashScope, Gemini, Ollama) con procesamiento por lotes, caché, soporte multimodal
- **Multiplataforma** — Detección de shell con análisis de seguridad PowerShell/Cmd, soporte Windows

---

## Inicio Rápido

**Requisitos:** Go 1.25+

```bash
go get github.com/alanfokco/agentscope-go/v2/pkg/agentscope
```

```bash
export DASHSCOPE_API_KEY=sk-...   # or ANTHROPIC_API_KEY / OPENAI_API_KEY
go run ./examples/agent_v2
```

### Agente Mínimo con Llamadas a Herramientas

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"

    as "github.com/alanfokco/agentscope-go/v2/pkg/agentscope"
    "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/agent"
    "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
    "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
)

func main() {
    as.Init()

    cm, _ := model.NewDashScopeChatModel(model.DashScopeConfig{
        APIKey: "sk-...", Model: "qwen-plus",
    })

    weatherTool := tool.NewFunctionTool(
        "get_weather", "Get current weather for a city",
        json.RawMessage(`{
            "type": "object",
            "properties": {"location": {"type": "string"}},
            "required": ["location"]
        }`),
        func(ctx context.Context, input map[string]any) (any, error) {
            return map[string]any{"temp": "22°C", "condition": "sunny"}, nil
        },
    )

    a := agent.NewUnifiedAgent("assistant", "You are a weather bot.", cm,
        agent.WithToolkit(tool.NewToolkit(weatherTool)),
        agent.WithReactConfig(agent.ReactConfig{MaxIters: 5}),
    )

    reply, _ := a.Reply(context.Background(), "What's the weather in Shanghai?")
    if txt := reply.GetTextContent("\n"); txt != nil {
        fmt.Println(*txt)
    }
}
```

### Streaming

```go
ch, _ := a.ReplyStream(ctx, "Tell me a story.")
for evt := range ch {
    switch e := evt.(type) {
    case event.TextBlockDeltaEvent:
        fmt.Print(e.Delta)
    case event.ReplyEndEvent:
        fmt.Println()
    }
}
```

### Middleware Personalizado

```go
type TimingMiddleware struct { middleware.BaseMiddleware }

func (m *TimingMiddleware) OnModelCall(ctx context.Context, input *middleware.ModelCallInput, next middleware.ModelCallHandler) (*model.ChatResponse, error) {
    start := time.Now()
    resp, err := next(ctx, input)
    log.Printf("[%s] model call: %v", input.ModelName, time.Since(start))
    return resp, err
}

a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithMiddlewares(&TimingMiddleware{
        BaseMiddleware: middleware.BaseMiddleware{MiddlewareKey: "timing"},
    }),
)
```

---

## Arquitectura

```
pkg/agentscope/
├── agent/                  # Interfaz de Agente + UnifiedAgent, UserAgent, A2AAgent
├── model/                  # Interfaz ChatModel + 9 proveedores + 78 fichas de modelo
├── tool/                   # Interfaz de Herramienta + FunctionTool + 17 herramientas integradas + análisis de seguridad
├── message/                # Msg + ContentBlock (texto, pensamiento, tool_call, tool_result, datos, pista)
├── event/                  # 30 tipos de evento para el ciclo de vida en streaming
├── middleware/             # Cadena de cebolla de 7 ganchos + tracing, TTS, presupuesto, memoria, métricas, costo
├── formatter/              # Formateo de mensajes por proveedor (9 formateadores)
├── permission/             # 5 modos + Motor + Verificador + coincidencia de reglas
├── pipeline/               # Pipeline (Then/If) + MsgHub (enrutamiento multi-agente)
├── credential/             # 9 tipos de credenciales de proveedor + detección automática desde env
│
├── replay/                 # Grabación/reproducción determinista de llamadas a LLM
├── runtime/                # AgentPool, SessionEngine, AgentManager, BudgetTracker, Run
├── hotreload/              # Recargador de configuración genérico tipado con observación de archivos
├── wasm/                   # Sandbox WASM (límites mediante Wasmtime)
├── bench/                  # Framework de pruebas de carga con informes P50/P95/P99
├── a2a/                    # Tipos de protocolo A2A + cliente HTTP
├── a2a/grpc/               # Transporte TCP: malla de agentes bidireccional
│
├── hub/                    # MCP Hub + Skill Hub + Registro (agregación multi-hub)
├── access/                 # Compartición de recursos: usuarios/grupos/orgs con 4 niveles de permiso
├── workspace/              # 8 backends: Local, Docker, E2B, Apple, Bubblewrap, Daytona, OpenSandbox, K8s
├── rag/                    # Índice + KnowledgeBase + 5 almacenes vectoriales
├── rag/parser/             # 5 analizadores de documentos: Texto, PDF, Word, Excel, PPT
├── storage/                # 4 backends: InMemory, File, Redis, SQL
├── tts/                    # 3 proveedores: DashScope, OpenAI, Gemini
├── embedding/              # 4 proveedores + lotes + caché + multimodal
│
├── mcp/                    # Cliente MCP (Stdio + HTTP) + servidor MCP
├── team/                   # Equipos de agentes con coordinación líder/trabajador
├── service/                # Servicio HTTP de agente + SSE + protocolo AG-UI
├── webui/                  # Interfaz web integrada (SPA con go:embed)
├── tracing/                # Interfaz Tracer + OTel + LoggerTracer
├── metrics/                # Counter/Histogram + InMemoryProvider + MetricsHook
├── resilience/             # Breaker de circuito + limitador de tasa para ChatModel
├── loop/                   # Bucle de agente configurable (modelo → herramienta → iterar)
├── memory/                 # Memoria de conversación + compresión
├── messagebus/             # Pub/sub InMemory + Redis + registro
├── session/                # Almacén KV de sesiones (memoria + archivo JSON)
├── skill/                  # Sistema de habilidades reutilizables + registro SkillManager
├── prompt/                 # Ensamblaje de prompts del sistema componibles
├── schedule/               # InMemoryScheduler para tareas periódicas
├── realtime/               # Interfaz de streaming en tiempo real
├── sandbox/                # Políticas de ejecución (Allow/Deny/AskUser)
├── platform/               # Detección de shell multiplataforma + seguridad
├── logging/                # Manejadores de registro estructurado
├── protocol/               # LoopState, ApprovalPolicy, PermissionProfile
├── errors/                 # Jerarquía de errores tipados (Retriable, Throttled, PermissionDenied)
├── config/                 # Carga de configuración
├── app/                    # Bootstrap de aplicación
├── tune/                   # Utilidades de ajuste de modelos
├── types/                  # Definiciones de tipos compartidos
├── agenttest/              # Ayudas de prueba y mocks
├── exception/              # Manejo de excepciones
└── internal/               # fsutil (escrituras atómicas), httpsec (protección SSRF), httpx (HTTP+SSE), jsonx (reparación)
```

---

## Ejemplos

54 ejemplos en `examples/`. Ejecuta cualquiera con `go run ./examples/<nombre>`.

| Ejemplo | Descripción |
|---------|-------------|
| **Agent Basics** | |
| `simple` | Agente mínimo + llamada de chat única |
| `agent_v2` | UnifiedAgent con llamadas nativas a herramientas de API |
| `streaming` | Streaming en tiempo real vía `ReplyStream` + canal de eventos |
| `react_tool` | UnifiedAgent con FunctionTool personalizado |
| `react_builtin_tools` | UnifiedAgent con kit de herramientas integrado mejorado (bash, read, write, edit, glob, grep) |
| **Model API** | |
| `model_call` | API cruda del modelo: streaming + llamadas a herramientas de dos rondas + salida estructurada |
| `structured_output` | Fuerza salida compatible con JSON Schema vía `GenerateStructuredOutput` |
| `multi_provider` | Consultas de fichas de modelo + conmutación entre 9 proveedores |
| `multimodal` | Entrada de imagen vía URL y `DataBlock` Base64 |
| `multiagent` | Conversación multi-agente con resumen del moderador |
| `multiagent_multimodal` | Multi-agente + entrada de imagen compartida |
| `openai_response` | API de Responses de OpenAI (llamadas + herramientas + salida estructurada) |
| **Infrastructure** | |
| `middleware` | Middleware de registro personalizado (ganchos de llamada a modelo + ejecución de herramienta) |
| `permission` | Motor de permisos: modos Explore / Default / Bypass |
| `tracing` | Tracing estilo OpenTelemetry con spans anidados |
| `agent_loop` | Bucle de agente v3 con MetricsHook e InMemoryProvider |
| `embedding` | Incrustación de texto + matriz de similitud coseno |
| `long_term_memory` | Middleware de memoria entre sesiones (3 modos) |
| `rag_react` | RAG con índice en memoria + base de conocimiento |
| **Multi-Agent & Orchestration** | |
| `pipeline_multi_agent` | Orquestación Pipeline + MsgHub |
| `agent_team` | Equipo Líder/Trabajador con enrutamiento de mensajes |
| `mcp` | Cliente MCP: descubrimiento de herramientas + ejecución remota |
| `a2a_http` | Agente-a-Agente sobre HTTP |
| **Runtime Go** | |
| `replay` | Graba llamadas a LLM, reproduce en CI sin costos de API |
| `agent_pool` | Piscina de agentes con fan-out y backpressure |
| `hotreload` | Actualizaciones de configuración sin downtime con `Reloader[T]` tipado |
| `wasm_sandbox` | Sandbox de herramientas WASM con límites de memoria/tiempo |
| `grpc_a2a` | Malla de agentes TCP con streaming bidireccional |
| `bench` | Pruebas de carga de agentes con latencia P50/P95/P99 |
| `hub_install` | Explora e instala servidores MCP/habilidades desde el hub |
| `access_control` | Compartición de recursos entre usuarios/grupos/orgs |
| `document_parser` | Analiza PDF/Word/Excel/PPT en fragmentos para RAG |
| **Deployment** | |
| `agent_service` | Servicio HTTP de Agente (REST + streaming SSE) |
| `webui` | Estudio Web UI con chat en streaming, visualización de herramientas, HITL |
| `scheduled_task` | Programación de tareas únicas y recurrentes |
| `realtime_echo` | Demo de interfaz de streaming en tiempo real |
| **Edge & IoT** | |
| `edge_offline` | Modelo local+nube con detección de conectividad |
| `edge_sensor` | Middleware de sensor con filtrado de lecturas |
| `edge_serial_robot` | Control de robot vía puerto serie |
| `edge_fleet` | Flota de agentes edge con MQTT PubSub |
| **Evaluación y Seguridad** | |
| `audit_logging` | Política de sandbox + registro de auditoría estructurado |
| `eval_harness` | Evaluación de agentes basada en replay con scorers |
| `guardrail` | Filtrado de contenido de salida con block/redact/warn |
| `spend_cap` | Límite de gasto USD/CNY con CostTrackerMiddleware |
| **Multi-Agent** | |
| `werewolves` | Juego Hombres Lobo multi-agente con roles |
| **Tracing** | |
| `tracing_otlp` | Patrón de configuración OTLP (sin dependencia OTel SDK) |
| **Kubernetes** | |
| `k8s_workspace` | Sandbox K8s + herramientas de cluster solo lectura |

---

## Relación con Python

Este repositorio documenta sus propias API y limitaciones. Las comparaciones con Python requieren versiones o commits explícitos; no se mantiene aquí una tabla de ausencias de funciones sin versiones.

## Documentación

La documentación detallada está disponible en el directorio [`docs/`](docs/):

- [Estabilidad y límites pendientes](STABILITY.md) — Garantías de API y trabajo de endurecimiento abierto
- [Endurecimiento de ejecución y sesiones](docs/adversarial-hardening.md) — Comportamiento tras los PR #4 y #5
- [Primeros Pasos](docs/getting-started.md) — Instalación, primer agente, configuración del entorno
- [Arquitectura](docs/architecture.md) — Estructura de paquetes, conceptos centrales, flujo de datos
- [Proveedores de Modelos](docs/model-providers.md) — Configura 9 proveedores de LLM con ejemplos
- [Herramientas](docs/tools.md) — Herramientas integradas, funciones personalizadas, permisos
- [Middleware](docs/middleware.md) — Sistema de 7 ganchos, tracing, presupuesto, memoria
- [Ejemplos](docs/examples.md) — Catálogo completo de 54 ejemplos ejecutables
- [Despliegue](docs/deployment.md) — Servicio HTTP, sandboxing, checklist de producción

## Contribuir

¡Agradecemos las contribuciones! Por favor, consulta [CONTRIBUTING.md](./CONTRIBUTING.md) para las directrices.

## Licencia

Apache License 2.0 — consulta [LICENSE](./LICENSE) para más detalles.

## Publicaciones

Si encuentras AgentScope útil, por favor cita nuestros artículos:

- [AgentScope: A Flexible yet Robust Multi-Agent Platform](https://arxiv.org/abs/2402.14034)
