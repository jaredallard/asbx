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

// Package config reads the asbx configuration files. See config.example
// for the options.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/caarlos0/env/v11"
)

// Config is the asbx configuration. Every option is off when it isn't
// set.
type Config struct {
	// ForgejoEnabled passes the host's Forgejo token and fj
	// configuration into the container.
	ForgejoEnabled bool `env:"FORGEJO_ENABLED"`

	// OPServiceAccountTokenSecretRef is the 1Password ref to a service
	// account token that op uses inside the container.
	OPServiceAccountTokenSecretRef string `env:"OP_SERVICE_ACCOUNT_TOKEN_SECRET_REF"`

	// NetworkHost runs the container on the host's network.
	NetworkHost bool `env:"NETWORK_HOST"`

	// ExtraMounts are absolute host paths mounted read-only at the same
	// path inside the container.
	ExtraMounts []string `env:"EXTRA_MOUNTS"`

	// ExtraSecretEnvVars are NAME:op://ref entries, each passed into the
	// container as the environment variable NAME.
	ExtraSecretEnvVars []string `env:"EXTRA_SECRET_ENV_VARS"`

	// Claude is the configuration specific to Claude Code.
	Claude Claude
}

// Claude is the configuration specific to Claude Code.
type Claude struct {
	// OAuthTokenSecretRef is the 1Password ref to read
	// CLAUDE_CODE_OAUTH_TOKEN from.
	OAuthTokenSecretRef string `env:"CLAUDE_CODE_OAUTH_TOKEN_SECRET_REF"`

	// LiteLLMTokenSecretRef is the 1Password ref to read LITELLM_API_KEY
	// from.
	LiteLLMTokenSecretRef string `env:"LITELLM_TOKEN_SECRET_REF"`

	// MirrorManagedSettings mounts the host's managed-settings.json.
	MirrorManagedSettings bool `env:"CLAUDE_MIRROR_MANAGED_SETTINGS"`

	// MirrorProjects mounts the working directory's Claude sessions and
	// memory read-write.
	MirrorProjects bool `env:"CLAUDE_MIRROR_PROJECTS"`
}

// Patterns for the KEY=value lines of a configuration file.
var (
	linePattern         = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
	doubleQuotedPattern = regexp.MustCompile(`^"([^"]*)"$`)
	singleQuotedPattern = regexp.MustCompile(`^'([^']*)'$`)
	unquotedPattern     = regexp.MustCompile(`^[^\s"']*$`)
)

// readFile sets values from the configuration file at path, if it
// exists. It's parsed rather than sourced, so it can't run code: every
// line is a comment or KEY=value, and values are literal, optionally
// wrapped in quotes. Only the keys in keys are allowed.
func readFile(path string, keys map[string]bool, values map[string]string) error {
	f, err := os.Open(path) //nolint:gosec // Why: reading the given config file is the point.
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		match := linePattern.FindStringSubmatch(line)
		if match == nil {
			return fmt.Errorf("%s:%d: expected KEY=value", path, n)
		}

		key, value := match[1], match[2]
		if !keys[key] {
			return fmt.Errorf("%s:%d: unknown option %s", path, n, key)
		}

		if m := doubleQuotedPattern.FindStringSubmatch(value); m != nil {
			value = m[1]
		} else if m := singleQuotedPattern.FindStringSubmatch(value); m != nil {
			value = m[1]
		} else if !unquotedPattern.MatchString(value) {
			return fmt.Errorf("%s:%d: %s must be quoted when it has whitespace or quotes", path, n, key)
		}

		values[key] = value
	}

	return scanner.Err()
}

// Load reads the configuration files at paths, in order, so that later
// files override earlier ones. Files that don't exist are skipped. The
// process environment is never read.
func Load(paths ...string) (*Config, error) {
	params, err := env.GetFieldParams(&Config{})
	if err != nil {
		return nil, err
	}

	keys := make(map[string]bool)
	for _, p := range params {
		keys[p.Key] = true
	}

	values := make(map[string]string)
	for _, path := range paths {
		if err := readFile(path, keys, values); err != nil {
			return nil, err
		}
	}

	// An empty value means the option isn't set, so a later file can
	// turn off an option an earlier one set.
	for key, value := range values {
		if value == "" {
			delete(values, key)
		}
	}

	cfg, err := env.ParseAsWithOptions[Config](env.Options{Environment: values})
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}
