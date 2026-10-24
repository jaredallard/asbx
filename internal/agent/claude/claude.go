// Copyright (C) 2026 asbx contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// SPDX-License-Identifier: GPL-3.0

// Package claude runs Claude Code in the sandbox.
package claude

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/sandbox"
)

// private are the entries in ~/.claude that hold other projects'
// sessions, memory or history, so they aren't mounted.
var private = map[string]bool{
	"projects":        true,
	"history.jsonl":   true,
	"file-history":    true,
	"paste-cache":     true,
	"session-env":     true,
	"sessions":        true,
	"shell-snapshots": true,
	"jobs":            true,
	"plans":           true,
	"backups":         true,
	"debug":           true,
	"daemon":          true,
	"daemon.log":      true,
	"feedback":        true,
	"ide":             true,
}

// Agent implements [sandbox.Agent] for Claude code.
type Agent struct{}

var _ sandbox.Agent = (*Agent)(nil)

// managedSettingsPath returns where Claude Code reads its managed
// settings from for the given GOOS.
func managedSettingsPath(goos string) (string, error) {
	switch goos {
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json", nil
	case "linux":
		return "/etc/claude-code/managed-settings.json", nil
	case "windows":
		return `C:\Program Files\ClaudeCode\managed-settings.json`, nil
	default:
		return "", fmt.Errorf("claude.mirror_managed_settings isn't supported on %s", goos)
	}
}

// mountConfig mounts the host's Claude configuration for the
// entrypoint to copy, except for the private parts of it.
func mountConfig(home string, run *docker.Run) error {
	run.Mount(filepath.Join(home, ".claude.json"), sandbox.ReadOnlyHostDir+"/.claude.json", true)

	dir := filepath.Join(home, ".claude")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if private[entry.Name()] {
			continue
		}

		run.Mount(filepath.Join(dir, entry.Name()), sandbox.ReadOnlyHostDir+"/.claude/"+entry.Name(), true)
	}

	return nil
}

// Name returns "claude".
func (a *Agent) Name() string {
	return "claude"
}

// ProtectedPaths returns Claude Code's project settings and MCP
// servers, which it runs commands from.
func (a *Agent) ProtectedPaths() []string {
	return []string{".claude/settings.json", ".claude/settings.local.json", ".mcp.json"}
}

// Configure adds Claude Code's secrets and configuration to run.
func (a *Agent) Configure(ctx context.Context, host *sandbox.Host, run *docker.Run) error {
	cfg := host.Config.Claude

	if err := sandbox.AddSecrets(ctx, run,
		sandbox.Secret{
			Env:  "LITELLM_API_KEY",
			Ref:  cfg.LiteLLMTokenSecretRef,
			Name: "LiteLLM API Key",
			Help: "create a virtual key at https://litellm.koi-insen.ts.net/ui/api-keys/",
		},
		sandbox.Secret{
			Env:  "CLAUDE_CODE_OAUTH_TOKEN",
			Ref:  cfg.OAuthTokenSecretRef,
			Name: "Claude OAuth Token",
			Help: "create a new token with 'claude setup-token'",
		},
	); err != nil {
		return err
	}

	if cfg.MirrorManagedSettings {
		path, err := managedSettingsPath(runtime.GOOS)
		if err != nil {
			return err
		}

		if _, err := os.Stat(path); err != nil {
			host.Warn("claude.mirror_managed_settings is enabled but %s doesn't exist", path)
		} else {
			run.Mount(path, "/etc/claude-code/managed-settings.json", true)
		}
	}

	if err := mountConfig(host.Home, run); err != nil {
		return err
	}

	if cfg.MirrorProjects {
		return mirrorProjects(host, run)
	}

	return nil
}

// New returns a new instance of [Agent].
func New() *Agent {
	return &Agent{}
}
