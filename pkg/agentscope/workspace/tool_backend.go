package workspace

import (
	"context"
	"path/filepath"
	"time"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/tool"
)

// ToolBackend adapts a Workspace to tool.Backend so the file/shell builtin tools
// (read/write/edit/bash/grep/glob) operate inside the workspace — a Docker or
// E2B sandbox — instead of on the host. This gives real isolation.
//
// Wire it into tool execution via:
//
//	ctx = tool.WithBackend(ctx, workspace.NewToolBackend(ws))
type ToolBackend struct {
	ws Workspace
}

// NewToolBackend returns a tool.Backend backed by the given Workspace.
func NewToolBackend(ws Workspace) *ToolBackend { return &ToolBackend{ws: ws} }

// ExecShell runs a command inside the workspace. The timeout is governed by the
// workspace's own execution policy.
func (b *ToolBackend) ExecShell(ctx context.Context, command string, _ time.Duration) (*tool.ExecResult, error) {
	res, err := b.ws.Execute(ctx, command)
	if err != nil {
		return nil, err
	}
	return &tool.ExecResult{Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: res.ExitCode}, nil
}

// ReadFile reads a file from the workspace.
func (b *ToolBackend) ReadFile(ctx context.Context, path string) ([]byte, error) {
	return b.ws.ReadFile(ctx, path)
}

// WriteFile writes a file into the workspace.
func (b *ToolBackend) WriteFile(ctx context.Context, path string, data []byte) error {
	return b.ws.WriteFile(ctx, path, data)
}

// FileExists reports whether a file exists in the workspace.
func (b *ToolBackend) FileExists(ctx context.Context, path string) (bool, error) {
	if _, err := b.ws.ReadFile(ctx, path); err == nil {
		return true, nil
	}
	return false, nil
}

// ListDir returns the entry names of a directory in the workspace.
func (b *ToolBackend) ListDir(ctx context.Context, path string) ([]string, error) {
	fis, err := b.ws.ListFiles(ctx, path)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(fis))
	for i, fi := range fis {
		names[i] = fi.Name
	}
	return names, nil
}

// Glob matches a single-level pattern within the workspace. It lists the
// pattern's directory and applies filepath.Match to each entry — no shell is
// invoked, so it is injection-safe (recursive ** is not supported).
func (b *ToolBackend) Glob(ctx context.Context, pattern string) ([]string, error) {
	dir := filepath.Dir(pattern)
	base := filepath.Base(pattern)
	fis, err := b.ws.ListFiles(ctx, dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, fi := range fis {
		if ok, _ := filepath.Match(base, fi.Name); ok {
			if dir == "." {
				out = append(out, fi.Name)
			} else {
				out = append(out, filepath.Join(dir, fi.Name))
			}
		}
	}
	return out, nil
}

// Compile-time check.
var _ tool.Backend = (*ToolBackend)(nil)
