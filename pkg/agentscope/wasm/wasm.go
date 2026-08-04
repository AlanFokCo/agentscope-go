package wasm

import (
	"context"
	"errors"
	"time"
)

// Runtime abstracts a WASM execution engine.
type Runtime interface {
	// Execute runs a WASM module with the given input and returns stdout.
	Execute(ctx context.Context, req *ExecRequest) (*ExecResult, error)
	// Name returns the runtime implementation name.
	Name() string
}

// ExecRequest describes what to execute.
type ExecRequest struct {
	ModulePath string            // path to .wasm file
	Function   string            // exported function name (default: "_start")
	Stdin      []byte            // data piped to stdin
	Env        map[string]string // environment variables
	Args       []string          // command-line arguments
	Timeout    time.Duration     // execution timeout (default: 30s)
	MemoryMax  uint64            // max memory in bytes (default: 64MB)
}

// ExecResult holds the output of a WASM execution.
type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
}

// Default values.
const (
	DefaultTimeout   = 30 * time.Second
	DefaultMemoryMax = 64 * 1024 * 1024 // 64MB
	DefaultFuel      = int64(1_000_000)
)

// Errors.
var (
	ErrTimeout         = errors.New("wasm: execution timed out")
	ErrMemoryExceeded  = errors.New("wasm: memory limit exceeded")
	ErrModuleNotFound  = errors.New("wasm: module not found")
	ErrRuntimeNotFound = errors.New("wasm: runtime binary not found")
)
