package tool

import (
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// ReadOnlyCommands is the set of commands considered safe (read-only).
var ReadOnlyCommands = map[string]bool{
	"cat": true, "head": true, "tail": true, "less": true, "more": true,
	"ls": true, "dir": true, "find": true, "locate": true, "which": true,
	"whereis": true, "file": true, "stat": true, "wc": true, "du": true,
	"df": true, "free": true, "uptime": true, "uname": true, "hostname": true,
	"whoami": true, "id": true, "groups": true, "env": true, "printenv": true,
	"echo": true, "printf": true, "true": true, "false": true, "test": true,
	"grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true,
	"awk": true, "sed": false, // sed can modify files, handled separately
	"sort": true, "uniq": true, "cut": true, "tr": true, "paste": true,
	"diff": true, "cmp": true, "comm": true,
	"date": true, "cal": true, "bc": true, "expr": true,
	"ps": true, "top": true, "htop": true, "pgrep": true,
	"git":    true, // git subcommands are checked separately below
	"pwd":    true,
	"type":   true,
	"man":    true,
	"help":   true,
	"jq":     true,
	"yq":     true,
	"tree":   true,
	"column": true,
	"md5sum": true, "sha256sum": true, "sha1sum": true,
	"xxd": true, "hexdump": true, "od": true,
	"curl": true, "wget": true, // read-only fetches
	"ping": true, "traceroute": true, "nslookup": true, "dig": true, "host": true,
	"ip": true, "ifconfig": true, "netstat": true, "ss": true,
	"realpath": true, "basename": true, "dirname": true, "readlink": true,
}

// ReadOnlyGitSubcommands lists git subcommands that are read-only.
var ReadOnlyGitSubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true, "branch": true,
	"tag": true, "describe": true, "rev-parse": true, "rev-list": true,
	"ls-files": true, "ls-tree": true, "ls-remote": true, "remote": true,
	"config": true, "blame": true, "shortlog": true, "stash": true,
	"reflog": true, "cat-file": true, "name-rev": true,
}

// parseBashAST parses a bash command string into an AST.
func parseBashAST(cmd string) (*syntax.File, error) {
	parser := syntax.NewParser(syntax.KeepComments(false), syntax.Variant(syntax.LangBash))
	f, err := parser.Parse(strings.NewReader(cmd), "")
	if err != nil {
		return nil, fmt.Errorf("parse bash: %w", err)
	}
	return f, nil
}

// IsReadOnlyCommand checks whether a bash command is read-only by parsing it
// into an AST and checking each simple command against the ReadOnlyCommands set.
// Returns true only if ALL commands in the pipeline/script are read-only.
func IsReadOnlyCommand(cmd string) bool {
	f, err := parseBashAST(cmd)
	if err != nil {
		return false // can't parse => not safe to assume read-only
	}

	allReadOnly := true
	hasCommands := false

	syntax.Walk(f, func(node syntax.Node) bool {
		if !allReadOnly {
			return false
		}
		callExpr, ok := node.(*syntax.CallExpr)
		if !ok || len(callExpr.Args) == 0 {
			return true
		}

		hasCommands = true
		name := extractCommandName(callExpr.Args[0])
		if name == "" {
			allReadOnly = false
			return false
		}

		// Check if the command is in our read-only set
		if !ReadOnlyCommands[name] {
			allReadOnly = false
			return false
		}

		// Special handling for git: check subcommand
		if name == "git" && len(callExpr.Args) > 1 {
			sub := extractWordLiteral(callExpr.Args[1])
			if sub != "" && !ReadOnlyGitSubcommands[sub] {
				allReadOnly = false
				return false
			}
		}

		return true
	})

	return hasCommands && allReadOnly
}

// CheckInjectionRisk detects potentially dangerous shell constructs:
// command substitution $(...) or `...`, process substitution <(...) or >(...),
// eval, exec, source, and similar.
func CheckInjectionRisk(cmd string) (bool, string) {
	f, err := parseBashAST(cmd)
	if err != nil {
		return true, "command could not be parsed as valid bash"
	}

	var risk bool
	var reason string

	syntax.Walk(f, func(node syntax.Node) bool {
		if risk {
			return false
		}
		switch n := node.(type) {
		case *syntax.CmdSubst:
			risk = true
			reason = "contains command substitution $(...)  or `...`"
		case *syntax.ProcSubst:
			risk = true
			reason = "contains process substitution"
		case *syntax.CallExpr:
			if len(n.Args) > 0 {
				name := extractCommandName(n.Args[0])
				switch name {
				case "eval":
					risk = true
					reason = "uses eval which can execute arbitrary code"
				case "exec":
					risk = true
					reason = "uses exec which replaces the current process"
				case "source", ".":
					risk = true
					reason = "uses source/. which can execute arbitrary scripts"
				}
			}
		}
		return true
	})

	return risk, reason
}

