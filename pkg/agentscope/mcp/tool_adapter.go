package mcp

import (
	"context"
	"encoding/json"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

// MCPTool wraps a single MCP server tool as a Go Tool interface implementation.
type MCPTool struct {
	tool.BaseTool
	client   Client
	toolName string // original MCP tool name (may differ from adapted name)
}

// NewMCPTool creates a Tool backed by an MCP client for the given tool schema.
func NewMCPTool(client Client, schema model.ToolSchema) *MCPTool {
	return &MCPTool{
		BaseTool: tool.BaseTool{
			ToolName:        schema.Function.Name,
			ToolDescription: schema.Function.Description,
			ToolSchema:      schema.Function.Parameters,
			ReadOnly:        false,
			ConcurrencySafe: true,
		},
		client:   client,
		toolName: schema.Function.Name,
	}
}

// Execute calls the MCP server to execute the tool.
func (t *MCPTool) Execute(ctx context.Context, args map[string]any) (*tool.ToolResponse, error) {
	return t.client.CallTool(ctx, t.toolName, args)
}

// NewMCPToolkit creates a Toolkit containing all tools from an MCP client.
func NewMCPToolkit(ctx context.Context, client Client) (*tool.Toolkit, error) {
	schemas, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}

	var tools []tool.Tool
	for _, s := range schemas {
		tools = append(tools, NewMCPTool(client, s))
	}

	return tool.NewToolkit(tools...), nil
}

// MergeToolkits combines multiple toolkits into one.
func MergeToolkits(toolkits ...*tool.Toolkit) *tool.Toolkit {
	var allTools []tool.Tool
	for _, tk := range toolkits {
		for _, schema := range tk.GetToolSchemas() {
			t := tk.Get(schema.Function.Name)
			if t != nil {
				allTools = append(allTools, t)
			}
		}
	}
	return tool.NewToolkit(allTools...)
}

// Compile-time interface checks.
var _ Client = (*StdioClient)(nil)
var _ Client = (*HttpClient)(nil)
var _ tool.Tool = (*MCPTool)(nil)

// ensure BaseTool's ToolSchema field is json.RawMessage
var _ json.RawMessage = json.RawMessage(nil)
