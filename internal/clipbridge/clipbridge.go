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

// Package clipbridge lets the clipboard tools inside the container,
// such as wl-copy and xclip, reach the host's clipboard.
//
// The host serves [Service] to the container over [stdiobridge], which
// the container's tools reach on [Socket].
package clipbridge

import (
	"context"
	"errors"
	"fmt"

	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
)

// SocketEnv is the environment variable that holds the path of the
// bridge's socket inside the container. It's only set when the bridge
// is enabled.
const SocketEnv = "ASBX_CLIP_SOCKET"

// Socket is where the bridge listens inside the container.
const Socket = "/run/asbx/clip.sock"

// TextType is the type of the clipboard's text, which is what a tool
// uses when it isn't asked for another.
const TextType = "text/plain"

// MaxData is the most clipboard data either side accepts, so that
// neither can make the other buffer without limit.
const MaxData = 8 << 20 // 8 MiB.

// ErrUnavailable is returned by the client functions when the bridge
// isn't enabled.
var ErrUnavailable = errors.New("the clipboard isn't available in this sandbox, set clipboard in asbx's configuration")

// CopyArgs are the arguments of [Service.Copy].
type CopyArgs struct {
	// Type is the MIME type of Data. Empty means [TextType].
	Type string

	// Data is what goes on the clipboard.
	Data []byte
}

// PasteArgs are the arguments of [Service.Paste].
type PasteArgs struct {
	// Type is the MIME type to paste. Empty means [TextType].
	Type string
}

// Copy sets the host's clipboard to data, of the MIME type mime, through
// the bridge at socket. An empty socket means the bridge isn't enabled,
// which is [ErrUnavailable].
func Copy(ctx context.Context, socket, mime string, data []byte) error {
	if socket == "" {
		return ErrUnavailable
	}

	if len(data) > MaxData {
		return fmt.Errorf("refusing to copy more than %d MiB", MaxData>>20)
	}

	return stdiobridge.Call(ctx, socket, serviceName+".Copy", &CopyArgs{Type: mime, Data: data}, &stdiobridge.Empty{})
}

// Paste returns the host's clipboard as the MIME type mime, through the
// bridge at socket. An empty socket means the bridge isn't enabled,
// which is [ErrUnavailable].
func Paste(ctx context.Context, socket, mime string) ([]byte, error) {
	if socket == "" {
		return nil, ErrUnavailable
	}

	var data []byte
	err := stdiobridge.Call(ctx, socket, serviceName+".Paste", &PasteArgs{Type: mime}, &data)

	return data, err
}

// Types returns the MIME types the host's clipboard holds, through the
// bridge at socket. An empty socket means the bridge isn't enabled,
// which is [ErrUnavailable].
func Types(ctx context.Context, socket string) ([]string, error) {
	if socket == "" {
		return nil, ErrUnavailable
	}

	var types []string
	err := stdiobridge.Call(ctx, socket, serviceName+".Types", &stdiobridge.Empty{}, &types)

	return types, err
}
