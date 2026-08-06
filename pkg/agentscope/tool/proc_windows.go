//go:build windows

package tool

import "os/exec"

// configureProcIsolation is a no-op on Windows. exec.CommandContext's
// default Cancel (Process.Kill) terminates the process tree on Windows.
func configureProcIsolation(_ *exec.Cmd) {}
