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

// Package codex runs Codex in the sandbox.
package codex

import (
	"context"
	"os"
	"path/filepath"

	"git.rgst.io/jaredallard/asbx/internal/config"
	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/sandbox"
)

// shared are the entries in Codex's home directory that are mounted.
// Unlike Claude Code's, Codex keeps every project's sessions, history
// and logs together in it, and its login in auth.json, so only what's
// known to be configuration is shared.
var shared = []string{
	"AGENTS.md",
	"AGENTS.override.md",
	"config.toml",
	"prompts",
	"rules",
	"skills",
}

// Agent implements [sandbox.Agent] for Codex.
type Agent struct{}

var _ sandbox.Agent = (*Agent)(nil)

// codexHome returns the directory Codex keeps its configuration in,
// $CODEX_HOME or ~/.codex, for the user whose home directory is home.
func codexHome(home string) string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}

	return filepath.Join(home, ".codex")
}

// mountConfig mounts the shared entries of dir, Codex's home directory,
// for the entrypoint to copy. Entries that don't exist are skipped.
func mountConfig(dir string, run *docker.Run) {
	for _, name := range shared {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			continue
		}

		run.Mount(path, sandbox.ReadOnlyHostDir+"/.codex/"+name, true)
	}
}

// Name returns "codex".
func (a *Agent) Name() string {
	return "codex"
}

// ProtectedPaths returns Codex's project configuration, which can run
// MCP servers and notify commands, and its rules, which approve
// commands.
func (a *Agent) ProtectedPaths() []string {
	return []string{".codex/config.toml", ".codex/rules"}
}

// Configure adds Codex's configuration to run.
func (a *Agent) Configure(_ context.Context, host *sandbox.Host, run *docker.Run) error {
	cfg := host.Config.Codex

	// The host's login isn't shared, since refreshing it in the container
	// would log the host out.
	if cfg.APIKeySecretRef == "" {
		host.Warn("codex.api_key_secret_ref isn't set, codex's login won't outlive the container")
	}

	mountConfig(codexHome(host.Home), run)

	return nil
}

// Secrets returns Codex's API key. The user is offered to create its
// item when it's the default.
func (a *Agent) Secrets(host *sandbox.Host) []sandbox.Secret {
	ref := host.Config.Codex.APIKeySecretRef

	return []sandbox.Secret{{
		Env:  "OPENAI_API_KEY",
		Ref:  ref,
		Name: "OpenAI API Key",
		Help: "create one at https://platform.openai.com/api-keys",
		Instructions: "Create a secret key at " +
			"https://platform.openai.com/api-keys.",
		Create: ref == config.Default().Codex.APIKeySecretRef,
	}}
}

// New returns a new instance of [Agent].
func New() *Agent {
	return &Agent{}
}
