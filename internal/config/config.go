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
// embed/config.example.yaml for the options.
package config

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"go.yaml.in/yaml/v3"
)

// Example is the example configuration, which documents every option.
// It's written as the user's configuration when they don't have one.
//
//go:embed embed/config.example.yaml
var Example []byte

// Config is the asbx configuration. Every option is off when it isn't
// set, except those [Default] sets.
type Config struct {
	// Agent is the name of the agent asbx runs when it isn't given one,
	// such as "claude" or "codex".
	Agent string `yaml:"agent"`

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

	// Clipboard is how the container's clipboard tools, such as wl-copy
	// and xclip, reach the host's clipboard: one of [ClipboardOff],
	// [ClipboardCopy] or [ClipboardCopyPaste].
	Clipboard string `yaml:"clipboard"`

	// OpenURLsEnabled lets xdg-open inside the container open http and
	// https URLs in the host's browser.
	OpenURLsEnabled bool `yaml:"open_urls_enabled"`

	// NetworkHost runs the container on the host's network.
	NetworkHost bool `yaml:"network_host"`

	// PodmanEnabled lets the agent run containers with rootless Podman
	// inside the container.
	PodmanEnabled bool `yaml:"podman_enabled"`

	// ExtraMounts are absolute host paths mounted read-only at the same
	// path inside the container.
	ExtraMounts []string `yaml:"extra_mounts"`

	// ExtraSecretEnvVars maps environment variable names to the op://
	// refs whose secrets are passed into the container as them. An empty
	// ref skips the variable, so a later file can turn one off.
	ExtraSecretEnvVars map[string]string `yaml:"extra_secret_env_vars"`

	// Claude is the configuration specific to Claude Code.
	Claude Claude `yaml:"claude"`

	// Codex is the configuration specific to Codex.
	Codex Codex `yaml:"codex"`
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
	// memory read-write. It only applies when Home is [ClaudeHomeHost].
	MirrorProjects bool `yaml:"mirror_projects"`

	// Home is where Claude Code's state in the container is kept, one of
	// [ClaudeHomeHost], [ClaudeHomeProject] or [ClaudeHomeASBX].
	Home string `yaml:"home"`
}

// The values of [Claude.Home].
const (
	// ClaudeHomeHost copies the host's ~/.claude configuration into the
	// container, which doesn't outlive it.
	ClaudeHomeHost = "host"

	// ClaudeHomeProject keeps Claude Code's state in a volume per
	// working directory, starting empty.
	ClaudeHomeProject = "project"

	// ClaudeHomeASBX keeps Claude Code's state in a directory asbx owns,
	// shared by every project that uses it, starting empty.
	ClaudeHomeASBX = "asbx"
)

// The values of [Config.Clipboard].
const (
	// ClipboardOff doesn't forward the clipboard.
	ClipboardOff = "off"

	// ClipboardCopy lets the container write to the host's clipboard,
	// but not read it.
	ClipboardCopy = "copy"

	// ClipboardCopyPaste lets the container write to and read from the
	// host's clipboard.
	ClipboardCopyPaste = "copy_paste"
)

// Codex is the configuration specific to Codex.
type Codex struct {
	// APIKeySecretRef is the 1Password ref to read OPENAI_API_KEY from,
	// which Codex logs in with.
	APIKeySecretRef string `yaml:"api_key_secret_ref"`
}

// Default returns the configuration before any file is read.
func Default() Config {
	// Passing through the host's logins is skipped when it isn't logged
	// in, so it's safe to have on unless a file turns it off. Opening
	// URLs is how agents log in and show links, and is skipped when the
	// host can't. Claude Code was the only agent before there was a
	// choice.
	return Config{
		Agent:           "claude",
		ForgejoEnabled:  true,
		GitHubEnabled:   true,
		Clipboard:       ClipboardCopy,
		OpenURLsEnabled: true,
		//nolint:gosec // Why: these are where the secrets are, not secrets.
		Claude: Claude{
			OAuthTokenSecretRef: "op://Private/claude-token/credential",
			Home:                ClaudeHomeHost,
		},
		//nolint:gosec // Why: these are where the secrets are, not secrets.
		Codex: Codex{APIKeySecretRef: "op://Private/codex-token/credential"},
	}
}

// File is a configuration file's contents and the path they were read
// from, which errors are reported against.
type File struct {
	// Path is where Data was read from.
	Path string

	// Data is the file's contents, empty when it doesn't exist.
	Data []byte
}

// ReadFile reads the configuration file at path. A file that doesn't
// exist is read as empty, so it sets nothing.
func ReadFile(path string) (File, error) {
	data, err := os.ReadFile(path) //nolint:gosec // Why: reading the given config file is the point.
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return File{}, err
	}

	return File{Path: path, Data: data}, nil
}

// decode decodes file into cfg. Only the options it sets are changed.
// Unknown options are errors, so a typo can't silently leave an option
// off.
func decode(file File, cfg *Config) error {
	dec := yaml.NewDecoder(bytes.NewReader(file.Data))
	dec.KnownFields(true)

	// An empty file, or one that's only comments, sets nothing.
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", file.Path, err)
	}

	return nil
}

// Load decodes the configuration files, in order, over [Default], so
// that later files override earlier ones. Empty files, such as ones that don't exist, are
// skipped.
//
// Each option a later file sets replaces the earlier value, including
// lists, except for extra_secret_env_vars, which is merged by name. A
// later file turns an option off by setting it to false, an empty
// string or, for lists and extra_secret_env_vars, null.
func Load(files ...File) (*Config, error) {
	cfg := Default()
	for _, file := range files {
		if err := decode(file, &cfg); err != nil {
			return nil, err
		}
	}

	switch cfg.Clipboard {
	case ClipboardOff, ClipboardCopy, ClipboardCopyPaste:
	default:
		return nil, fmt.Errorf("invalid clipboard %q, must be %q, %q or %q",
			cfg.Clipboard, ClipboardOff, ClipboardCopy, ClipboardCopyPaste)
	}

	return &cfg, nil
}
