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

// Package opbridge lets op inside the container read secrets through
// the host's op, limited to an allowlist the host enforces.
//
// The host serves [Service] to the container over [stdiobridge], which
// the container's op reaches on [Socket].
package opbridge

import (
	"context"
	"errors"

	"git.rgst.io/jaredallard/asbx/internal/onepassword"
	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
)

// SocketEnv is the environment variable that holds the path of the
// bridge's socket inside the container. It's only set when the bridge
// is enabled.
const SocketEnv = "ASBX_OP_SOCKET"

// Socket is where the bridge listens inside the container.
const Socket = "/run/asbx/op.sock"

// ErrUnavailable is returned by [Read] and [ListItems] when the bridge
// isn't enabled.
var ErrUnavailable = errors.New("op isn't available in this sandbox, set op_allowed_secret_refs in asbx's configuration")

// ReadFunc reads the secret at an op:// ref.
type ReadFunc func(ctx context.Context, ref string) (string, error)

// ReadArgs are the arguments of [Service.Read].
type ReadArgs struct {
	Ref string
}

// ListArgs are the arguments of [Service.ListItems].
type ListArgs struct {
	// Vault, when set, narrows the items to the vault with this name or
	// ID.
	Vault string
}

// Read returns the secret at ref, an op:// secret reference, read by
// the host's op through the bridge at socket. An empty socket means the
// bridge isn't enabled, which is [ErrUnavailable].
func Read(ctx context.Context, socket, ref string) (string, error) {
	if socket == "" {
		return "", ErrUnavailable
	}

	var value string
	err := stdiobridge.Call(ctx, socket, serviceName+".Read", &ReadArgs{Ref: ref}, &value)

	return value, err
}

// ListItems returns the items with a field the container may read,
// only from the vault with the name or ID vault, if it's set. They're
// listed by the host's op through the bridge at socket. An empty socket
// means the bridge isn't enabled, which is [ErrUnavailable].
func ListItems(ctx context.Context, socket, vault string) ([]onepassword.Item, error) {
	if socket == "" {
		return nil, ErrUnavailable
	}

	var items []onepassword.Item
	err := stdiobridge.Call(ctx, socket, serviceName+".ListItems", &ListArgs{Vault: vault}, &items)

	return items, err
}