// CheckDangerousRemoval detects potentially dangerous rm commands that could
// cause significant data loss (recursive removal, force removal of system paths, etc.).
func CheckDangerousRemoval(cmd string) (bool, string) {
	f, err := parseBashAST(cmd)
	if err != nil {
		return false, ""
	}

	var dangerous bool
	var reason string

	syntax.Walk(f, func(node syntax.Node) bool {
		if dangerous {
			return false
		}
		callExpr, ok := node.(*syntax.CallExpr)
		if !ok || len(callExpr.Args) == 0 {
			return true
		}

		name := extractCommandName(callExpr.Args[0])
		if name != "rm" {
			return true
		}

		hasRecursive := false
		hasForce := false
		var targets []string

		for _, arg := range callExpr.Args[1:] {
			word := extractWordLiteral(arg)
			if word == "" {
				continue
			}
			if strings.HasPrefix(word, "-") {
				if strings.Contains(word, "r") || strings.Contains(word, "R") {
					hasRecursive = true
				}
				if strings.Contains(word, "f") {
					hasForce = true
				}
				continue
			}
			targets = append(targets, word)
		}

		if hasRecursive && hasForce {
			dangerous = true
			reason = "rm -rf detected"
			return false
		}

		for _, t := range targets {
			if t == "/" || t == "/*" || t == "~" || t == "~/" ||
				strings.HasPrefix(t, "/etc") || strings.HasPrefix(t, "/usr") ||
				strings.HasPrefix(t, "/var") || strings.HasPrefix(t, "/home") ||
				strings.HasPrefix(t, "/root") || strings.HasPrefix(t, "/sys") ||
				strings.HasPrefix(t, "/boot") || strings.HasPrefix(t, "/bin") ||
				strings.HasPrefix(t, "/sbin") || strings.HasPrefix(t, "/lib") {
				dangerous = true
				reason = fmt.Sprintf("rm targets system path %q", t)
				return false
			}
		}

		return true
	})

	return dangerous, reason
}

// ExtractFilePaths extracts file path arguments from commands that operate on
// files (rm, mv, cp, chmod, chown, etc.).
func ExtractFilePaths(cmd string) []string {
	f, err := parseBashAST(cmd)
	if err != nil {
		return nil
	}

	fileCommands := map[string]bool{
		"rm": true, "mv": true, "cp": true, "chmod": true, "chown": true,
		"chgrp": true, "touch": true, "mkdir": true, "rmdir": true,
		"ln": true, "install": true,
	}

	var paths []string
	syntax.Walk(f, func(node syntax.Node) bool {
		callExpr, ok := node.(*syntax.CallExpr)
		if !ok || len(callExpr.Args) == 0 {
			return true
		}

		name := extractCommandName(callExpr.Args[0])
		if !fileCommands[name] {
			return true
		}

		for _, arg := range callExpr.Args[1:] {
			word := extractWordLiteral(arg)
			if word == "" || strings.HasPrefix(word, "-") {
				continue
			}
			// Skip mode arguments for chmod (e.g., "755")
			if name == "chmod" && isChmodMode(word) {
				continue
			}
			// Skip user:group arguments for chown
			if name == "chown" && strings.Contains(word, ":") {
				continue
			}
			paths = append(paths, word)
		}

		return true
	})

	return paths
}

// ExtractCommandPrefixes extracts command name prefixes for permission rule
// suggestions. It returns up to max prefixes like "git *", "npm *", etc.
func ExtractCommandPrefixes(cmd string, max int) []string {
	f, err := parseBashAST(cmd)
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var prefixes []string

	syntax.Walk(f, func(node syntax.Node) bool {
		if len(prefixes) >= max {
			return false
		}
		callExpr, ok := node.(*syntax.CallExpr)
		if !ok || len(callExpr.Args) == 0 {
			return true
		}

		name := extractCommandName(callExpr.Args[0])
		if name == "" {
			return true
		}

		// For git-like commands, include subcommand: "git status"
		prefix := name
		if len(callExpr.Args) > 1 {
			sub := extractWordLiteral(callExpr.Args[1])
			if sub != "" && !strings.HasPrefix(sub, "-") {
				prefix = name + " " + sub
			}
		}

		suggestion := prefix + ":*"
		if !seen[suggestion] {
			seen[suggestion] = true
			prefixes = append(prefixes, suggestion)
		}
		return true
	})

	return prefixes
}

