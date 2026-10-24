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

package docker

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"go.rgst.io/jaredallard/cmdexec/v2"
)

// Exec runs docker running image with cmd, attached to the terminal,
// and waits for it to exit. A non-zero exit is returned as an
// [ExitError]. docker is killed if ctx is canceled.
func Exec(ctx context.Context, run *Run, image string, cmd []string) error {
	c := cmdexec.CommandContext(ctx, "docker", run.command(image, cmd)...)
	c.SetEnviron(run.environ(os.Environ()))
	c.UseOSStreams(true)

	// Ctrl+C reaches docker as well, since it shares the terminal, so
	// it's left to docker rather than exiting before it does.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	err := c.Run()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()

		// Like a shell, report being killed by a signal as 128 plus it.
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			code = 128 + int(status.Signal())
		}

		return &ExitError{Code: code}
	}

	return err
}
