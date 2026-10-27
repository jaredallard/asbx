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
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"git.rgst.io/jaredallard/asbx/internal/config"
	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/term"
	"git.rgst.io/jaredallard/asbx/internal/trust"
	"go.rgst.io/jaredallard/cmdexec/v2"
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

// trustedDirsDir is where, in the configuration directory, the working
// directories the user trusted are recorded.
const trustedDirsDir = "trusted-dirs"

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

// ID returns the name of the working directory with a checksum of its
// path, which names what's kept per working directory, such as its
// volumes. The checksum keeps directories with the same name from
// sharing them.
func (h *Host) ID() string {
	return fmt.Sprintf("%s-%d", filepath.Base(h.PWD), crc32.ChecksumIEEE([]byte(h.PWD)))
}

// protectPaths mounts each of paths, relative to the working directory,
// read-only if it exists. It's for files the host reads that could
// otherwise be used to run code on it, such as git hooks.
func (h *Host) protectPaths(run *docker.Run, paths ...string) {
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

	return fmt.Errorf("%q uses the old KEY=value format, which is no longer read, convert it to %q (see the README)",
		legacy, path)
}

// writeDefaultConfig writes the example configuration to path, as the
// user's configuration, if it doesn't exist. It reports whether it did.
func writeDefaultConfig(path string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}

	// O_EXCL, so a file created since asbx checked is never overwritten.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // Why: path is the user's config file.
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if _, err := f.Write(config.Example); err != nil {
		f.Close() //nolint:errcheck,gosec // Why: the write error is the one that matters.
		return false, err
	}

	return true, f.Close()
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

	// The agent is told the working directory is trusted, so it doesn't
	// ask on every run, which means asbx has to ask first. The resolved
	// path is used so that repointing a symlink asks again.
	configDir := filepath.Join(home, ".config", "asbx")
	t := term.New()
	dirs := trust.DirStore{Dir: filepath.Join(configDir, trustedDirsDir)}
	if err := dirs.Verify(workDir, t); err != nil {
		return nil, err
	}

	// The project's configuration is read after the user's so it
	// overrides it. The repository can't run code through it, but it can
	// turn options on, so it has to be trusted, see Security in the
	// README.
	root := projectRoot(ctx, workDir)
	userConfig := filepath.Join(configDir, userConfigFile)
	projectConfig := filepath.Join(root, projectConfigFile)

	if err := checkLegacyConfig(filepath.Join(configDir, "config"), userConfig); err != nil {
		return nil, err
	}

	if err := checkLegacyConfig(filepath.Join(root, ".asbx"), projectConfig); err != nil {
		return nil, err
	}

	// New users start from the example, which documents every option.
	var notices []notice
	wrote, err := writeDefaultConfig(userConfig)
	if err != nil {
		return nil, err
	}

	if wrote {
		notices = append(notices, notice{msg: "wrote the default configuration to ~/.config/asbx/" + userConfigFile})
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
		if err := store.Verify(projectConfig, projectFile.Data, t); err != nil {
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
		notices:   notices,
	}, nil
}
