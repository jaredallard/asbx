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

// Package ptyproxy runs a command on a pseudo-terminal of its own, so
// what it writes to the terminal can be filtered on the way.
package ptyproxy

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	xterm "github.com/charmbracelet/x/term"
	"github.com/creack/pty"
)

// killDelay is how long a command has to stop once it's asked to.
const killDelay = 10 * time.Second

// Run runs argv attached to a new pseudo-terminal, passing the real
// terminal's input to it and its output through wrap to the real
// terminal, and waits for it to exit. A non-zero exit is returned as
// an [*exec.ExitError].
func Run(ctx context.Context, argv []string, wrap func(io.Writer) io.Writer) error {
	// Raw before the command starts, so there's nothing to clean up if
	// it fails.
	if xterm.IsTerminal(os.Stdin.Fd()) {
		old, err := xterm.MakeRaw(os.Stdin.Fd())
		if err != nil {
			return err
		}
		defer xterm.Restore(os.Stdin.Fd(), old) //nolint:errcheck // Why: nothing to do about it.
	}

	// Canceling ctx asks the command to stop, and only kills it if it
	// doesn't.
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = killDelay

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	defer ptmx.Close()

	// The size follows the real terminal's.
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer signal.Stop(resize)

	go func() {
		for range resize {
			//nolint:errcheck // Why: the size is only a nicety.
			pty.InheritSize(os.Stdin, ptmx)
		}
	}()
	resize <- syscall.SIGWINCH

	// Ctrl+C and the like are typed, so they reach the command through
	// the terminal. Signals sent to this process are passed on, except
	// for SIGTERM, which is left to ctx being canceled.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(signals)

	go func() {
		for s := range signals {
			//nolint:errcheck // Why: it may have already exited.
			cmd.Process.Signal(s)
		}
	}()

	go io.Copy(ptmx, os.Stdin) //nolint:errcheck // Why: it ends with the command.

	// Ends with EIO once the command has closed the terminal.
	_, copyErr := io.Copy(wrap(os.Stdout), ptmx)

	// Otherwise nothing reads the command's output, and it blocks.
	failed := copyErr != nil && !errors.Is(copyErr, syscall.EIO)
	if failed {
		ptmx.Close()
		cmd.Process.Signal(syscall.SIGHUP) //nolint:errcheck // Why: it may have already exited.
	}

	if err := cmd.Wait(); err != nil {
		return err
	}

	if failed {
		return copyErr
	}

	return nil
}