// sedAllowedFlags is the set of sed flags that are safe to use.
var sedAllowedFlags = map[string]bool{
	"-e": true, "-n": true, "--quiet": true, "--silent": true,
	"-E": true, "-r": true, "--regexp-extended": true,
}

// CheckSedConstraints checks whether a sed command is safe: it must not use -i
// (in-place edit) or target dangerous files.
func CheckSedConstraints(cmd string, dangerousFiles []string) (bool, string) {
	f, err := parseBashAST(cmd)
	if err != nil {
		return false, ""
	}

	var violation bool
	var reason string

	syntax.Walk(f, func(node syntax.Node) bool {
		if violation {
			return false
		}
		callExpr, ok := node.(*syntax.CallExpr)
		if !ok || len(callExpr.Args) == 0 {
			return true
		}

		name := extractCommandName(callExpr.Args[0])
		if name != "sed" {
			return true
		}

		var filePaths []string
		for _, arg := range callExpr.Args[1:] {
			word := extractWordLiteral(arg)
			if word == "" {
				continue
			}
			// Check for in-place editing
			if word == "-i" || strings.HasPrefix(word, "-i") || word == "--in-place" {
				violation = true
				reason = "sed uses -i (in-place editing) which modifies files"
				return false
			}
			// Check for disallowed flags
			if strings.HasPrefix(word, "-") && !sedAllowedFlags[word] {
				// Combined flags like -ne are OK
				allAllowed := true
				if len(word) > 1 && word[1] != '-' {
					for _, c := range word[1:] {
						if !sedAllowedFlags["-"+string(c)] {
							allAllowed = false
							break
						}
					}
				} else {
					allAllowed = false
				}
				if !allAllowed && !sedAllowedFlags[word] {
					// Ignore unrecognized flags as they may be expressions
				}
				continue
			}
			if !strings.HasPrefix(word, "-") {
				filePaths = append(filePaths, word)
			}
		}

		// Check if any target file is dangerous
		for _, fp := range filePaths {
			for _, df := range dangerousFiles {
				if matchesDangerousFile(fp, df) {
					violation = true
					reason = fmt.Sprintf("sed targets dangerous file %q", df)
					return false
				}
			}
		}

		return true
	})

	return violation, reason
}

// --- helpers ---

// extractCommandName extracts the command name from an AST word node,
// handling simple cases and ignoring complex expansions.
func extractCommandName(word *syntax.Word) string {
	if len(word.Parts) == 0 {
		return ""
	}
	lit, ok := word.Parts[0].(*syntax.Lit)
	if !ok {
		return ""
	}
	// Strip path prefix: /usr/bin/grep -> grep
	name := lit.Value
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}

// extractWordLiteral extracts a simple literal string from a word node.
// Returns empty string if the word contains expansions or other complex parts.
func extractWordLiteral(word *syntax.Word) string {
	if len(word.Parts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, p := range word.Parts {
		lit, ok := p.(*syntax.Lit)
		if !ok {
			// Contains variable expansion, command substitution, etc.
			return ""
		}
		b.WriteString(lit.Value)
	}
	return b.String()
}

// isChmodMode returns true if s looks like a chmod mode argument (e.g. "755", "u+x").
func isChmodMode(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Numeric modes: 644, 755, 0755
	allDigits := true
	for _, c := range s {
		if c < '0' || c > '9' {
			allDigits = false
			break
		}
	}
	if allDigits && len(s) >= 3 && len(s) <= 4 {
		return true
	}
	// Symbolic modes: u+x, go-w, a+r, etc.
	for _, c := range s {
		switch {
		case c == 'u', c == 'g', c == 'o', c == 'a',
			c == '+', c == '-', c == '=',
			c == 'r', c == 'w', c == 'x', c == 'X',
			c == 's', c == 't':
			continue
		default:
			return false
		}
	}
	return true
}
