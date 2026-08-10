# Tools

## Overview

Tools give agents the ability to execute actions. The `Tool` interface embeds `permission.Checker` for fine-grained access control.

## Built-in Tools

| Tool | Description | Safety |
|------|-------------|--------|
| `Bash` | Execute shell commands | AST-level injection detection, dangerous path protection, read-only command recognition |
| `Read` | Read files with line range support | Path validation, line truncation (>2000 chars) |
| `Write` | Create/overwrite files | Generates unified diff in response metadata |
| `Edit` | Search/replace in files | Generates unified diff in response metadata |
| `Glob` | File pattern matching | Read-only |
| `Grep` | Text search with regex | Read-only |
| `ResetTools` | Activate/deactivate tool groups | Meta-tool |
| `TaskCreate` | Create tasks with dependencies | Bidirectional blocks/blockedBy |
| `TaskGet` | Get task details | Read-only |
| `TaskList` | List all tasks | Read-only |
| `TaskUpdate` | Update task status/fields | Dependency tracking |

Use `tool.NewEnhancedToolkit()` to get all built-in tools, or select individually:

```go
tk := tool.NewToolkit(tool.BashTool(), tool.ReadTool(), tool.WriteTool())
```

### Bash Tool Options

```go
tool.BashTool(
    tool.WithCwd("/path/to/workdir"),  // set working directory
)
```

## Custom Function Tools

Wrap any Go function as a tool:

```go
weatherTool := tool.NewFunctionTool(
    "get_weather",
    "Get current weather for a city",
    json.RawMessage(`{
        "type": "object",
        "properties": {
            "city": {"type": "string", "description": "City name"}
        },
        "required": ["city"]
    }`),
    func(ctx context.Context, input map[string]any) (any, error) {
        city, _ := input["city"].(string)
        return map[string]any{"city": city, "temp": "22°C"}, nil
    },
)
```

## Tool Groups

Organize tools into activatable groups:

```go
tk := tool.NewToolkit(weatherTool, searchTool, calcTool)
tk.AddGroup("research", searchTool, calcTool)
tk.ActivateGroup("research")   // only research tools available
tk.DeactivateGroup("research")  // restore defaults
```

The `ResetTools` meta-tool lets agents manage groups themselves.

## MCP Tools

Discover and use remote tools via Model Context Protocol:

```go
client, _ := mcp.NewHttpClient(ctx, &mcp.HttpConfig{URL: "http://mcp-server:8080"})
mcpToolkit, _ := mcp.NewMCPToolkit(ctx, client)
merged := mcp.MergeToolkits(mcpToolkit, localToolkit)
```

## Document Parsers (RAG)

The `rag/parser` package converts common file formats into `rag.Document` slices ready for indexing. Each parser supports configurable text chunking with overlap.

### Supported Formats

| Parser | Extensions | Description |
|--------|-----------|-------------|
| `TextParser` | `.txt`, `.md`, `.csv`, `.log` | Plain text with configurable chunking |
| `PDFParser` | `.pdf` | Stream-based text extraction (FlateDecode + plain text) |
| `WordParser` | `.docx` | XML-based text extraction from Word documents |
| `ExcelParser` | `.xlsx` | Row-based text extraction from spreadsheets |
| `PPTParser` | `.pptx` | Slide text extraction from PowerPoint |

### Usage

```go
import "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/rag/parser"

// Parse a PDF into document chunks
p := &parser.PDFParser{Cfg: parser.DefaultChunkConfig()}
f, _ := os.Open("report.pdf")
docs, _ := p.Parse(ctx, f, "report.pdf")

// Each doc has Content (text) and Meta (source, page, etc.)
for _, doc := range docs {
    fmt.Printf("Chunk: %s... (source: %s)\n", doc.Content[:80], doc.Meta["source"])
}
```

### Chunk Configuration

Control how text is split into chunks:

```go
cfg := parser.ChunkConfig{
    MaxChunkSize: 1000,  // max characters per chunk
    Overlap:      200,   // overlap between consecutive chunks
}

p := &parser.TextParser{Cfg: cfg}
```

### Integration with RAG Pipeline

Parse documents and add to a knowledge base:

