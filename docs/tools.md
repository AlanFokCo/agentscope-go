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

func (m *AuditMiddleware) Wrap(ctx context.Context, name string, input map[string]any, next tool.ToolHandler) (*tool.ToolResponse, error) {
    log.Printf("tool %s called with %v", name, input)
    return next(ctx, name, input)
}

myTool.AddMiddleware(&AuditMiddleware{})
```
