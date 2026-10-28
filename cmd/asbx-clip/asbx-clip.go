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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"git.rgst.io/jaredallard/asbx/internal/clipbridge"
	"git.rgst.io/jaredallard/asbx/internal/clipshim"
	"git.rgst.io/jaredallard/asbx/internal/oscfilter"
	"git.rgst.io/jaredallard/asbx/internal/ptyproxy"
	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
)

// copyTimeout is how long a copy to the host's clipboard can take.
const copyTimeout = 10 * time.Second

// runPty runs args, a command, on a terminal of its own, setting the
// host's clipboard to what it copies with OSC 52 sequences, which
// terminals may not honor. The command's exit code is passed on.
func runPty(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}

	if len(args) == 0 {
		return fmt.Errorf("usage: asbx-clip pty -- <command> [args...]")
	}

	socket := os.Getenv(clipbridge.SocketEnv)

	// One copy at a time, in order, so a flood can't pile up and an
	// older copy can't land after a newer one. If the host falls
	// behind, only the latest is kept. The terminal isn't held up.
	copies := make(chan []byte, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)

		for data := range copies {
			cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), copyTimeout)

			//nolint:errcheck // Why: there's nowhere to report it that wouldn't corrupt the terminal.
			clipbridge.Copy(cctx, socket, clipbridge.TextType, data)
			cancel()
		}
	}()

	err := ptyproxy.Run(ctx, args, func(w io.Writer) io.Writer {
		return oscfilter.New(w, clipbridge.MaxData, func(data []byte) {
			select {
			case copies <- data:
			default:
				// Only this function sends, so there's room after this.
				select {
				case <-copies:
				default:
				}
				copies <- data
			}
		})
	})

	close(copies)
	<-done

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()

		// Like a shell, report being killed by a signal as 128 plus it.
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			code = 128 + int(status.Signal())
		}

		os.Exit(code)
	}

	return err
}

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

	if name == "pty" {
		return runPty(ctx, args)
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