```go
// Parse
p := &parser.WordParser{Cfg: parser.DefaultChunkConfig()}
docs, _ := p.Parse(ctx, file, "manual.docx")

// Index
index := rag.NewInMemoryIndex()
index.AddDocuments(ctx, docs)

// Query
results, _ := index.Query(ctx, "installation steps", 5)
```

## Vector Store Backends

The `rag` package supports multiple vector store backends for document retrieval:

| Backend | Description | Use Case |
|---------|-------------|----------|
| `InMemoryIndex` | Simple linear scan, no external dependencies | Prototyping, small datasets |
| `QdrantIndex` | Qdrant vector database backend | Production, large-scale retrieval |
| `QdrantTextIndex` | Auto-embeds text then stores in Qdrant | Production with automatic embedding |

### InMemoryIndex

```go
index := rag.NewInMemoryIndex()
index.AddDocuments(ctx, docs)
results, _ := index.Query(ctx, "search query", 10)
```

### QdrantIndex

```go
// Requires a *qdrant.Client from github.com/qdrant/go-client/qdrant
qdrantClient, _ := qdrant.NewClient(&qdrant.Config{Host: "localhost", Port: 6334})
index, _ := rag.NewQdrantIndex(rag.QdrantConfig{
    Client:     qdrantClient,
    Collection: "my-docs",
})
```

### QdrantTextIndex

Combines embedding generation with Qdrant storage — no need to pre-compute vectors:

```go
qdrantClient, _ := qdrant.NewClient(&qdrant.Config{Host: "localhost", Port: 6334})
embedder, _ := embedding.NewOpenAIEmbeddingModel(...)
index, _ := rag.NewQdrantTextIndex(rag.QdrantTextConfig{
    Client:     qdrantClient,
    Collection: "my-docs",
    Embedder:   embedder,
})
// AddDocuments automatically embeds text before storing
index.AddDocuments(ctx, docs)
```

### Reranked Retrieval

Improve retrieval precision by wrapping an Index with a Reranker:

```go
rerankedIdx := rag.NewRerankedIndex(baseIndex, myReranker, 3)
results, _ := rerankedIdx.Query(ctx, "search query", 5)
```

## WASM Sandbox

Execute untrusted code as WebAssembly modules. The `wasm` package provides a `Sandbox` that enforces memory, time, and instruction-count limits.

```go
rt, _ := wasm.NewCLIRuntime("")  // auto-discover wasmtime/wasmer/wasm3
sandbox := wasm.NewSandbox(wasm.SandboxConfig{
    Runtime:     rt,
    MaxMemory:   64 * 1024 * 1024,
    MaxDuration: 10 * time.Second,
    MaxFuel:     1_000_000,
})

result, _ := sandbox.Run(ctx, "plugin.wasm", inputData)
```

This is complementary to workspace sandboxing (Docker, K8s, etc.) — WASM provides lighter-weight isolation without requiring container infrastructure. See [Deployment](deployment.md) for more sandbox options.

## Permission System

Every tool execution goes through the permission engine:

| Mode | Behavior |
|------|----------|
| `Default` | Requires explicit allow rules or user confirmation |
| `AcceptEdits` | Allows file modifications, asks for shell commands |
| `Explore` | Read-only operations only |
| `Bypass` | Allows everything (for sandboxed environments) |
| `DontAsk` | Denies anything that would normally ask |

```go
permCtx := permission.NewContext(permission.ModeAcceptEdits)
a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithPermissionContext(permCtx),
)
```

## Tool-level Middleware

Attach middleware to individual tools:

```go
type AuditMiddleware struct{}

func (m *AuditMiddleware) Wrap(ctx context.Context, name string, input map[string]any, next tool.ToolHandler) (any, error) {
    log.Printf("tool %s called with %v", name, input)
    return next(ctx, name, input)
}

myTool.AddMiddleware(&AuditMiddleware{})
```

## See Also

- [Architecture](architecture.md) — Tool system in the broader design
- [Middleware](middleware.md) — Agent-level middleware (including OnActing hook)
- [Deployment](deployment.md) — Workspace sandboxing for tool execution
- [Go-Exclusive Features](go-exclusive.md) — WASM sandbox details
