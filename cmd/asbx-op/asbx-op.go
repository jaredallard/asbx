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
// op, limited to the refs the host allows, and only supports op read,
// op run and op item list.
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

	"git.rgst.io/jaredallard/asbx/internal/onepassword"
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

// listItems lists the items with a field the container may read
// through the bridge, when it's enabled.
func listItems(ctx context.Context, vault string) ([]onepassword.Item, error) {
	return opbridge.ListItems(ctx, os.Getenv(opbridge.SocketEnv), vault)
}

// itemListCommand is op item list.
func itemListCommand() *cli.Command {
	// op's other options, which are accepted so that using one says it
	// isn't supported, rather than that it doesn't exist.
	unsupported := []cli.Flag{
		&cli.StringFlag{Name: "categories", Hidden: true},
		&cli.StringFlag{Name: "tags", Hidden: true},
		&cli.BoolFlag{Name: "favorite", Hidden: true},
		&cli.BoolFlag{Name: "include-archive", Hidden: true},
		&cli.BoolFlag{Name: "long", Hidden: true},
	}

	return &cli.Command{
		Name:        "list",
		Aliases:     []string{"ls"},
		Usage:       "List the items with a field you may read",
		Description: "Only --vault and --format are supported, op's other options, such as --categories, aren't.",
		Flags: append([]cli.Flag{
			&cli.StringFlag{Name: "vault", Usage: "Only list the items in this vault"},
			&cli.StringFlag{Name: "format", Value: "human", Usage: "Write the items as human or json"},
		}, unsupported...),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, flag := range unsupported {
				if name := flag.Names()[0]; cmd.IsSet(name) {
					return fmt.Errorf("op item list --%s isn't supported in asbx, only --vault and --format are", name)
				}
			}

			return opshim.ListItems(ctx, listItems, os.Stdout, opshim.ListOptions{
				Vault:  cmd.String("vault"),
				Format: cmd.String("format"),
			})
		},
	}
}

// itemCommand is op item, of which only op item list is supported.
func itemCommand() *cli.Command {
	return &cli.Command{
		Name:     "item",
		Usage:    "Work with items, of which only listing them is supported",
		Commands: []*cli.Command{itemListCommand()},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if !cmd.Args().Present() {
				return cli.ShowSubcommandHelp(cmd)
			}

			return fmt.Errorf("op item %s isn't supported in asbx, only op item list is", cmd.Args().First())
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
			itemCommand(),
			bridgeCommand(),
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if !cmd.Args().Present() {
				return cli.ShowRootCommandHelp(cmd)
			}

			return fmt.Errorf("op %s isn't supported in asbx, only op read, op run and op item list are", cmd.Args().First())
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
