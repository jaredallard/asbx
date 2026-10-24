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
	// turn options on, see Security in the README.
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

	cfg, err := config.Load(userConfig, projectConfig)
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
