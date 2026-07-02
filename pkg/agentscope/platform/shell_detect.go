package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	detectedShell Shell
	detectOnce    sync.Once
)

// Detect returns the current shell, auto-detecting on first call.
// Result is cached for the process lifetime.
func Detect() Shell {
	detectOnce.Do(func() {
		detectedShell = detect()
	})
	return detectedShell
}

// DetectShellTypeFromPath identifies a ShellType from a shell executable path.
func DetectShellTypeFromPath(shellPath string) ShellType {
	// filepath.Base only splits on the OS path separator, so on Unix it
	// won't split Windows-style backslash paths. Handle both separators.
	base := filepath.Base(shellPath)
	if i := strings.LastIndex(base, `\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.ToLower(base)
	base = strings.TrimSuffix(base, ".exe")

	switch {
	case base == "bash":
		return ShellBash
	case base == "zsh":
		return ShellZsh
	case base == "sh":
		return ShellSh
	case base == "pwsh" || base == "powershell":
		return ShellPowerShell
	case base == "cmd":
		return ShellCmd
	}

	if strings.Contains(base, "bash") {
		return ShellBash
	}
	if strings.Contains(base, "zsh") {
		return ShellZsh
	}
	if strings.Contains(base, "powershell") || strings.Contains(base, "pwsh") {
		return ShellPowerShell
	}

	return ShellSh
}

func detect() Shell {
	if shellEnv := os.Getenv("SHELL"); shellEnv != "" {
		st := DetectShellTypeFromPath(shellEnv)
		return Shell{Type: st, Path: shellEnv}
	}

	if comspec := os.Getenv("COMSPEC"); comspec != "" {
		st := DetectShellTypeFromPath(comspec)
		return Shell{Type: st, Path: comspec}
	}

	if runtime.GOOS == "windows" {
		if pwsh, err := exec.LookPath("pwsh"); err == nil {
			return Shell{Type: ShellPowerShell, Path: pwsh}
		}
		if ps, err := exec.LookPath("powershell.exe"); err == nil {
			return Shell{Type: ShellPowerShell, Path: ps}
		}
		return Shell{Type: ShellCmd, Path: "cmd.exe"}
	}

	for _, name := range []string{"bash", "zsh", "sh"} {
		if p, err := exec.LookPath(name); err == nil {
			return Shell{Type: DetectShellTypeFromPath(p), Path: p}
		}
	}

	return Shell{Type: ShellSh, Path: "/bin/sh"}
}
