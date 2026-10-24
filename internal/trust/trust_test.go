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

package trust

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/term"
	"gotest.tools/v3/assert"
)

// path is the file being trusted in the tests.
const path = "/p/.asbx.yaml"

// interactive returns a terminal that answers with answer, and the
// buffer what's shown on it is written to.
func interactive(answer string) (*term.Terminal, *strings.Builder) {
	var out strings.Builder
	return &term.Terminal{In: strings.NewReader(answer), Out: &out, Interactive: true, Accessible: true, Width: 30}, &out
}

// trustCopy writes data to store as the trusted copy of path.
func trustCopy(t *testing.T, store Store, data string) {
	t.Helper()

	if err := os.MkdirAll(store.Dir, 0o750); err != nil {
		t.Fatalf("MkdirAll(%q) = %v", store.Dir, err)
	}

	if err := os.WriteFile(store.copyPath(path), []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) = %v", store.copyPath(path), err)
	}
}

// TestVerifyTrusted verifies that a file that's unchanged since it was
// trusted is accepted without asking the user.
func TestVerifyTrusted(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	trustCopy(t, store, "network_host: true\n")

	terminal, out := interactive("")
	assert.NilError(t, store.Verify(path, []byte("network_host: true\n"), terminal))
	assert.Equal(t, out.String(), "")
}

// TestVerifyNewTrusted verifies that a new file is shown in full and,
// once the user trusts it, stored, so they aren't asked about it again.
func TestVerifyNewTrusted(t *testing.T) {
	for _, answer := range []string{"y\n", "yes\n", "Y\n", "y"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			store := Store{Dir: filepath.Join(t.TempDir(), "trusted")}

			terminal, out := interactive(answer)
			assert.NilError(t, store.Verify(path, []byte("network_host: true\n"), terminal))
			assert.Assert(t, strings.Contains(out.String(), path+" is new, review it before it's used:\n"), "Verify() wrote:\n%s", out)
			assert.Assert(t, strings.Contains(out.String(), "   1   │ network_host: true\n"), "Verify() wrote:\n%s", out)
			assert.Assert(t, strings.Contains(out.String(), "Trust this configuration? [y/N]"), "Verify() wrote:\n%s", out)

			terminal, out = interactive("")
			assert.NilError(t, store.Verify(path, []byte("network_host: true\n"), terminal))
			assert.Equal(t, out.String(), "")
		})
	}
}

// TestVerifyChangedTrusted verifies that a file that changed since it
// was trusted is shown as a diff and, once the user trusts it, stored
// as changed.
func TestVerifyChangedTrusted(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	trustCopy(t, store, "network_host: false\n")

	terminal, out := interactive("y\n")
	assert.NilError(t, store.Verify(path, []byte("network_host: true\n"), terminal))

	header := path + " is changed since it was trusted, review it before it's used:\n"
	assert.Assert(t, strings.Contains(out.String(), header), "Verify() wrote:\n%s", out)
	assert.Assert(t, strings.Contains(out.String(), "   1 - │ network_host: false\n   1 + │ network_host: true\n"), "Verify() wrote:\n%s", out)

	got, err := os.ReadFile(store.copyPath(path))
	assert.NilError(t, err)
	assert.Equal(t, string(got), "network_host: true\n")
}

// TestVerifyNewDeclined verifies that a new file the user doesn't
// trust is refused and not stored.
func TestVerifyNewDeclined(t *testing.T) {
	tests := []struct {
		name   string
		answer string
	}{
		{name: "Empty", answer: "\n"},
		{name: "No", answer: "n\n"},
		{name: "EOF", answer: ""},
		{name: "Other", answer: "yep\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := Store{Dir: t.TempDir()}

			terminal, _ := interactive(tt.answer)
			err := store.Verify(path, []byte("network_host: true\n"), terminal)
			assert.Assert(t, errors.Is(err, ErrNotTrusted), "Verify() = %v, want ErrNotTrusted", err)

			_, err = os.Stat(store.copyPath(path))
			assert.Assert(t, errors.Is(err, fs.ErrNotExist), "Stat() = %v, want fs.ErrNotExist", err)
		})
	}
}

// TestVerifyChangedDeclined verifies that a changed file the user
// doesn't trust is refused, and what they trusted before is kept.
func TestVerifyChangedDeclined(t *testing.T) {
	tests := []struct {
		name   string
		answer string
	}{
		{name: "No", answer: "no\n"},
		{name: "EOF", answer: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := Store{Dir: t.TempDir()}
			trustCopy(t, store, "network_host: false\n")

			terminal, _ := interactive(tt.answer)
			err := store.Verify(path, []byte("network_host: true\n"), terminal)
			assert.Assert(t, errors.Is(err, ErrNotTrusted), "Verify() = %v, want ErrNotTrusted", err)

			got, err := os.ReadFile(store.copyPath(path))
			assert.NilError(t, err)
			assert.Equal(t, string(got), "network_host: false\n")
		})
	}
}

// notInteractive returns a terminal with no user to ask, and the buffer
// what's shown on it is written to.
func notInteractive() (*term.Terminal, *strings.Builder) {
	var out strings.Builder
	return &term.Terminal{In: strings.NewReader("y\n"), Out: &out}, &out
}

