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
// Unix sockets can't be shared with the container on every Docker
// host, OrbStack included, so the host runs the container's bridge
// with docker exec and talks to it over its stdin and stdout. Only
// asbx holds those, so nothing else on the host can reach op through
// it. The bridge listens on a Unix socket inside the container for op
// to connect to.
//
// Every message is a single line of JSON: a [Request] from the
// container, answered in order by a [Response] from the host.
package opbridge

// SocketEnv is the environment variable that holds the path of the
// bridge's socket inside the container. It's only set when the bridge
// is enabled.
const SocketEnv = "ASBX_OP_SOCKET"

// Socket is where the bridge listens inside the container.
const Socket = "/run/asbx/op.sock"

// Request asks the host for the secret at an op:// ref.
type Request struct {
	Ref string `json:"ref"`
}

// Response is the host's answer to a [Request]: the secret, or why it
// can't be read.
type Response struct {
	Value string `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}
