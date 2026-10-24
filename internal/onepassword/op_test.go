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

package onepassword

import (
	"errors"
	"os/exec"
	"testing"

	"go.rgst.io/jaredallard/cmdexec/v2"
	"gotest.tools/v3/assert"
)

// TestRead verifies that a secret is returned without the trailing
// newline op prints after it.
func TestRead(t *testing.T) {
	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name:   "op",
		Args:   []string{"read", "op://a/b/c"},
		Stdout: []byte("hunter2\n"),
	}))

	got, err := Read(t.Context(), "op://a/b/c")
	assert.NilError(t, err)
	assert.Equal(t, got, "hunter2")
}

// TestReadErrors verifies that a secret op can't read, or that is
// empty, is an error rather than an empty value.
func TestReadErrors(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cmdexec.MockCommand
	}{
		{
			name: "CommandFails",
			cmd:  &cmdexec.MockCommand{Err: errors.New("exit status 1")},
		},
		{
			name: "EmptySecret",
			cmd:  &cmdexec.MockCommand{Stdout: []byte("\n")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cmd.Name = "op"
			tt.cmd.Args = []string{"read", "op://a/b/c"}
			cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(tt.cmd))

			got, err := Read(t.Context(), "op://a/b/c")
			assert.Assert(t, err != nil, "Read() = %q, want error", got)
		})
	}
}

// TestReadNonInteractiveError verifies that why op failed, which it
// prints to stderr, is part of the error since there's no terminal to
// show it on.
func TestReadNonInteractiveError(t *testing.T) {
	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name: "op",
		Args: []string{"read", "op://a/b/c"},
		Err:  &exec.ExitError{Stderr: []byte("[ERROR] not signed in\n")},
	}))

	_, err := ReadNonInteractive(t.Context(), "op://a/b/c")
	assert.ErrorContains(t, err, "[ERROR] not signed in")
}

// TestParseRef verifies that a ref to a field outside of a section is
// split into its vault, item and field.
func TestParseRef(t *testing.T) {
	tests := []struct {
		ref   string
		vault string
		item  string
		field string
	}{
		{ref: "op://Private/claude-token/credential", vault: "Private", item: "claude-token", field: "credential"},
		{ref: "op://My Vault/an item/a field", vault: "My Vault", item: "an item", field: "a field"},
	}

	for _, tt := range tests {
		vault, item, field, err := ParseRef(tt.ref)
		assert.NilError(t, err, "ParseRef(%q)", tt.ref)
		assert.Equal(t, vault, tt.vault, "ParseRef(%q) vault", tt.ref)
		assert.Equal(t, item, tt.item, "ParseRef(%q) item", tt.ref)
		assert.Equal(t, field, tt.field, "ParseRef(%q) field", tt.ref)
	}
}

// TestParseRefErrors verifies that refs that aren't to a field outside
// of a section, whose item asbx couldn't create, are errors.
func TestParseRefErrors(t *testing.T) {
	for _, ref := range []string{
		"Private/claude-token/credential",
		"op://Private/claude-token",
		"op://Private/claude-token/section/credential",
		"op://Private//credential",
		"op://",
	} {
		_, _, _, err := ParseRef(ref)
		assert.ErrorContains(t, err, "isn't an op://vault/item/field ref", "ParseRef(%q)", ref)
	}
}

// TestItemExists verifies that an item exists only when the vault has
// one with exactly its title.
func TestItemExists(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want bool
	}{
		{name: "Found", out: `[{"id":"a","title":"other"},{"id":"b","title":"claude-token"}]`, want: true},
		{name: "NotFound", out: `[{"id":"a","title":"claude-token-old"}]`},
		{name: "EmptyVault", out: `[]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
				Name:   "op",
				Args:   []string{"item", "list", "--vault", "Private", "--format", "json"},
				Stdout: []byte(tt.out),
			}))

			got, err := ItemExists(t.Context(), "Private", "claude-token")
			assert.NilError(t, err)
			assert.Equal(t, got, tt.want)
		})
	}
}

// TestItemExistsErrors verifies that when op can't list the vault, or
// its output can't be parsed, it's an error rather than the item not
// existing.
func TestItemExistsErrors(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cmdexec.MockCommand
	}{
		{name: "CommandFails", cmd: &cmdexec.MockCommand{Err: errors.New("exit status 1")}},
		{name: "Malformed", cmd: &cmdexec.MockCommand{Stdout: []byte("not json")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cmd.Name = "op"
			tt.cmd.Args = []string{"item", "list", "--vault", "Private", "--format", "json"}
			cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(tt.cmd))

			got, err := ItemExists(t.Context(), "Private", "claude-token")
			assert.Assert(t, err != nil, "ItemExists() = %v, want error", got)
		})
	}
}

// TestCreateItem verifies that the item is created as an API Credential
// with the value in a concealed field, given on stdin rather than as an
// argument.
func TestCreateItem(t *testing.T) {
	template := `{"title":"claude-token","category":"API_CREDENTIAL",` +
		`"fields":[{"id":"credential","label":"credential","type":"CONCEALED","value":"hunter2"}]}`

	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name:  "op",
		Args:  []string{"item", "create", "--vault", "Private", "-"},
		Stdin: []byte(template),
	}))

	assert.NilError(t, CreateItem(t.Context(), "Private", "claude-token", "credential", "hunter2"))
}
