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

// Package opshim implements the op commands that work inside the
// sandbox, op read, op run and op item list, on top of the op bridge.
package opshim

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"

	"git.rgst.io/jaredallard/asbx/internal/opbridge"
	"go.rgst.io/jaredallard/cmdexec/v2"
)

// envLinePattern matches the KEY=value lines of an env file, with an
// optional export.
var envLinePattern = regexp.MustCompile(`^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// ReadOptions are the options to [Read].
type ReadOptions struct {
	// NoNewline leaves out the newline after the secret on w.
	NoNewline bool

	// OutFile, when set, is written to with the secret instead of w,
	// which gets its path.
	OutFile string

	// FileMode is OutFile's mode.
	FileMode os.FileMode

	// Force overwrites OutFile if it exists.
	Force bool
}

// RunOptions are the options to [Run].
type RunOptions struct {
	// EnvFiles are dotenv files whose variables are added to the
	// environment, overriding it.
	EnvFiles []string

	// NoMasking leaves secrets in the command's output.
	NoMasking bool
}

// writeFile writes secret to path, as op read --out-file does.
func writeFile(path, secret string, opts ReadOptions) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !opts.Force {
		flags |= os.O_EXCL
	}

	f, err := os.OpenFile(path, flags, opts.FileMode) //nolint:gosec // Why: writing where the user asked is the point.
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists, use --force to overwrite it", path)
	}
	if err != nil {
		return err
	}
	defer f.Close()

	// An existing file keeps its mode otherwise.
	if err := f.Chmod(opts.FileMode); err != nil {
		return err
	}

	if _, err := f.WriteString(secret); err != nil {
		return err
	}

	return f.Close()
}

// Read writes the secret at ref, read with read, to w.
func Read(ctx context.Context, read opbridge.ReadFunc, w io.Writer, ref string, opts ReadOptions) error {
	secret, err := read(ctx, ref)
	if err != nil {
		return err
	}

	if opts.OutFile != "" {
		if err := writeFile(opts.OutFile, secret, opts); err != nil {
			return err
		}

		secret = opts.OutFile
	}

	if !opts.NoNewline {
		secret += "\n"
	}

	_, err = io.WriteString(w, secret)
	return err
}

// unquote returns value without the quotes around it, if it has
// matching ones.
func unquote(value string) string {
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}

	return value
}

// ParseEnvFile returns the variables in the dotenv file at path, as
// NAME=value. Values may be quoted, but aren't expanded.
func ParseEnvFile(path string) ([]string, error) {
	f, err := os.Open(path) //nolint:gosec // Why: reading the given env file is the point.
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var env []string
	scanner := bufio.NewScanner(f)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		match := envLinePattern.FindStringSubmatch(line)
		if match == nil {
			return nil, fmt.Errorf("%s:%d: expected NAME=value", path, n)
		}

		env = append(env, match[1]+"="+unquote(strings.TrimSpace(match[2])))
	}

	return env, scanner.Err()
}

// Resolve returns env with every value that's an op:// ref replaced by
// the secret it refers to, read with read, and the secrets it read.
func Resolve(ctx context.Context, read opbridge.ReadFunc, env []string) (resolved, secrets []string, err error) {
	// Each ref is only read once, however many variables use it.
	cache := make(map[string]string)
	for _, kv := range env {
		name, ref, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(ref, "op://") {
			resolved = append(resolved, kv)
			continue
		}

		secret, ok := cache[ref]
		if !ok {
			secret, err = read(ctx, ref)
			if err != nil {
				return nil, nil, fmt.Errorf("unable to read %s: %w", name, err)
			}

			cache[ref] = secret
			secrets = append(secrets, secret)
		}

		resolved = append(resolved, name+"="+secret)
	}

	return resolved, secrets, nil
}

// merge returns base with the variables in overrides added, replacing
// any of the same name.
func merge(base, overrides []string) []string {
	names := make(map[string]bool)
	for _, kv := range overrides {
		name, _, _ := strings.Cut(kv, "=")
		names[name] = true
	}

	var env []string
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if !names[name] {
			env = append(env, kv)
		}
	}

	return append(env, overrides...)
}

// Run runs args with the op:// refs in its environment, and its env
// files, resolved with read. Unless opts.NoMasking, the secrets are
// masked in its output, so they don't end up in the agent's context.
func Run(ctx context.Context, read opbridge.ReadFunc, args []string, opts RunOptions) error {
	if len(args) == 0 {
		return errors.New("expected a command to run, e.g. op run -- printenv")
	}

	var files []string
	for _, path := range opts.EnvFiles {
		env, err := ParseEnvFile(path)
		if err != nil {
			return err
		}

		files = append(files, env...)
	}

	env, secrets, err := Resolve(ctx, read, merge(os.Environ(), files))
	if err != nil {
		return err
	}

	cmd := cmdexec.CommandContext(ctx, args[0], args[1:]...)
	cmd.SetEnviron(env)
	cmd.UseOSStreams(true)

	var stdout, stderr *MaskWriter
	if !opts.NoMasking {
		stdout = NewMaskWriter(os.Stdout, secrets)
		stderr = NewMaskWriter(os.Stderr, secrets)
		cmd.SetStdout(stdout)
		cmd.SetStderr(stderr)
	}

	// Ctrl+C reaches the command as well, since it shares the
	// terminal, so it's left to the command rather than exiting before
	// it does.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	err = cmd.Run()

	if stdout != nil {
		// The command's exit is what's reported, there's nowhere left to
		// report failing to write its output.
		_ = stdout.Flush() //nolint:errcheck // Why: see above.
		_ = stderr.Flush() //nolint:errcheck // Why: see above.
	}

	return err
}
