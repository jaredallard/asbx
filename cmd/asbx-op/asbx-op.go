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

// Package main implements asbx-op. It reads secrets through the host's
// op, limited to the refs the host allows, and only supports op read
// and op run.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	"git.rgst.io/jaredallard/asbx/internal/opbridge"
	"git.rgst.io/jaredallard/asbx/internal/opshim"
	"github.com/urfave/cli/v3"
)

// read reads the secret at ref through the bridge, when it's enabled.
func read(ctx context.Context, ref string) (string, error) {
	return opbridge.Read(ctx, os.Getenv(opbridge.SocketEnv), ref)
}

// readCommand is op read.
func readCommand() *cli.Command {
	return &cli.Command{
		Name:      "read",
		Usage:     "Read a secret by its op:// reference",
		ArgsUsage: "<reference>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "no-newline", Aliases: []string{"n"}, Usage: "Don't print a new line after the secret"},
			&cli.StringFlag{Name: "out-file", Aliases: []string{"o"}, Usage: "Write the secret to a file instead of stdout"},
			&cli.StringFlag{Name: "file-mode", Value: "0600", Usage: "Set the mode of the file written with --out-file"},
			&cli.BoolFlag{Name: "force", Aliases: []string{"f"}, Usage: "Overwrite the file written with --out-file"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.NArg() != 1 {
				return errors.New("expected a single op:// reference")
			}

			mode, err := strconv.ParseUint(cmd.String("file-mode"), 8, 32)
			if err != nil {
				return fmt.Errorf("invalid --file-mode %q: %w", cmd.String("file-mode"), err)
			}

			return opshim.Read(ctx, read, os.Stdout, cmd.Args().First(), opshim.ReadOptions{
				NoNewline: cmd.Bool("no-newline"),
				OutFile:   cmd.String("out-file"),
				FileMode:  os.FileMode(mode),
				Force:     cmd.Bool("force"),
			})
		},
	}
}

// runCommand is op run.
func runCommand() *cli.Command {
	// Flags after the command's name are its own, with or without --.
	commandName := 1

	return &cli.Command{
		Name:         "run",
		Usage:        "Run a command with the op:// references in its environment replaced by their secrets",
		ArgsUsage:    "-- <command> [args...]",
		StopOnNthArg: &commandName,
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: "env-file", Usage: "Add the variables in a dotenv file to the environment"},
			&cli.BoolFlag{Name: "no-masking", Usage: "Don't mask secrets in the command's output"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return opshim.Run(ctx, read, cmd.Args().Slice(), opshim.RunOptions{
				EnvFiles:  cmd.StringSlice("env-file"),
				NoMasking: cmd.Bool("no-masking"),
			})
		},
	}
}

// bridgeCommand runs the bridge, which the host does with docker exec.
func bridgeCommand() *cli.Command {
	return &cli.Command{
		Name:   "bridge",
		Hidden: true,
		Action: func(ctx context.Context, _ *cli.Command) error {
			return opbridge.Bridge(ctx, os.Stdin, os.Stdout, opbridge.Socket)
		},
	}
}

func main() {
	cmd := &cli.Command{
		Name:  "op",
		Usage: "1Password CLI, through the host's op (asbx)",
		Commands: []*cli.Command{
			readCommand(),
			runCommand(),
			bridgeCommand(),
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if !cmd.Args().Present() {
				return cli.ShowRootCommandHelp(cmd)
			}

			return fmt.Errorf("op %s isn't supported in asbx, only op read and op run are", cmd.Args().First())
		},

		// We handle them separately.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)

	err := cmd.Run(ctx, os.Args)
	stop()

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}

		fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		os.Exit(1)
	}
}
