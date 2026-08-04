package wasm

import (
	"context"
	"fmt"
	"os"
	"time"
)

// SandboxConfig configures the WASM sandbox.
type SandboxConfig struct {
	Runtime      Runtime
	AllowedPaths []string      // paths the module can access (if WASI enabled)
	MaxMemory    uint64        // default: 64MB
	MaxDuration  time.Duration // default: 30s
	MaxFuel      int64         // instruction count limit (wasmtime-specific)
}

// Sandbox provides a safe execution environment for WASM modules.
type Sandbox struct {
	cfg SandboxConfig
}

// NewSandbox creates a new sandbox with the given config.
func NewSandbox(cfg SandboxConfig) *Sandbox {
	if cfg.MaxMemory == 0 {
		cfg.MaxMemory = DefaultMemoryMax
	}
	if cfg.MaxDuration == 0 {
		cfg.MaxDuration = DefaultTimeout
	}
	if cfg.MaxFuel == 0 {
		cfg.MaxFuel = DefaultFuel
	}
	return &Sandbox{cfg: cfg}
}

// Run executes a WASM module in the sandbox with stdin input.
func (s *Sandbox) Run(ctx context.Context, modulePath string, input []byte) (*ExecResult, error) {
	return s.RunWithArgs(ctx, modulePath, nil, input)
}

// RunWithArgs executes a WASM module with explicit arguments.
func (s *Sandbox) RunWithArgs(ctx context.Context, modulePath string, args []string, input []byte) (*ExecResult, error) {
	if s.cfg.Runtime == nil {
		return nil, fmt.Errorf("wasm: no runtime configured")
	}

	// Validate module exists before sending to runtime.
	if _, err := os.Stat(modulePath); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrModuleNotFound, modulePath)
	}

	req := &ExecRequest{
		ModulePath: modulePath,
		Function:   "_start",
		Stdin:      input,
		Args:       args,
		Timeout:    s.cfg.MaxDuration,
		MemoryMax:  s.cfg.MaxMemory,
	}

	return s.cfg.Runtime.Execute(ctx, req)
}
