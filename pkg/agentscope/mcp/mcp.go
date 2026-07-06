package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/model"
	"github.com/alanfokco/agentscope-go/v2/pkg/agentscope/tool"
)

// mcpNameRegexp validates MCP server names. Names must consist of
// alphanumeric characters, hyphens, and underscores only.
var mcpNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ValidateMCPName checks whether the given name is a valid MCP server name.
// Valid names match the pattern ^[a-zA-Z0-9_-]+$.
func ValidateMCPName(name string) error {
	if name == "" {
		return fmt.Errorf("mcp: name must not be empty")
	}
	if !mcpNameRegexp.MatchString(name) {
		return fmt.Errorf("mcp: name %q is invalid (must match %s)", name, mcpNameRegexp.String())
	}
	return nil
}

// Client defines the interface for communicating with an MCP server.
type Client interface {
	ListTools(ctx context.Context) ([]model.ToolSchema, error)
	CallTool(ctx context.Context, name string, input map[string]any) (*tool.ToolResponse, error)
	Close() error
}

// StdioConfig configures a subprocess-based (stdio) MCP connection.
type StdioConfig struct {
	Command string
	Args    []string
	Env     []string
	WorkDir string

	Stateful         bool     // true = keep subprocess alive across calls; false = ephemeral per CallTool
	EnableTools      []string // whitelist; empty = all tools
	DisableTools     []string // blacklist; applied after EnableTools
	ExecutionTimeout int      // per-tool call timeout in seconds; 0 = no timeout
}

// HttpConfig configures an HTTP/SSE-based MCP connection.
type HttpConfig struct {
	URL     string
	Headers map[string]string
	Timeout int // seconds, 0 = default (30)

	Stateful         bool // true = reuse session ID across calls
	EnableTools      []string
	DisableTools     []string
	ExecutionTimeout int
}

// JSON-RPC 2.0 types for MCP protocol.

type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCP protocol types

type initializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      clientInfo     `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolListResult struct {
	Tools []mcpTool `json:"tools"`
}

type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations *mcpAnnotations `json:"annotations,omitempty"`
}

type mcpAnnotations struct {
	ReadOnlyHint bool `json:"readOnlyHint"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type toolCallResult struct {
	Content []mcpContent `json:"content"`
	IsError bool         `json:"isError"`
}

type mcpContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}
