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

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
	"gotest.tools/v3/assert"
)

// configFiles returns each of contents as a configuration file, named in
// order.
func configFiles(contents ...string) []File {
	var files []File
	for i, content := range contents {
		path := string(rune('a'+i)) + ".yaml"
		files = append(files, File{Path: path, Data: []byte(content)})
	}

	return files
}

// TestReadFile verifies that a configuration file is read with the path
// it was read from.
func TestReadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	assert.NilError(t, os.WriteFile(path, []byte("network_host: true\n"), 0o600))

	got, err := ReadFile(path)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, File{Path: path, Data: []byte("network_host: true\n")})
}

// TestReadFileMissing verifies that a configuration file that doesn't
// exist is read as empty rather than treated as an error.
func TestReadFileMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	got, err := ReadFile(path)
	assert.NilError(t, err)
	assert.Equal(t, got.Path, path)
	assert.Equal(t, len(got.Data), 0)
}

// TestLoad verifies that configuration files are decoded and merged,
// with later files overriding the options they set.
func TestLoad(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  Config
	}{
		{
			name:  "Empty",
			files: []string{"", "# only a comment\n"},
			want:  Config{ForgejoEnabled: true, GitHubEnabled: true},
		},
		{
			name:  "Comments",
			files: []string{"# a comment\n\nnetwork_host: true # trailing\n"},
			want:  Config{ForgejoEnabled: true, GitHubEnabled: true, NetworkHost: true},
		},
		{
			name: "AllOptions",
			files: []string{`
forgejo_enabled: true
github_enabled: true
network_host: true
op_allowed_secret_refs:
  - op://x/y/*
  - op://x/z/w
extra_mounts: [/a, /b]
extra_secret_env_vars:
  GITHUB_TOKEN: op://a b/c:d/e
claude:
  oauth_token_secret_ref: op://f/g
  litellm_token_secret_ref: 'op://"h"/i'
  mirror_managed_settings: true
  mirror_projects: true
`},
			want: Config{
				ForgejoEnabled:      true,
				GitHubEnabled:       true,
				NetworkHost:         true,
				OPAllowedSecretRefs: []string{"op://x/y/*", "op://x/z/w"},
				ExtraMounts:         []string{"/a", "/b"},
				ExtraSecretEnvVars:  map[string]string{"GITHUB_TOKEN": "op://a b/c:d/e"},
				Claude: Claude{
					OAuthTokenSecretRef:   "op://f/g",
					LiteLLMTokenSecretRef: `op://"h"/i`,
					MirrorManagedSettings: true,
					MirrorProjects:        true,
				},
			},
		},
		{
			name: "LaterFileOverrides",
			files: []string{
				"network_host: true\nforgejo_enabled: false\n",
				"network_host: false\n",
			},
			want: Config{GitHubEnabled: true},
		},
		{
			name:  "DefaultsTurnedOff",
			files: []string{"forgejo_enabled: false\ngithub_enabled: false\n"},
			want:  Config{},
		},
		{
			name: "LaterNestedOptionOverrides",
			files: []string{
				"claude:\n  mirror_projects: true\n  oauth_token_secret_ref: op://a/b/c\n",
				"claude:\n  oauth_token_secret_ref: op://d/e/f\n",
			},
			want: Config{
				ForgejoEnabled: true,
				GitHubEnabled:  true,
				Claude:         Claude{MirrorProjects: true, OAuthTokenSecretRef: "op://d/e/f"},
			},
		},
		{
			name: "LaterListReplaces",
			files: []string{
				"extra_mounts: [/a, /b]\n",
				"extra_mounts: [/c]\n",
			},
			want: Config{ForgejoEnabled: true, GitHubEnabled: true, ExtraMounts: []string{"/c"}},
		},
		{
			name: "SecretsMergeByName",
			files: []string{
				"extra_secret_env_vars:\n  A: op://a/a/a\n  B: op://b/b/b\n",
				"extra_secret_env_vars:\n  B: ''\n  C: op://c/c/c\n",
			},
			want: Config{
				ForgejoEnabled:     true,
				GitHubEnabled:      true,
				ExtraSecretEnvVars: map[string]string{"A": "op://a/a/a", "B": "", "C": "op://c/c/c"},
			},
		},
		{
			name: "EmptyValuesUnset",
			files: []string{
				"extra_mounts: [/a]\nextra_secret_env_vars: {A: op://a/a/a}\nclaude:\n  oauth_token_secret_ref: op://a/b/c\n",
				"extra_mounts: null\nextra_secret_env_vars: ~\nclaude:\n  oauth_token_secret_ref: ''\n",
			},
			want: Config{ForgejoEnabled: true, GitHubEnabled: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(configFiles(tt.files...)...)
			assert.NilError(t, err)
			assert.DeepEqual(t, *got, tt.want)
		})
	}
}

// TestLoadMissingFiles verifies that configuration files that don't
// exist, read as empty, leave the defaults.
func TestLoadMissingFiles(t *testing.T) {
	got, err := Load(File{Path: "missing.yaml"})
	assert.NilError(t, err)
	assert.DeepEqual(t, *got, Config{ForgejoEnabled: true, GitHubEnabled: true})
}

// TestLoadErrors verifies that malformed configuration is rejected
// with the file and line it was found on.
func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "UnknownOption",
			content: "network_host: true\nnetwork_hots: true\n",
			want:    "line 2: field network_hots not found",
		},
		{
			// The old KEY=value names aren't accepted as YAML keys.
			name:    "OldOptionName",
			content: "NETWORK_HOST: true\n",
			want:    "line 1: field NETWORK_HOST not found",
		},
		{
			// Replaced by op_allowed_secret_refs, so it's an error rather
			// than silently doing nothing.
			name:    "RemovedServiceAccountOption",
			content: "OP_SERVICE_ACCOUNT_TOKEN_SECRET_REF: op://a/b/c\n",
			want:    "line 1: field OP_SERVICE_ACCOUNT_TOKEN_SECRET_REF not found",
		},
		{
			name:    "UnknownNestedOption",
			content: "claude:\n  mirror_project: true\n",
			want:    "line 2: field mirror_project not found",
		},
		{
			name:    "OldFormat",
			content: "NETWORK_HOST=true\n",
			want:    "cannot unmarshal !!str `NETWORK...` into config.Config",
		},
		{
			name:    "Malformed",
			content: "extra_mounts: [/a\n",
			want:    "line 1",
		},
		{
			name:    "ListForScalar",
			content: "claude:\n  oauth_token_secret_ref: [op://a/b/c]\n",
			want:    "line 2: cannot unmarshal !!seq into string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(configFiles(tt.content)...)
			assert.ErrorContains(t, err, "a.yaml: ")
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

// TestLoadInvalidValue verifies that a value that doesn't decode as its
// option's type, such as a non-boolean network_host, is rejected.
func TestLoadInvalidValue(t *testing.T) {
	_, err := Load(configFiles("network_host: maybe\n")...)

	var typeErr *yaml.TypeError
	assert.Assert(t, errors.As(err, &typeErr), "Load() = %v, want *yaml.TypeError", err)
}
