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
	"strings"
	"syscall"

	"git.rgst.io/jaredallard/asbx/internal/agent/claude"
	"git.rgst.io/jaredallard/asbx/internal/agent/codex"
	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/sandbox"
	"github.com/urfave/cli/v3"
)

// agentCommand returns the command that runs agent in the sandbox with
// the arguments it was given. known are every agent asbx can run.
func agentCommand(agent sandbox.Agent, known []sandbox.Agent) *cli.Command {
	return &cli.Command{
		Name:  agent.Name(),
		Usage: "Run " + agent.Name(),

		// Every argument, flags included, is the agent's.
		SkipFlagParsing: true,
		HideHelp:        true,

		Action: func(ctx context.Context, cmd *cli.Command) error {
			host, err := sandbox.NewHost(ctx)
			if err != nil {
				return err
			}

			return sandbox.Run(ctx, host, agent, known, cmd.Args().Slice())
		},
	}
}

// runDefault returns the action that runs the agent the configuration
// names, out of known, with the arguments asbx was given.
func runDefault(known []sandbox.Agent) cli.ActionFunc {
	return func(ctx context.Context, cmd *cli.Command) error {
		host, err := sandbox.NewHost(ctx)
		if err != nil {
			return err
		}

		var names []string
		for _, agent := range known {
			if agent.Name() == host.Config.Agent {
				return sandbox.Run(ctx, host, agent, known, cmd.Args().Slice())
			}

			names = append(names, agent.Name())
		}

		return fmt.Errorf("agent %q isn't one of %s", host.Config.Agent, strings.Join(names, ", "))
	}
}

func main() {
	known := []sandbox.Agent{claude.New(), codex.New()}

	var commands []*cli.Command
	for _, agent := range known {
		commands = append(commands, agentCommand(agent, known))
	}

	cmd := &cli.Command{
		Name:  "asbx",
		Usage: "Run a coding agent, Claude Code or Codex, inside of a Docker container",

		// "asbx <agent> [args]" runs that agent. Otherwise every argument,
		// flags included, is for the agent the configuration names.
		Commands:        commands,
		SkipFlagParsing: true,
		HideHelp:        true,
		HideVersion:     true,

		Action: runDefault(known),

		ExitErrHandler: func(context.Context, *cli.Command, error) {},
	}

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
