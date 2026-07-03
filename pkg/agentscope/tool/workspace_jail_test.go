package tool

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/message"
)

func TestResolvePath_JailConfinesToRoot(t *testing.T) {
	root := t.TempDir()
	ctx := WithWorkspaceRoot(context.Background(), root)

	// A path inside the workspace resolves fine.
	if _, err := resolvePath(ctx, filepath.Join(root, "sub", "file.txt")); err != nil {
		t.Errorf("inside-root path was rejected: %v", err)
	}

	// Traversal escapes are rejected.
	for _, p := range []string{
		filepath.Join(root, "..", "escape.txt"),
		"/etc/passwd",
		"/root/.ssh/id_rsa",
	} {
		if _, err := resolvePath(ctx, p); err == nil {
			t.Errorf("expected escape %q to be rejected", p)
		}
	}
}

func TestResolvePath_NoRootIsUnconfined(t *testing.T) {
	// With no workspace root configured, behavior is unchanged (absolute paths
	// resolve without confinement) so existing callers are unaffected.
	if _, err := resolvePath(context.Background(), "/etc/hosts"); err != nil {
		t.Errorf("unconfined resolve should succeed, got %v", err)
	}
}

// TestReadTool_RespectsWorkspaceJail is the end-to-end guard: the read tool
// cannot read outside the configured workspace root.
func TestReadTool_RespectsWorkspaceJail(t *testing.T) {
	root := t.TempDir()
	ctx := WithWorkspaceRoot(context.Background(), root)

	rt := ReadTool()
	resp, err := rt.Execute(ctx, map[string]any{"file_path": "/etc/passwd"})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if resp.State != message.ToolResultError {
		t.Fatalf("expected read of /etc/passwd to be rejected under workspace jail, got state %q", resp.State)
	}
}
