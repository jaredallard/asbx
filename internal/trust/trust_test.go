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

	"gotest.tools/v3/assert"
)

// path is the file being trusted in the tests.
const path = "/p/.asbx.yaml"

// interactive returns a terminal that answers with answer, and the
// buffer what's shown on it is written to.
func interactive(answer string) (Terminal, *strings.Builder) {
	var out strings.Builder
	return Terminal{In: strings.NewReader(answer), Out: &out, Interactive: true, Width: 30}, &out
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

	term, out := interactive("")
	assert.NilError(t, store.Verify(path, []byte("network_host: true\n"), term))
	assert.Equal(t, out.String(), "")
}

// TestVerifyNewTrusted verifies that a new file is shown in full and,
// once the user trusts it, stored, so they aren't asked about it again.
func TestVerifyNewTrusted(t *testing.T) {
	for _, answer := range []string{"y\n", "yes\n", "Y\n", " YES \n", "y"} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			store := Store{Dir: filepath.Join(t.TempDir(), "trusted")}

			term, out := interactive(answer)
			assert.NilError(t, store.Verify(path, []byte("network_host: true\n"), term))
			assert.Assert(t, strings.Contains(out.String(), path+" is new, review it before it's used:\n"), "Verify() wrote:\n%s", out)
			assert.Assert(t, strings.Contains(out.String(), "   1   │ network_host: true\n"), "Verify() wrote:\n%s", out)
			assert.Assert(t, strings.HasSuffix(out.String(), "Trust this configuration? [y/N] "), "Verify() wrote:\n%s", out)

			term, out = interactive("")
			assert.NilError(t, store.Verify(path, []byte("network_host: true\n"), term))
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

	term, out := interactive("y\n")
	assert.NilError(t, store.Verify(path, []byte("network_host: true\n"), term))

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

			term, _ := interactive(tt.answer)
			err := store.Verify(path, []byte("network_host: true\n"), term)
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

			term, _ := interactive(tt.answer)
			err := store.Verify(path, []byte("network_host: true\n"), term)
			assert.Assert(t, errors.Is(err, ErrNotTrusted), "Verify() = %v, want ErrNotTrusted", err)

			got, err := os.ReadFile(store.copyPath(path))
			assert.NilError(t, err)
			assert.Equal(t, string(got), "network_host: false\n")
		})
	}
}

// notInteractive returns a terminal with no user to ask, and the buffer
// what's shown on it is written to.
func notInteractive() (Terminal, *strings.Builder) {
	var out strings.Builder
	return Terminal{In: strings.NewReader("y\n"), Out: &out}, &out
}

// TestVerifyNewNotInteractive verifies that, without a user to ask, a
// new file is refused without being shown or stored.
func TestVerifyNewNotInteractive(t *testing.T) {
	store := Store{Dir: t.TempDir()}

	term, out := notInteractive()
	err := store.Verify(path, []byte("network_host: true\n"), term)
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

	term, out := notInteractive()
	err := store.Verify(path, []byte("network_host: true\n"), term)
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

	term, _ := interactive("")
	err := store.Verify("/other/.asbx.yaml", []byte("network_host: true\n"), term)
	assert.Assert(t, errors.Is(err, ErrNotTrusted), "Verify() = %v, want ErrNotTrusted", err)
}
