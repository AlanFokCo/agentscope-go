package wasm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CLIRuntime executes WASM modules by shelling out to wasmtime, wasmer, or wasm3.
type CLIRuntime struct {
	binaryPath  string
	runtimeName string
}

// supportedRuntimes lists the CLI binaries we can shell out to, in preference order.
var supportedRuntimes = []string{"wasmtime", "wasm3", "wasmer"}

// NewCLIRuntime creates a runtime that uses the given binary.
// If binaryPath is empty, it attempts auto-discovery.
func NewCLIRuntime(binaryPath string) (*CLIRuntime, error) {
	if binaryPath == "" {
		return AutoDiscover()
	}

	// Verify the binary exists.
	if _, err := os.Stat(binaryPath); err != nil {
		// Maybe it is just a name in PATH.
		resolved, lookErr := exec.LookPath(binaryPath)
		if lookErr != nil {
			return nil, fmt.Errorf("%w: %s", ErrRuntimeNotFound, binaryPath)
		}
		binaryPath = resolved
	}

	name := runtimeNameFromPath(binaryPath)
	return &CLIRuntime{
		binaryPath:  binaryPath,
		runtimeName: name,
	}, nil
}

// AutoDiscover finds an available WASM runtime in PATH.
func AutoDiscover() (*CLIRuntime, error) {
	for _, name := range supportedRuntimes {
		path, err := exec.LookPath(name)
		if err == nil {
			return &CLIRuntime{
				binaryPath:  path,
				runtimeName: name,
			}, nil
		}
	}
	return nil, ErrRuntimeNotFound
}

// Name returns the runtime implementation name.
func (r *CLIRuntime) Name() string {
	return r.runtimeName
}

// BinaryPath returns the resolved binary path (useful for testing).
func (r *CLIRuntime) BinaryPath() string {
	return r.binaryPath
}

// Execute runs a WASM module by shelling out to the CLI runtime.
func (r *CLIRuntime) Execute(ctx context.Context, req *ExecRequest) (*ExecResult, error) {
	if req == nil {
		return nil, fmt.Errorf("wasm: nil exec request")
	}

	// Validate module path exists.
	if _, err := os.Stat(req.ModulePath); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrModuleNotFound, req.ModulePath)
	}

	// Apply defaults.
	timeout := req.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	memMax := req.MemoryMax
	if memMax == 0 {
		memMax = DefaultMemoryMax
	}

	// Build the command.
	args := r.buildArgs(req, memMax)

	// Apply timeout to context.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.binaryPath, args...)

	// Set environment variables.
	if len(req.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range req.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	// Pipe stdin.
	if len(req.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(req.Stdin)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	result := &ExecResult{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		Duration: duration,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return result, ErrTimeout
		}
		// Check for memory exceeded patterns in stderr.
		stderrStr := stderr.String()
		if strings.Contains(stderrStr, "memory") && strings.Contains(stderrStr, "limit") {
			return result, ErrMemoryExceeded
		}
		if strings.Contains(stderrStr, "out of memory") {
			return result, ErrMemoryExceeded
		}
		// Extract exit code.
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		return result, nil
	}

	result.ExitCode = 0
	return result, nil
}

// BuildArgs constructs the command-line arguments for the runtime (exported for testing).
func (r *CLIRuntime) BuildArgs(req *ExecRequest, memMax uint64) []string {
	return r.buildArgs(req, memMax)
}

// buildArgs constructs the CLI arguments based on the runtime type.
func (r *CLIRuntime) buildArgs(req *ExecRequest, memMax uint64) []string {
	switch r.runtimeName {
	case "wasmtime":
		return r.buildWasmtimeArgs(req, memMax)
	case "wasm3":
		return r.buildWasm3Args(req)
	case "wasmer":
		return r.buildWasmerArgs(req, memMax)
	default:
		return r.buildWasmtimeArgs(req, memMax)
	}
}

func (r *CLIRuntime) buildWasmtimeArgs(req *ExecRequest, memMax uint64) []string {
	args := []string{"run"}

	// Fuel limit for bounded execution.
	args = append(args, "--fuel", fmt.Sprintf("%d", DefaultFuel), "--max-wasm-stack", "1048576")

	// Memory limit.
	memMB := memMax / (1024 * 1024)
	if memMB == 0 {
		memMB = 64
	}
	args = append(args, fmt.Sprintf("-O max-memory=%d", memMB*1024*1024))

	// Function to invoke (if not _start).
	if req.Function != "" && req.Function != "_start" {
		args = append(args, "--invoke", req.Function)
	}

	// Environment variables passed to the module.
	for k, v := range req.Env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	// Module path.
	args = append(args, req.ModulePath)

	// Separator and module arguments.
	if len(req.Args) > 0 {
		args = append(args, "--")
		args = append(args, req.Args...)
	}

	return args
}

func (r *CLIRuntime) buildWasm3Args(req *ExecRequest) []string {
	args := []string{}

	// Function to invoke.
	if req.Function != "" && req.Function != "_start" {
		args = append(args, "--func", req.Function)
	}

	// Module path.
	args = append(args, req.ModulePath)

	// Module arguments.
	args = append(args, req.Args...)

	return args
}

func (r *CLIRuntime) buildWasmerArgs(req *ExecRequest, memMax uint64) []string {
	args := []string{"run"}

	// Environment variables.
	for k, v := range req.Env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	// Module path.
	args = append(args, req.ModulePath)

	// Separator and module arguments.
	if len(req.Args) > 0 {
		args = append(args, "--")
		args = append(args, req.Args...)
	}

	return args
}

// runtimeNameFromPath extracts the runtime name from a binary path.
func runtimeNameFromPath(path string) string {
	for _, name := range supportedRuntimes {
		if strings.Contains(path, name) {
			return name
		}
	}
	// Default to treating it as wasmtime-compatible.
	return "wasmtime"
}
