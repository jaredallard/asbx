package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/caarlos0/env/v11"
	"gotest.tools/v3/assert"
)

// writeFiles writes each of contents to its own file in a temporary
// directory and returns their paths, in order.
func writeFiles(t *testing.T, contents ...string) []string {
	t.Helper()

	dir := t.TempDir()

	var paths []string
	for i, content := range contents {
		path := filepath.Join(dir, string(rune('a'+i)))
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) = %v", path, err)
		}

		paths = append(paths, path)
	}

	return paths
}

// TestLoad verifies that configuration files are parsed with the
// shell-like KEY=value grammar and merged with later files winning.
func TestLoad(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		want  Config
	}{
		{
			name: "CommentsAndBlankLines",
			files: []string{
				"# a comment\n\n   \n  # indented comment\nNETWORK_HOST=true\n",
			},
			want: Config{NetworkHost: true},
		},
		{
			name:  "SurroundingWhitespace",
			files: []string{"  NETWORK_HOST=true  \r\n"},
			want:  Config{NetworkHost: true},
		},
		{
			name: "Quoting",
			files: []string{
				`OP_SERVICE_ACCOUNT_TOKEN_SECRET_REF="op://a b/c"` + "\n" +
					`LITELLM_TOKEN_SECRET_REF='op://"d"/e'` + "\n" +
					`CLAUDE_CODE_OAUTH_TOKEN_SECRET_REF=op://f/g`,
			},
			want: Config{
				OPServiceAccountTokenSecretRef: "op://a b/c",
				Claude: Claude{
					LiteLLMTokenSecretRef: `op://"d"/e`,
					OAuthTokenSecretRef:   "op://f/g",
				},
			},
		},
		{
			name: "Lists",
			files: []string{
				`EXTRA_MOUNTS="/a,/b"` + "\n" +
					"EXTRA_SECRET_ENV_VARS=A:op://x/y/z\n",
			},
			want: Config{
				ExtraMounts:        []string{"/a", "/b"},
				ExtraSecretEnvVars: []string{"A:op://x/y/z"},
			},
		},
		{
			name: "NestedClaudeOptions",
			files: []string{
				"CLAUDE_MIRROR_PROJECTS=true\nCLAUDE_MIRROR_MANAGED_SETTINGS=1\n",
			},
			want: Config{
				Claude: Claude{MirrorProjects: true, MirrorManagedSettings: true},
			},
		},
		{
			name: "LaterFileOverrides",
			files: []string{
				"NETWORK_HOST=true\nFORGEJO_ENABLED=true\n",
				"NETWORK_HOST=false\n",
			},
			want: Config{ForgejoEnabled: true},
		},
		{
			name: "EmptyLaterValueUnsets",
			files: []string{
				`CLAUDE_CODE_OAUTH_TOKEN_SECRET_REF="op://a/b/c"`,
				`CLAUDE_CODE_OAUTH_TOKEN_SECRET_REF=""`,
			},
			want: Config{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(writeFiles(t, tt.files...)...)
			assert.NilError(t, err)
			assert.DeepEqual(t, *got, tt.want)
		})
	}
}

// TestLoadMissingFiles verifies that configuration files that don't
// exist are skipped rather than treated as errors.
func TestLoadMissingFiles(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "missing"))
	assert.NilError(t, err)
	assert.DeepEqual(t, *got, Config{})
}

// TestLoadIgnoresEnvironment verifies that options set in the process
// environment have no effect, so an exported variable can't quietly
// enable an option.
func TestLoadIgnoresEnvironment(t *testing.T) {
	t.Setenv("NETWORK_HOST", "true")
	t.Setenv("EXTRA_MOUNTS", "/etc")

	got, err := Load(writeFiles(t, "FORGEJO_ENABLED=true\n")...)
	assert.NilError(t, err)
	assert.DeepEqual(t, *got, Config{ForgejoEnabled: true})
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
			name:    "NotKeyValue",
			content: "# ok\nexport NETWORK_HOST=true\n",
			want:    ":2: expected KEY=value",
		},
		{
			name:    "UnknownOption",
			content: "PATH=/tmp\n",
			want:    ":1: unknown option PATH",
		},
		{
			name:    "UnquotedWhitespace",
			content: "EXTRA_MOUNTS=/a /b\n",
			want:    ":1: EXTRA_MOUNTS must be quoted when it has whitespace or quotes",
		},
		{
			name:    "UnquotedQuote",
			content: `EXTRA_MOUNTS=/a"b` + "\n",
			want:    ":1: EXTRA_MOUNTS must be quoted when it has whitespace or quotes",
		},
		{
			name:    "MismatchedQuotes",
			content: `EXTRA_MOUNTS="/a'` + "\n",
			want:    ":1: EXTRA_MOUNTS must be quoted when it has whitespace or quotes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := writeFiles(t, tt.content)

			_, err := Load(paths...)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

// TestLoadInvalidValue verifies that a value that doesn't parse as its
// option's type, such as a non-boolean NETWORK_HOST, is rejected.
func TestLoadInvalidValue(t *testing.T) {
	_, err := Load(writeFiles(t, "NETWORK_HOST=yes\n")...)

	var parseErr env.ParseError
	assert.Assert(t, errors.As(err, &parseErr), "Load() = %v, want env.ParseError", err)
	assert.Equal(t, parseErr.Name, "NetworkHost")
}
