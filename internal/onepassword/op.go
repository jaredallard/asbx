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

// Package onepassword reads secrets from 1Password with the op CLI.
package onepassword

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"go.rgst.io/jaredallard/cmdexec/v2"
)

// read returns the secret at ref from op's output, or op's error.
func read(cmd cmdexec.Cmd) (string, error) {
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// op ends the secret with a newline, which isn't part of it.
	secret := strings.TrimRight(string(out), "\n")
	if secret == "" {
		return "", errors.New("secret is empty")
	}

	return secret, nil
}

// Read returns the secret at ref, an op:// secret reference. op may
// prompt to unlock 1Password, so it's given the terminal's stdin and
// stderr.
func Read(ctx context.Context, ref string) (string, error) {
	cmd := cmdexec.CommandContext(ctx, "op", "read", ref)
	cmd.SetStdin(os.Stdin)
	cmd.SetStderr(os.Stderr)

	return read(cmd)
}

// ReadNonInteractive is [Read] for when something else, such as the
// agent, controls the terminal and writing to it would be bad UX.
func ReadNonInteractive(ctx context.Context, ref string) (string, error) {
	secret, err := read(cmdexec.CommandContext(ctx, "op", "read", ref))

	// Output captures stderr when it isn't set.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
	}

	return secret, err
}

// ParseRef returns the vault, item and field of ref, an op:// secret
// reference to a field that isn't in a section, op://vault/item/field.
func ParseRef(ref string) (vault, item, field string, err error) {
	path, ok := strings.CutPrefix(ref, "op://")
	parts := strings.Split(path, "/")
	if !ok || len(parts) != 3 || slices.Contains(parts, "") {
		return "", "", "", fmt.Errorf("%q isn't an op://vault/item/field ref", ref)
	}

	return parts[0], parts[1], parts[2], nil
}

// ItemExists reports whether vault has an item titled item. Archived
// items aren't counted. op may prompt to unlock 1Password, as for
// [Read].
func ItemExists(ctx context.Context, vault, item string) (bool, error) {
	cmd := cmdexec.CommandContext(ctx, "op", "item", "list", "--vault", vault, "--format", "json")
	cmd.SetStdin(os.Stdin)
	cmd.SetStderr(os.Stderr)

	out, err := cmd.Output()
	if err != nil {
		return false, err
	}

	var items []struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(out, &items); err != nil {
		return false, fmt.Errorf("unable to parse op item list output: %w", err)
	}

	for _, i := range items {
		if i.Title == item {
			return true, nil
		}
	}

	return false, nil
}

// itemField is a field of an item template, see itemTemplate.
type itemField struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// itemTemplate is the JSON template op item create creates an item
// from.
type itemTemplate struct {
	Title    string      `json:"title"`
	Category string      `json:"category"`
	Fields   []itemField `json:"fields"`
}

// CreateItem creates an API Credential item titled item in vault, with
// value in its concealed field. value is given to op on stdin, rather
// than as an argument, where other processes could read it, so op
// can't prompt to unlock 1Password; it's expected to be unlocked
// already, such as by [Read].
func CreateItem(ctx context.Context, vault, item, field, value string) error {
	template, err := json.Marshal(itemTemplate{
		Title:    item,
		Category: "API_CREDENTIAL",
		Fields:   []itemField{{ID: field, Label: field, Type: "CONCEALED", Value: value}},
	})
	if err != nil {
		return err
	}

	// "-" reads the template from stdin.
	cmd := cmdexec.CommandContext(ctx, "op", "item", "create", "--vault", vault, "-")
	cmd.SetStdin(bytes.NewReader(template))
	cmd.SetStderr(os.Stderr)

	return cmd.Run()
}
