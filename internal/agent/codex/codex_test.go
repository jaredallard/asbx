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

package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/sandbox"
	"gotest.tools/v3/assert"
)

// mountedCodexEntries returns the names of the Codex home directory
// entries args mounts.
func mountedCodexEntries(args []string) []string {
	prefix := "target=" + sandbox.ReadOnlyHostDir + "/.codex/"

	var names []string
	for _, arg := range args {
		if _, target, ok := strings.Cut(arg, prefix); ok {
			names = append(names, strings.TrimSuffix(target, ",readonly"))
		}
	}

	return names
}

// TestMountConfig verifies that only Codex's configuration is mounted,
// not its login or any project's sessions, history or logs.
func TestMountConfig(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"prompts", "sessions", "log"} {
		assert.NilError(t, os.MkdirAll(filepath.Join(dir, name), 0o750))
	}
	for _, name := range []string{"config.toml", "AGENTS.md", "auth.json", "history.jsonl"} {
		assert.NilError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
	}

	run := &docker.Run{}
	mountConfig(dir, run)
	assert.DeepEqual(t, mountedCodexEntries(run.Args()), []string{"AGENTS.md", "config.toml", "prompts"})
}

// TestMountConfigMissing verifies that a missing Codex home directory
// mounts nothing.
func TestMountConfigMissing(t *testing.T) {
	run := &docker.Run{}
	mountConfig(filepath.Join(t.TempDir(), "missing"), run)
	assert.Equal(t, len(mountedCodexEntries(run.Args())), 0)
}

// TestCodexHome verifies that CODEX_HOME, when set, is used over
// ~/.codex, as Codex itself does.
func TestCodexHome(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	assert.Equal(t, codexHome("/home/u"), "/home/u/.codex")

	t.Setenv("CODEX_HOME", "/elsewhere")
	assert.Equal(t, codexHome("/home/u"), "/elsewhere")
}