// TestVerifyNewNotInteractive verifies that, without a user to ask, a
// new file is refused without being shown or stored.
func TestVerifyNewNotInteractive(t *testing.T) {
	store := Store{Dir: t.TempDir()}

	terminal, out := notInteractive()
	err := store.Verify(path, []byte("network_host: true\n"), terminal)
	assert.Assert(t, errors.Is(err, ErrNotTrusted), "Verify() = %v, want ErrNotTrusted", err)
	assert.ErrorContains(t, err, path+" is new, run asbx in a terminal to review it")
	assert.Equal(t, out.String(), "")

	_, err = os.Stat(store.copyPath(path))
	assert.Assert(t, errors.Is(err, fs.ErrNotExist), "Stat() = %v, want fs.ErrNotExist", err)
}

// TestVerifyChangedNotInteractive verifies that, without a user to ask,
// a changed file is refused without being shown or stored.
func TestVerifyChangedNotInteractive(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	trustCopy(t, store, "network_host: false\n")

	terminal, out := notInteractive()
	err := store.Verify(path, []byte("network_host: true\n"), terminal)
	assert.Assert(t, errors.Is(err, ErrNotTrusted), "Verify() = %v, want ErrNotTrusted", err)
	assert.ErrorContains(t, err, path+" is changed since it was trusted, run asbx in a terminal to review it")
	assert.Equal(t, out.String(), "")

	got, err := os.ReadFile(store.copyPath(path))
	assert.NilError(t, err)
	assert.Equal(t, string(got), "network_host: false\n")
}

// TestVerifyKeyedByPath verifies that trusting a file doesn't trust the
// same contents at another path.
func TestVerifyKeyedByPath(t *testing.T) {
	store := Store{Dir: t.TempDir()}
	trustCopy(t, store, "network_host: true\n")

	terminal, _ := interactive("")
	err := store.Verify("/other/.asbx.yaml", []byte("network_host: true\n"), terminal)
	assert.Assert(t, errors.Is(err, ErrNotTrusted), "Verify() = %v, want ErrNotTrusted", err)
}

// dirPath is the directory being trusted in the tests.
const dirPath = "/p"

// TestVerifyDirTrusted verifies that a directory that was trusted is
// accepted without asking the user.
func TestVerifyDirTrusted(t *testing.T) {
	store := DirStore{Dir: t.TempDir()}
	if err := os.WriteFile(store.recordPath(dirPath), []byte(dirPath+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) = %v", store.recordPath(dirPath), err)
	}

	terminal, out := interactive("")
	assert.NilError(t, store.Verify(dirPath, terminal))
	assert.Equal(t, out.String(), "")
}

// TestVerifyDirNewTrusted verifies that a directory the user trusts is
// recorded, with its path, so they aren't asked about it again.
func TestVerifyDirNewTrusted(t *testing.T) {
	for _, answer := range []string{"y\n", "yes\n", "Y\n", "y"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			store := DirStore{Dir: filepath.Join(t.TempDir(), "trusted")}

			terminal, out := interactive(answer)
			assert.NilError(t, store.Verify(dirPath, terminal))
			assert.Assert(t, strings.HasPrefix(out.String(), dirPath+" hasn't been trusted."), "Verify() wrote:\n%s", out)
			assert.Assert(t, strings.Contains(out.String(), "Trust this directory? [y/N]"), "Verify() wrote:\n%s", out)

			got, err := os.ReadFile(store.recordPath(dirPath))
			assert.NilError(t, err)
			assert.Equal(t, string(got), dirPath+"\n")

			terminal, out = interactive("")
			assert.NilError(t, store.Verify(dirPath, terminal))
			assert.Equal(t, out.String(), "")
		})
	}
}

// TestVerifyDirDeclined verifies that a directory the user doesn't
// trust is refused and not recorded.
func TestVerifyDirDeclined(t *testing.T) {
	tests := []struct {
		name   string
		answer string
	}{
		{name: "Empty", answer: "\n"},
		{name: "No", answer: "n\n"},
		{name: "EOF", answer: ""},
		{name: "Other", answer: "yep\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := DirStore{Dir: t.TempDir()}

			terminal, _ := interactive(tt.answer)
			err := store.Verify(dirPath, terminal)
			assert.Assert(t, errors.Is(err, ErrNotTrusted), "Verify() = %v, want ErrNotTrusted", err)

			_, err = os.Stat(store.recordPath(dirPath))
			assert.Assert(t, errors.Is(err, fs.ErrNotExist), "Stat() = %v, want fs.ErrNotExist", err)
		})
	}
}

// TestVerifyDirNotInteractive verifies that, without a user to ask, a
// directory that wasn't trusted is refused without being recorded.
func TestVerifyDirNotInteractive(t *testing.T) {
	store := DirStore{Dir: t.TempDir()}

	terminal, out := notInteractive()
	err := store.Verify(dirPath, terminal)
	assert.Assert(t, errors.Is(err, ErrNotTrusted), "Verify() = %v, want ErrNotTrusted", err)
	assert.ErrorContains(t, err, dirPath+" hasn't been trusted, run asbx in a terminal to trust it")
	assert.Equal(t, out.String(), "")

	_, err = os.Stat(store.recordPath(dirPath))
	assert.Assert(t, errors.Is(err, fs.ErrNotExist), "Stat() = %v, want fs.ErrNotExist", err)
}

// TestVerifyDirEscapesPath verifies that a directory's path is shown
// with its escape sequences escaped, so it can't hide what it is.
func TestVerifyDirEscapesPath(t *testing.T) {
	store := DirStore{Dir: t.TempDir()}

	terminal, out := interactive("n\n")
	err := store.Verify("/p\x1b[2K", terminal)
	assert.Assert(t, errors.Is(err, ErrNotTrusted), "Verify() = %v, want ErrNotTrusted", err)
	assert.Assert(t, strings.HasPrefix(out.String(), `/p\x1b[2K hasn't been trusted.`), "Verify() wrote:\n%q", out)
}
