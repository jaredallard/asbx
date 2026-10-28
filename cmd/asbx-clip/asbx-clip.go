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

// Package main implements asbx-clip. It stands in for the clipboard
// tools wl-copy, wl-paste, xclip, xsel, pbcopy and pbpaste, which it's
// linked as, and forwards them to the host's clipboard.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"

	"git.rgst.io/jaredallard/asbx/internal/clipbridge"
	"git.rgst.io/jaredallard/asbx/internal/clipshim"
	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
)

// run runs the tool it was started as, or the one named by its first
// argument when it's started as asbx-clip, or the bridge, which the
// host does with docker exec.
func run(ctx context.Context, argv []string) error {
	name := filepath.Base(argv[0])
	args := argv[1:]

	if name == "asbx-clip" {
		if len(args) == 0 {
			return fmt.Errorf("usage: asbx-clip <bridge|%v> [args...]", clipshim.Tools)
		}

		name, args = args[0], args[1:]
	}

	if name == "bridge" {
		return stdiobridge.Bridge(ctx, stdiobridge.Stdio(), clipbridge.Socket)
	}

	if !slices.Contains(clipshim.Tools, name) {
		return fmt.Errorf("asbx-clip unknown tool %q", name)
	}

	return clipshim.Run(ctx, name, args, clipshim.IO{In: os.Stdin, Out: os.Stdout}, os.Getenv(clipbridge.SocketEnv))
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)

	err := run(ctx, os.Args)
	stop()

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
