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

// Package onepassword reads secrets from 1Password with the op CLI.
package onepassword

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go.rgst.io/jaredallard/cmdexec/v2"
)

// read returns the secret at ref from op's output, or op's error.
func read(cmd cmdexec.Cmd) (string, error) {
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// op ends the secret with a newline, which isn't part of it.
	secret := strings.TrimRight(string(out), "\n")
	if secret == "" {
		return "", errors.New("secret is empty")
	}

	return secret, nil
}

// Read returns the secret at ref, an op:// secret reference. op may
// prompt to unlock 1Password, so it's given the terminal's stdin and
// stderr.
func Read(ctx context.Context, ref string) (string, error) {
	cmd := cmdexec.CommandContext(ctx, "op", "read", ref)
	cmd.SetStdin(os.Stdin)
	cmd.SetStderr(os.Stderr)

	return read(cmd)
}

// ReadNonInteractive is [Read] for when something else, such as the
// agent, controls the terminal and writing to it would be bad UX.
func ReadNonInteractive(ctx context.Context, ref string) (string, error) {
	secret, err := read(cmdexec.CommandContext(ctx, "op", "read", ref))

	// Output captures stderr when it isn't set.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	}

	return secret, err
}
