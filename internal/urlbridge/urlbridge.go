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

// Package urlbridge lets xdg-open inside the container open URLs in
// the host's browser.
//
// The host serves [Service] to the container over [stdiobridge], which
// the container's xdg-open reaches on [Socket].
package urlbridge

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
)

// SocketEnv is the environment variable that holds the path of the
// bridge's socket inside the container. It's only set when the bridge
// is enabled.
const SocketEnv = "ASBX_OPEN_SOCKET"

// Socket is where the bridge listens inside the container.
const Socket = "/run/asbx/open.sock"

// MaxURL is the longest URL either side accepts.
const MaxURL = 64 << 10 // 64 KiB.

// ErrUnavailable is returned by [Open] when the bridge isn't enabled.
var ErrUnavailable = errors.New("opening URLs isn't available in this sandbox, set open_urls_enabled in asbx's configuration")

// OpenArgs are the arguments of [Service.Open].
type OpenArgs struct {
	// URL is what's opened.
	URL string
}

// Validate returns an error unless raw is an http or https URL with a
// host. Anything else, such as a file or another app's scheme, would
// have the host's opener run more than a browser.
func Validate(raw string) error {
	if len(raw) > MaxURL {
		return fmt.Errorf("refusing to open a URL longer than %d KiB", MaxURL>>10)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return err
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("refusing to open %q, only http and https URLs can be opened", raw)
	}

	if u.Host == "" {
		return fmt.Errorf("refusing to open %q, it has no host", raw)
	}

	return nil
}

// Open opens rawURL in the host's browser, through the bridge at
// socket. An empty socket means the bridge isn't enabled, which is
// [ErrUnavailable].
func Open(ctx context.Context, socket, rawURL string) error {
	if socket == "" {
		return ErrUnavailable
	}

	if err := Validate(rawURL); err != nil {
		return err
	}

	return stdiobridge.Call(ctx, socket, serviceName+".Open", &OpenArgs{URL: rawURL}, &stdiobridge.Empty{})
}
