package tool

import (
	"fmt"
	"path/filepath"
	"strings"
)

// DangerousFiles lists files that should be protected from auto-editing.
// Includes shell configs, credential files, and environment secrets.
var DangerousFiles = []string{
	".gitconfig",
	".gitmodules",
	".bashrc",
	".bash_profile",
	".zshrc",
	".zprofile",
	".profile",
	".ssh/config",
	".ssh/authorized_keys",
	".netrc",
	".npmrc",
	".pypirc",
	".env",
	".envrc",
	".env.local",
	".env.development",
	".env.development.local",
	".env.test",
	".env.test.local",
	".env.staging",
	".env.production",
	".env.production.local",
}

// DangerousDirectories lists directories that contain sensitive config or executables.
var DangerousDirectories = []string{
	".git",
	".vscode",
	".idea",
	".ssh",
}

// DangerousCommandPatterns lists command patterns (word-boundary aware) that can
// cause data loss, system damage, or security issues.
var DangerousCommandPatterns = []string{
	"rm -rf",
	"sudo rm",
	"dd ",
	"mkfs",
	"fdisk",
	"format ",
	"chmod 777",
	"chmod -R 777",
	"chown -R",
	"kill -9",
	"> /dev/",
	// Windows / PowerShell patterns
	"del /s",
	"rmdir /s",
	"rd /s",
	"Remove-Item.*-Recurse",
	"Format-Volume",
	"Stop-Process.*-Force",
	"Clear-Content",
}

// CheckDangerousCommand returns true if cmd contains a dangerous command pattern.
// It first attempts AST-based analysis via CheckDangerousRemoval and CheckInjectionRisk,
// then falls back to simple string matching for patterns not covered by the parser.
func CheckDangerousCommand(cmd string) (bool, string) {
	// AST-based checks first
	if dangerous, reason := CheckDangerousRemoval(cmd); dangerous {
		return true, reason
	}
	if risky, reason := CheckInjectionRisk(cmd); risky {
		return true, reason
	}

	// Fallback: simple pattern matching for remaining patterns
	lower := strings.ToLower(cmd)
	for _, pattern := range DangerousCommandPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return true, fmt.Sprintf("command contains dangerous pattern %q", pattern)
		}
	}
	return false, ""
}

// CheckDangerousPath returns true if path touches a dangerous file or directory.
func CheckDangerousPath(path string) (bool, string) {
	clean := filepath.Clean(path)

	for _, df := range DangerousFiles {
		if matchesDangerousFile(clean, df) {
			return true, fmt.Sprintf("path touches dangerous file %q", df)
		}
	}

	parts := strings.Split(clean, string(filepath.Separator))
	for _, part := range parts {
		for _, dd := range DangerousDirectories {
			if part == dd {
				return true, fmt.Sprintf("path traverses dangerous directory %q", dd)
			}
		}
	}

	return false, ""
}

// CheckDangerousFilePaths extracts file paths from a command and checks each
// against the dangerous files/directories list. Returns true if any path is dangerous.
func CheckDangerousFilePaths(cmd string) (bool, string) {
	paths := ExtractFilePaths(cmd)
	for _, p := range paths {
		if dangerous, reason := CheckDangerousPath(p); dangerous {
			return true, reason
		}
	}
	return false, ""
}

func matchesDangerousFile(path, dangerous string) bool {
	if filepath.Base(path) == filepath.Base(dangerous) {
		if !strings.Contains(dangerous, "/") {
			return true
		}
		return strings.HasSuffix(filepath.ToSlash(path), filepath.ToSlash(dangerous))
	}
	return false
}
