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

// Asbx runs coding agents inside of a Docker container, with access to
// only the working directory.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"git.rgst.io/jaredallard/asbx/internal/agent/claude"
	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/sandbox"
	"github.com/urfave/cli/v3"
)

// run starts the agent in the sandbox with the arguments asbx was
// given.
func run(ctx context.Context, cmd *cli.Command) error {
	host, err := sandbox.NewHost(ctx)
	if err != nil {
		return err
	}

	dockerRun, err := sandbox.Build(ctx, host, claude.New())
	if err != nil {
		return err
	}

	args := append([]string{sandbox.Entrypoint}, cmd.Args().Slice()...)
	return docker.Exec(ctx, dockerRun, sandbox.Image, args)
}

func main() {
	cmd := &cli.Command{
		Name:  "asbx",
		Usage: "Run Claude Code inside of a Docker container",

		// Every argument, flags included, is the agent's.
		SkipFlagParsing: true,
		HideHelp:        true,
		HideVersion:     true,

		Action: run,

		// Errors are reported by main, urfave/cli would otherwise exit
		// silently on any error with an exit code, such as op's.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
	}

	// Stopping asbx stops docker with it, rather than leaving it
	// running without asbx.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)

	err := cmd.Run(ctx, os.Args)
	stop()

	if err != nil {
		// docker's exit code is passed through, it has already reported
		// why it exited.
		var exitErr *docker.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}

		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
