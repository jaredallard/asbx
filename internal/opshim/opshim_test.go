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

package opshim

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// fakeOP is an [opbridge.ReadFunc] that returns the secrets in it,
// recording the refs it was asked for.
type fakeOP struct {
	secrets map[string]string
	refs    []string
}

// read implements [opbridge.ReadFunc].
func (f *fakeOP) read(_ context.Context, ref string) (string, error) {
	f.refs = append(f.refs, ref)

	value, ok := f.secrets[ref]
	if !ok {
		return "", errors.New("item not found")
	}

	return value, nil
}

// TestMaskWriter verifies that secrets are masked in output however
// it's split across writes.
func TestMaskWriter(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		want   string
	}{
		{
			name:   "WholeSecret",
			writes: []string{"token is hunter2\n"},
			want:   "token is " + concealed + "\n",
		},
		{
			name:   "SplitSecret",
			writes: []string{"token is hun", "te", "r2\n"},
			want:   "token is " + concealed + "\n",
		},
		{
			name:   "StartOfSecretOnly",
			writes: []string{"hunt", "ing\n"},
			want:   "hunting\n",
		},
		{
			name:   "SecretAtEnd",
			writes: []string{"hunter2"},
			want:   concealed,
		},
		{
			name:   "LongestSecretWins",
			writes: []string{"abc", "def"},
			want:   concealed,
		},
		{
			name:   "ShorterSecretAtEnd",
			writes: []string{"abc"},
			want:   concealed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			m := NewMaskWriter(&out, []string{"hunter2", "abc", "abcdef", ""})
			for _, w := range tt.writes {
				n, err := m.Write([]byte(w))
				assert.NilError(t, err)
				assert.Equal(t, n, len(w))
			}

			assert.NilError(t, m.Flush())
			assert.Equal(t, out.String(), tt.want)
		})
	}
}

// TestResolve verifies that only values that are op:// refs are
// replaced, with each ref read once.
func TestResolve(t *testing.T) {
	op := &fakeOP{secrets: map[string]string{"op://a/b/c": "hunter2"}}

	resolved, secrets, err := Resolve(t.Context(), op.read, []string{
		"HOME=/home/ubuntu",
		"TOKEN=op://a/b/c",
		"SAME_TOKEN=op://a/b/c",
		"URL=https://op://a/b/c",
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, resolved, []string{
		"HOME=/home/ubuntu",
		"TOKEN=hunter2",
		"SAME_TOKEN=hunter2",
		"URL=https://op://a/b/c",
	})
	assert.DeepEqual(t, secrets, []string{"hunter2"})
	assert.DeepEqual(t, op.refs, []string{"op://a/b/c"})
}

// TestResolveError verifies that a ref that can't be read fails with
// the variable it's for.
func TestResolveError(t *testing.T) {
	op := &fakeOP{}

	_, _, err := Resolve(t.Context(), op.read, []string{"TOKEN=op://a/b/c"})
	assert.ErrorContains(t, err, "unable to read TOKEN: item not found")
}

// TestParseEnvFile verifies the dotenv grammar op run's env files are
// read with.
func TestParseEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\n\nA=op://a/b/c\nexport B=\"two words\"\n  C='x=y'  \nD=\n"
	assert.NilError(t, os.WriteFile(path, []byte(content), 0o600))

	got, err := ParseEnvFile(path)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []string{"A=op://a/b/c", "B=two words", "C=x=y", "D="})
}

// TestParseEnvFileError verifies that a line that isn't NAME=value is
// rejected with where it is.
func TestParseEnvFileError(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	assert.NilError(t, os.WriteFile(path, []byte("A=1\nnot a variable\n"), 0o600))

	_, err := ParseEnvFile(path)
	assert.ErrorContains(t, err, ".env:2: expected NAME=value")
}

// TestMerge verifies that env file variables replace the environment's
// of the same name.
func TestMerge(t *testing.T) {
	got := merge([]string{"A=1", "B=2"}, []string{"B=3", "C=4"})
	assert.DeepEqual(t, got, []string{"A=1", "B=3", "C=4"})
}

// TestRead verifies that a secret is written with a newline unless
// asked not to, and that --out-file writes it to the file instead.
func TestRead(t *testing.T) {
	op := &fakeOP{secrets: map[string]string{"op://a/b/c": "hunter2"}}

	var out strings.Builder
	assert.NilError(t, Read(t.Context(), op.read, &out, "op://a/b/c", ReadOptions{}))
	assert.NilError(t, Read(t.Context(), op.read, &out, "op://a/b/c", ReadOptions{NoNewline: true}))
	assert.Equal(t, out.String(), "hunter2\nhunter2")

	path := filepath.Join(t.TempDir(), "secret")
	out.Reset()
	assert.NilError(t, Read(t.Context(), op.read, &out, "op://a/b/c", ReadOptions{OutFile: path, FileMode: 0o600}))
	assert.Equal(t, out.String(), path+"\n")

	got, err := os.ReadFile(path)
	assert.NilError(t, err)
	assert.Equal(t, string(got), "hunter2")
}

// TestReadOutFileExists verifies that --out-file doesn't overwrite a
// file unless forced to, and that forcing it sets the file's mode.
func TestReadOutFileExists(t *testing.T) {
	op := &fakeOP{secrets: map[string]string{"op://a/b/c": "hunter2"}}
	path := filepath.Join(t.TempDir(), "secret")
	assert.NilError(t, os.WriteFile(path, []byte("old"), 0o644)) //nolint:gosec // Why: checking the mode changes.

	err := Read(t.Context(), op.read, &strings.Builder{}, "op://a/b/c", ReadOptions{OutFile: path, FileMode: 0o600})
	assert.ErrorContains(t, err, "already exists, use --force")

	opts := ReadOptions{OutFile: path, FileMode: 0o600, Force: true}
	assert.NilError(t, Read(t.Context(), op.read, &strings.Builder{}, "op://a/b/c", opts))

	info, err := os.Stat(path)
	assert.NilError(t, err)
	assert.Equal(t, info.Mode().Perm(), os.FileMode(0o600))
}
