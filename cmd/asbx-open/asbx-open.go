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

// Package main implements asbx-open. It stands in for xdg-open, which
// it's linked as, and opens URLs in the host's browser.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
	"git.rgst.io/jaredallard/asbx/internal/urlbridge"
)

// openTimeout is how long opening a URL on the host can take.
const openTimeout = 30 * time.Second

// run opens the URL it's given, or runs the bridge, which the host does
// with docker exec.
func run(ctx context.Context, argv []string) error {
	name := filepath.Base(argv[0])
	args := argv[1:]

	if name == "asbx-open" && len(args) == 1 && args[0] == "bridge" {
		return stdiobridge.Bridge(ctx, stdiobridge.Stdio(), urlbridge.Socket)
	}

	if len(args) != 1 {
		return fmt.Errorf("usage: %s <url>", name)
	}

	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()

	return urlbridge.Open(ctx, os.Getenv(urlbridge.SocketEnv), args[0])
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
