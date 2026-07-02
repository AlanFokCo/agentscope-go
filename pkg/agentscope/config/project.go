package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alanfokco/agentscope-go/pkg/agentscope/permission"
)

// ProjectConfig holds all project-level configuration loaded from disk.
type ProjectConfig struct {
	// Instructions from CLAUDE.md / AGENTS.md (raw markdown)
	Instructions string `json:"instructions,omitempty"`

	// PermissionRules loaded from .agentscope/settings.json
	PermissionRules []permission.Rule `json:"permission_rules,omitempty"`

	// AllowedTools is a whitelist of tool names. Empty = all tools allowed.
	AllowedTools []string `json:"allowed_tools,omitempty"`

	// DeniedTools is a blacklist of tool names.
	DeniedTools []string `json:"denied_tools,omitempty"`

	// ModelPreference is the preferred model name (e.g. "claude-sonnet-4-6")
	ModelPreference string `json:"model_preference,omitempty"`

	// Hooks maps event names to shell commands to execute.
	Hooks map[string][]HookConfig `json:"hooks,omitempty"`

	// CustomSettings holds arbitrary user-defined settings.
	CustomSettings map[string]any `json:"custom_settings,omitempty"`

	// ConfigDir is the resolved path to .agentscope/ directory.
	ConfigDir string `json:"-"`
}

// HookConfig defines a shell command to run on an event.
type HookConfig struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"` // seconds, default 30
}

// instructionFiles are the candidate filenames for project instructions,
// checked in priority order.
var instructionFiles = []string{"CLAUDE.md", "AGENTS.md", "README.md"}

// LoadProjectConfig loads configuration from the given project directory.
// It looks for:
//   - CLAUDE.md or AGENTS.md in the project root for instructions
//   - .agentscope/settings.json for structured settings
//   - .agentscope/settings.local.json for local overrides (merged on top)
//
// Returns a zero-value ProjectConfig (not error) if no config files exist.
func LoadProjectConfig(projectDir string) (*ProjectConfig, error) {
	cfg := &ProjectConfig{}

	root := FindProjectRoot(projectDir)
	if root == "" {
		root = projectDir
	}

	for _, name := range instructionFiles {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("config: read %s: %w", name, err)
		}
		cfg.Instructions = string(data)
		break
	}

	configDir := filepath.Join(root, ".agentscope")
	if fi, err := os.Stat(configDir); err == nil && fi.IsDir() {
		cfg.ConfigDir = configDir
	}

	base, err := loadSettings(filepath.Join(root, ".agentscope", "settings.json"))
	if err != nil {
		return nil, err
	}
	local, err := loadSettings(filepath.Join(root, ".agentscope", "settings.local.json"))
	if err != nil {
		return nil, err
	}

	settings := mergeSettings(base, local)
	if settings == nil {
		return cfg, nil
	}

	if settings.Permissions != nil {
		for _, r := range settings.Permissions.Allow {
			cfg.PermissionRules = append(cfg.PermissionRules, permission.Rule{
				ToolName:    r.Tool,
				RuleContent: r.Content,
				Behavior:    permission.BehaviorAllow,
				Source:      "project",
			})
		}
		for _, r := range settings.Permissions.Deny {
			cfg.PermissionRules = append(cfg.PermissionRules, permission.Rule{
				ToolName:    r.Tool,
				RuleContent: r.Content,
				Behavior:    permission.BehaviorDeny,
				Source:      "project",
			})
		}
	}

	if settings.Tools != nil {
		cfg.AllowedTools = settings.Tools.Allowed
		cfg.DeniedTools = settings.Tools.Denied
	}

	cfg.ModelPreference = settings.Model
	cfg.Hooks = settings.Hooks
	cfg.CustomSettings = settings.Custom

	return cfg, nil
}

// FindProjectRoot walks up from startDir looking for a directory containing
// .agentscope/, .git/, or CLAUDE.md. Returns "" if none found.
func FindProjectRoot(startDir string) string {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		dir = startDir
	}

	for {
		if hasProjectMarker(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func hasProjectMarker(dir string) bool {
	for _, marker := range []string{".agentscope", ".git", "CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}
