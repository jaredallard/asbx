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
