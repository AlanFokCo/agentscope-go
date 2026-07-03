package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// workspaceRootKey is the context key carrying the workspace-root jail.
type workspaceRootKey struct{}

// WithWorkspaceRoot confines file-tool path resolution to root. Any path that
// resolves (symlinks included) outside root is rejected. When no root is set,
// path resolution is unconfined (backward-compatible default).
func WithWorkspaceRoot(ctx context.Context, root string) context.Context {
	return context.WithValue(ctx, workspaceRootKey{}, root)
}

// getWorkspaceRoot returns the configured workspace root, or "" if unset.
func getWorkspaceRoot(ctx context.Context) string {
	if r, ok := ctx.Value(workspaceRootKey{}).(string); ok {
		return r
	}
	return ""
}

// resolvePath resolves path to an absolute, cleaned path. If a workspace root is
// configured on ctx, the result is confined to that root (symlink-aware), and an
// error is returned for any path that escapes it. This is the single choke point
// file tools use so an agent cannot read /etc/shadow or write outside its jail.
func resolvePath(ctx context.Context, path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}

	root := getWorkspaceRoot(ctx)
	if root == "" {
		return abs, nil
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("invalid workspace root: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = resolved
	}

	// Resolve symlinks on the target (or its nearest existing ancestor, so that
	// writing a new file cannot escape via a symlinked parent directory).
	target := evalSymlinksLenient(abs)

	if !withinRoot(rootAbs, target) {
		return "", fmt.Errorf("path %q escapes workspace root %q", path, root)
	}
	return abs, nil
}

// evalSymlinksLenient resolves symlinks for p if it exists, otherwise resolves
// the longest existing ancestor and rejoins the non-existent remainder.
func evalSymlinksLenient(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	dir := p
	var rest []string
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		rest = append([]string{filepath.Base(dir)}, rest...)
		dir = parent
		if _, err := os.Stat(dir); err == nil {
			if resolved, err := filepath.EvalSymlinks(dir); err == nil {
				return filepath.Join(append([]string{resolved}, rest...)...)
			}
			break
		}
	}
	return p
}

// withinRoot reports whether target is root itself or nested under it, using a
// separator-safe relative-path check (so "/rootx" is not treated as under "/root").
func withinRoot(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
