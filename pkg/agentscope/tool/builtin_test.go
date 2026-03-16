package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteShellCommandTool(t *testing.T) {
	tool := ExecuteShellCommandTool()
	if tool.Name != "execute_shell_command" {
		t.Fatalf("unexpected name: %s", tool.Name)
	}

	ctx := context.Background()

	// Success case
	out, err := tool.Execute(ctx, map[string]any{"command": "echo hello"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	output := m["output"].(string)
	if !strings.Contains(output, "hello") {
		t.Fatalf("unexpected output: %q", m["output"])
	}

	// Missing command
	_, err = tool.Execute(ctx, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestViewTextFileTool(t *testing.T) {
	tool := ViewTextFileTool()
	if tool.Name != "view_text_file" {
		t.Fatalf("unexpected name: %s", tool.Name)
	}

	ctx := context.Background()

	// Create temp file
	tmp, err := os.CreateTemp("", "agentscope-view-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(tmp.Name())
	_, _ = tmp.WriteString("hello world\n")
	tmp.Close()

	// Success case with "path"
	out, err := tool.Execute(ctx, map[string]any{"path": tmp.Name()})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", out)
	}
	if m["content"].(string) != "hello world\n" {
		t.Fatalf("unexpected content: %q", m["content"])
	}

	// Success case with "file_path"
	out, err = tool.Execute(ctx, map[string]any{"file_path": tmp.Name()})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	m = out.(map[string]any)
	if m["content"].(string) != "hello world\n" {
		t.Fatalf("unexpected content: %q", m["content"])
	}

	// File not found
	_, err = tool.Execute(ctx, map[string]any{"path": filepath.Join(os.TempDir(), "nonexistent-12345.txt")})
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}

	// Missing path
	_, err = tool.Execute(ctx, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestNewBuiltinToolkit(t *testing.T) {
	tk := NewBuiltinToolkit()
	if tk == nil {
		t.Fatal("nil toolkit")
	}
	if tk.Get("execute_shell_command") == nil {
		t.Fatal("execute_shell_command not in toolkit")
	}
	if tk.Get("view_text_file") == nil {
		t.Fatal("view_text_file not in toolkit")
	}
}
