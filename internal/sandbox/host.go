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

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"git.rgst.io/jaredallard/asbx/internal/config"
	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/trust"
	"go.rgst.io/jaredallard/cmdexec/v2"
	"golang.org/x/term"
)

// The configuration files, which are read in this order.
const (
	// userConfigFile is the user's configuration, in the config
	// directory.
	userConfigFile = "config.yaml"

	// projectConfigFile is the project's configuration, at its root.
	projectConfigFile = ".asbx.yaml"
)

// trustedConfigsDir is where, in the configuration directory, the
// project configuration files the user trusted are kept.
const trustedConfigsDir = "trusted-configs"

// Host is where asbx is running: the paths the sandbox is built from
// and the configuration that applies to them.
type Host struct {
	// Home is the user's home directory, as given by $HOME.
	Home string

	// PWD is the working directory as the user sees it, which may go
	// through symlinks. The container sees it at the same path.
	PWD string

	// WorkDir is PWD with symlinks resolved.
	WorkDir string

	// ConfigDir is asbx's configuration directory.
	ConfigDir string

	// Config is the user's configuration overridden by the project's.
	Config *config.Config

	// notices are the messages for the user from setting up the sandbox,
	// shown when it starts.
	notices []notice
}

// notice is a message for the user about how the sandbox was set up.
type notice struct {
	// warn is whether it's a warning, which needs the user's attention,
	// rather than a note.
	warn bool

	// msg is the message, as a lowercase phrase.
	msg string
}

// Warn adds a warning, formatted like [fmt.Sprintf], to show the user
// when the sandbox starts.
func (h *Host) Warn(format string, args ...any) {
	h.notices = append(h.notices, notice{warn: true, msg: fmt.Sprintf(format, args...)})
}

// Note adds a note, formatted like [fmt.Sprintf], to show the user when
// the sandbox starts.
func (h *Host) Note(format string, args ...any) {
	h.notices = append(h.notices, notice{msg: fmt.Sprintf(format, args...)})
}

// ProtectPaths mounts each of paths, relative to the working directory,
// read-only if it exists. It's for files the host reads that could
// otherwise be used to run code on it, such as git hooks.
func (h *Host) ProtectPaths(run *docker.Run, paths ...string) {
	for _, path := range paths {
		source := filepath.Join(h.WorkDir, path)
		if _, err := os.Stat(source); err != nil {
			continue
		}

		run.Mount(source, filepath.Join(h.PWD, path), true)
	}
}

// unsafeWorkDir reports whether the agent, which can write anything
// under workDir, would get write access to realHome or its config:
// workDir is /, realHome or one of its parents, or in a dot-directory
// of realHome.
func unsafeWorkDir(workDir, realHome string) bool {
	return workDir == "/" ||
		strings.HasPrefix(realHome+"/", workDir+"/") ||
		strings.HasPrefix(workDir, realHome+"/.")
}

// projectRoot returns the root of the git repository workDir is in, or
// workDir outside of one.
func projectRoot(ctx context.Context, workDir string) string {
	out, err := cmdexec.CommandContext(ctx, "git", "-C", workDir, "rev-parse", "--show-toplevel").Output()
	root := strings.TrimRight(string(out), "\n")
	if err != nil || root == "" {
		return workDir
	}

	return root
}

// checkLegacyConfig returns an error if legacy, a configuration file in
// the KEY=value format that path replaced, exists. It's no longer read,
// so its options would otherwise stop applying without anyone noticing.
func checkLegacyConfig(legacy, path string) error {
	_, err := os.Lstat(legacy)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	return fmt.Errorf("%q uses the old KEY=value format, which is no longer read, convert it to %q (see config.example.yaml)", legacy, path)
}

// terminal returns the terminal asbx is running in, for asking the user
// to trust a configuration file. It's only interactive when both stdin
// and stderr are terminals. Colors follow NO_COLOR and COLORTERM.
func terminal() trust.Terminal {
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stderr.Fd()))

	width, _, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil {
		// The frame falls back to a default width.
		width = 0
	}

	colorTerm := os.Getenv("COLORTERM")

	return trust.Terminal{
		In:          os.Stdin,
		Out:         os.Stderr,
		Interactive: interactive,
		Color:       interactive && os.Getenv("NO_COLOR") == "",
		TrueColor:   colorTerm == "truecolor" || colorTerm == "24bit",
		Width:       width,
	}
}

// NewHost resolves the host asbx is running on and reads its
// configuration. It refuses working directories where the agent could
// write to the user's home directory or its config.
func NewHost(ctx context.Context) (*Host, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return nil, err
	}

	pwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	workDir, err := filepath.EvalSymlinks(pwd)
	if err != nil {
		return nil, err
	}

	if unsafeWorkDir(workDir, realHome) {
		return nil, fmt.Errorf("refusing to run in %s, it would give the agent write access to your home directory or its config", pwd)
	}

	// The project's configuration is read after the user's so it
	// overrides it. The repository can't run code through it, but it can
	// turn options on, so it has to be trusted, see Security in the
	// README.
	configDir := filepath.Join(home, ".config", "asbx")
	root := projectRoot(ctx, workDir)
	userConfig := filepath.Join(configDir, userConfigFile)
	projectConfig := filepath.Join(root, projectConfigFile)

	if err := checkLegacyConfig(filepath.Join(configDir, "config"), userConfig); err != nil {
		return nil, err
	}

	if err := checkLegacyConfig(filepath.Join(root, ".asbx"), projectConfig); err != nil {
		return nil, err
	}

	userFile, err := config.ReadFile(userConfig)
	if err != nil {
		return nil, err
	}

	projectFile, err := config.ReadFile(projectConfig)
	if err != nil {
		return nil, err
	}

	// The project's configuration can come from anyone with access to
	// the repository, or the agent if it didn't exist, so the user has to
	// trust it first. The contents that were trusted are what's loaded.
	if len(projectFile.Data) > 0 {
		store := trust.Store{Dir: filepath.Join(configDir, trustedConfigsDir)}
		if err := store.Verify(projectConfig, projectFile.Data, terminal()); err != nil {
			return nil, err
		}
	}

	cfg, err := config.Load(userFile, projectFile)
	if err != nil {
		return nil, err
	}

	return &Host{
		Home:      home,
		PWD:       pwd,
		WorkDir:   workDir,
		ConfigDir: configDir,
		Config:    cfg,
	}, nil
}
