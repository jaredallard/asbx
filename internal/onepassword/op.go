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
	"os"
	"strings"

	"go.rgst.io/jaredallard/cmdexec/v2"
)

// Read returns the secret at ref, an op:// secret reference. op may
// prompt to unlock 1Password, so it's given the terminal's stdin and
// stderr.
func Read(ctx context.Context, ref string) (string, error) {
	cmd := cmdexec.CommandContext(ctx, "op", "read", ref)
	cmd.SetStdin(os.Stdin)
	cmd.SetStderr(os.Stderr)

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
