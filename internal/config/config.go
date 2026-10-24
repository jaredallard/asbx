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

// Package config reads the asbx configuration files. See
// config.example.yaml for the options.
package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"go.yaml.in/yaml/v3"
)

// Config is the asbx configuration. Every option is off when it isn't
// set, except ForgejoEnabled and GitHubEnabled, which are on.
type Config struct {
	// ForgejoEnabled passes the host's Forgejo token and fj
	// configuration into the container, if fj is logged in.
	ForgejoEnabled bool `yaml:"forgejo_enabled"`

	// GitHubEnabled passes the host's GitHub token, from gh, into the
	// container as GH_TOKEN, if gh is logged in.
	GitHubEnabled bool `yaml:"github_enabled"`

	// OPAllowedSecretRefs are the op:// refs, where * matches within a
	// path segment and /**/ across them, that op inside the container
	// may read through the host's op.
	OPAllowedSecretRefs []string `yaml:"op_allowed_secret_refs"`

	// NetworkHost runs the container on the host's network.
	NetworkHost bool `yaml:"network_host"`

	// ExtraMounts are absolute host paths mounted read-only at the same
	// path inside the container.
	ExtraMounts []string `yaml:"extra_mounts"`

	// ExtraSecretEnvVars maps environment variable names to the op://
	// refs whose secrets are passed into the container as them. An empty
	// ref skips the variable, so a later file can turn one off.
	ExtraSecretEnvVars map[string]string `yaml:"extra_secret_env_vars"`

	// Claude is the configuration specific to Claude Code.
	Claude Claude `yaml:"claude"`
}

// Claude is the configuration specific to Claude Code.
type Claude struct {
	// OAuthTokenSecretRef is the 1Password ref to read
	// CLAUDE_CODE_OAUTH_TOKEN from.
	OAuthTokenSecretRef string `yaml:"oauth_token_secret_ref"`

	// LiteLLMTokenSecretRef is the 1Password ref to read LITELLM_API_KEY
	// from.
	LiteLLMTokenSecretRef string `yaml:"litellm_token_secret_ref"`

	// MirrorManagedSettings mounts the host's managed-settings.json.
	MirrorManagedSettings bool `yaml:"mirror_managed_settings"`

	// MirrorProjects mounts the working directory's Claude sessions and
	// memory read-write.
	MirrorProjects bool `yaml:"mirror_projects"`
}

// readFile decodes the configuration file at path, if it exists, into
// cfg. Only the options it sets are changed. Unknown options are
// errors, so a typo can't silently leave an option off.
func readFile(path string, cfg *Config) error {
	f, err := os.Open(path) //nolint:gosec // Why: reading the given config file is the point.
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)

	// An empty file, or one that's only comments, sets nothing.
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", path, err)
	}

	return nil
}

// Load reads the configuration files at paths, in order, so that later
// files override earlier ones. Files that don't exist are skipped.
//
// Each option a later file sets replaces the earlier value, including
// lists, except for extra_secret_env_vars, which is merged by name. A
// later file turns an option off by setting it to false, an empty
// string or, for lists and extra_secret_env_vars, null.
func Load(paths ...string) (*Config, error) {
	// Passing through the host's logins is skipped when it isn't logged
	// in, so it's safe to have on unless a file turns it off.
	cfg := Config{ForgejoEnabled: true, GitHubEnabled: true}
	for _, path := range paths {
		if err := readFile(path, &cfg); err != nil {
			return nil, err
		}
	}

	return &cfg, nil
}
