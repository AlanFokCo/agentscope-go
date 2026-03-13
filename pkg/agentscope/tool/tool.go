package tool

import (
	"context"
	"fmt"
	"sync"
)

// Tool describes a callable tool that can be invoked by a model.
type Tool struct {
	Name        string
	Description string

	// Execute receives structured arguments and returns a result.
	Execute func(ctx context.Context, args map[string]any) (any, error)
}

var (
	mu       sync.RWMutex
	registry = map[string]*Tool{}
)

// Toolkit is a lightweight wrapper managing a set of tools per agent.
// It does not automatically register tools in the global registry.
type Toolkit struct {
	Tools map[string]*Tool
}

// NewToolkit builds a Toolkit from the given tool list.
func NewToolkit(tools ...*Tool) *Toolkit {
	t := &Toolkit{Tools: make(map[string]*Tool, len(tools))}
	for _, tool := range tools {
		if tool == nil || tool.Name == "" {
			continue
		}
		t.Tools[tool.Name] = tool
	}
	return t
}

// Get fetches a tool from the toolkit by name.
func (t *Toolkit) Get(name string) *Tool {
	if t == nil {
		return nil
	}
	return t.Tools[name]
}

// Register registers a tool in the global registry (by unique name).
func Register(t *Tool) error {
	if t == nil || t.Name == "" {
		return fmt.Errorf("tool: invalid tool")
	}
	mu.Lock()
	defer mu.Unlock()
	registry[t.Name] = t
	return nil
}

// Get returns a globally registered tool by name.
func Get(name string) *Tool {
	mu.RLock()
	defer mu.RUnlock()
	return registry[name]
}

// List returns the names of all globally registered tools.
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

