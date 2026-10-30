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

package urlbridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.rgst.io/jaredallard/cmdexec/v2"
)

// ErrNoTool is returned by [Detect] when the host has nothing to open
// URLs with.
var ErrNoTool = errors.New("no way to open URLs found on this host, install xdg-utils")

// openWait is how long Open waits for the host's tool to exit, so its
// failure can be reported. Some, such as xdg-open falling back to
// running a browser itself, only exit once the browser does.
const openWait = 5 * time.Second

// Opener opens URLs on the host.
type Opener interface {
	// Open opens url in the host's browser.
	Open(ctx context.Context, url string) error
}

// tool is the [Opener] that runs a command, such as open or xdg-open,
// with the URL as its only argument.
type tool string

// run runs t with url, and returns its error along with what it wrote
// to stderr.
func (t tool) run(ctx context.Context, url string) error {
	var stderr bytes.Buffer

	cmd := cmdexec.CommandContext(ctx, string(t), url)
	cmd.SetStderr(&stderr)
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%s: %w: %s", t, err, msg)
		}

		return fmt.Errorf("%s: %w", t, err)
	}

	return nil
}

// Open implements [Opener]. It returns once the tool exits, or after
// [openWait] if it's still running, which it's left to do.
func (t tool) Open(ctx context.Context, url string) error {
	done := make(chan error, 1)
	go func() {
		// Not canceled with ctx, so a browser the tool runs itself isn't
		// killed when asbx exits.
		done <- t.run(context.WithoutCancel(ctx), url)
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(openWait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Detect returns the [Opener] of the tools installed on the host, or
// [ErrNoTool].
func Detect(goos string) (Opener, error) {
	name := "xdg-open"
	if goos == "darwin" {
		name = "open"
	}

	if _, err := cmdexec.LookPath(name); err != nil {
		return nil, ErrNoTool
	}

	return tool(name), nil
}
