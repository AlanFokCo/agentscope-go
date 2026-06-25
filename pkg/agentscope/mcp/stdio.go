package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

// StdioClient communicates with an MCP server via subprocess stdin/stdout.
type StdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	nextID int
	mu     sync.Mutex
}

// NewStdioClient starts the MCP server subprocess and initializes the session.
func NewStdioClient(ctx context.Context, cfg *StdioConfig) (*StdioClient, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}
	if len(cfg.Env) > 0 {
		cmd.Env = cfg.Env
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdio: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp stdio: start: %w", err)
	}

	c := &StdioClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewScanner(stdout),
		nextID: 1,
	}

	if err := c.initialize(ctx); err != nil {
		c.Close()
		return nil, err
	}

	return c, nil
}

func (c *StdioClient) initialize(ctx context.Context) error {
	_, err := c.call(ctx, "initialize", initializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    map[string]any{},
		ClientInfo:      clientInfo{Name: "agentscope-go", Version: "2.0"},
	})
	if err != nil {
		return fmt.Errorf("mcp stdio: initialize: %w", err)
	}

	// Send initialized notification (no id, no response expected)
	notif := jsonrpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}
	data, _ := json.Marshal(notif)
	c.mu.Lock()
	_, err = c.stdin.Write(append(data, '\n'))
	c.mu.Unlock()
	return err
}

func (c *StdioClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}

	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		c.mu.Unlock()
		return nil, err
	}

	for c.stdout.Scan() {
		line := c.stdout.Bytes()
		var resp jsonrpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}
		if resp.ID == id {
			c.mu.Unlock()
			if resp.Error != nil {
				return nil, fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
			}
			return resp.Result, nil
		}
	}
	c.mu.Unlock()

	if err := c.stdout.Err(); err != nil {
		return nil, fmt.Errorf("mcp stdio: read: %w", err)
	}
	return nil, fmt.Errorf("mcp stdio: unexpected end of stream")
}

// ListTools queries the MCP server for available tools.
func (c *StdioClient) ListTools(ctx context.Context) ([]model.ToolSchema, error) {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var toolList toolListResult
	if err := json.Unmarshal(result, &toolList); err != nil {
		return nil, fmt.Errorf("mcp stdio: parse tools: %w", err)
	}

	return convertToolSchemas(toolList.Tools), nil
}

// CallTool invokes a tool on the MCP server.
func (c *StdioClient) CallTool(ctx context.Context, name string, input map[string]any) (*tool.ToolResponse, error) {
	result, err := c.call(ctx, "tools/call", toolCallParams{
		Name:      name,
		Arguments: input,
	})
	if err != nil {
		return nil, err
	}

	var callResult toolCallResult
	if err := json.Unmarshal(result, &callResult); err != nil {
		return nil, fmt.Errorf("mcp stdio: parse result: %w", err)
	}

	return convertToolResult(callResult), nil
}

// Close terminates the MCP server subprocess.
func (c *StdioClient) Close() error {
	c.stdin.Close()
	return c.cmd.Wait()
}

// Shared conversion helpers

func convertToolSchemas(tools []mcpTool) []model.ToolSchema {
	schemas := make([]model.ToolSchema, len(tools))
	for i, t := range tools {
		params := t.InputSchema
		if params == nil {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		schemas[i] = model.ToolSchema{
			Type: "function",
			Function: model.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		}
	}
	return schemas
}

func convertToolResult(result toolCallResult) *tool.ToolResponse {
	var blocks []message.ContentBlock
	for _, c := range result.Content {
		switch c.Type {
		case "text":
			blocks = append(blocks, message.TextBlock{Type: "text", Text: c.Text})
		case "image":
			blocks = append(blocks, message.DataBlock{
				Type: "data",
				Source: message.Base64Source{
					Type:      "base64",
					Data:      c.Data,
					MediaType: c.MimeType,
				},
			})
		default:
			blocks = append(blocks, message.TextBlock{Type: "text", Text: c.Text})
		}
	}

	state := message.ToolResultSuccess
	if result.IsError {
		state = message.ToolResultError
	}
	if len(blocks) == 0 {
		blocks = []message.ContentBlock{message.TextBlock{Type: "text", Text: ""}}
	}

	return &tool.ToolResponse{
		Content: blocks,
		State:   state,
	}
}
