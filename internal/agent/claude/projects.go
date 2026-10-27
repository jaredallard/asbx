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

package claude

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/sandbox"
)

// nonAlphanumeric matches the characters Claude Code replaces with "-"
// to name a project's directory after its working directory.
var nonAlphanumeric = regexp.MustCompile("[^a-zA-Z0-9]")

// pinMemoryLink records target as where the memory symlink of project
// points the first time it's seen, and errors if it has changed since.
// Otherwise the agent could repoint it and have the host read it on the
// next run.
func pinMemoryLink(host *sandbox.Host, project, link, target string) error {
	pin := filepath.Join(host.ConfigDir, "memory-links", project)

	pinned, err := os.ReadFile(pin) //nolint:gosec // Why: pin is in asbx's own config directory.
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(pin), 0o750); err != nil {
			return err
		}

		if err := os.WriteFile(pin, []byte(target+"\n"), 0o600); err != nil {
			return err
		}

		host.Note("pinned %s symlink to %s in %s", link, target, pin)

		return nil
	}
	if err != nil {
		return err
	}

	if was := strings.TrimRight(string(pinned), "\n"); was != target {
		return fmt.Errorf("%s now points to %s, but was pinned to %s, if you changed it delete %s and run asbx again", link, target, was, pin)
	}

	return nil
}

// mirrorProjects mounts the working directory's Claude Code sessions
// and memory read-write, so the host and the container can resume each
// other's sessions. Claude Code keeps them in a directory named after
// the working directory, which is at the same path in the container.
func mirrorProjects(host *sandbox.Host, run *docker.Run) error {
	project := nonAlphanumeric.ReplaceAllString(host.PWD, "-")
	dir := filepath.Join(host.Home, ".claude", "projects", project)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	run.Mount(dir, containerHome+"/projects/"+project, false)

	// If memory is a symlink, what it points to is mounted at the same
	// path so it resolves in the container as well.
	memory := filepath.Join(dir, "memory")
	info, err := os.Lstat(memory)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return nil
	}

	target, err := os.Readlink(memory)
	if err != nil {
		return err
	}

	if !filepath.IsAbs(target) {
		return fmt.Errorf("%s must be a symlink to an absolute path", memory)
	}

	if err := pinMemoryLink(host, project, memory, target); err != nil {
		return err
	}

	resolved, err := filepath.EvalSymlinks(memory)
	if err != nil {
		return err
	}

	run.Volume(resolved + ":" + target)

	return nil
}
