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

package opbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"syscall"
	"time"

	"git.rgst.io/jaredallard/asbx/internal/onepassword"
)

// requestTimeout is how long the bridge waits for a client to send its
// request, so one that doesn't can't hold up the others.
const requestTimeout = 10 * time.Second

// startTimeout is how long Read waits for the bridge to start, since
// the host starts it just after the container.
const startTimeout = 10 * time.Second

// ErrUnavailable is returned by [Read] when the bridge isn't enabled.
var ErrUnavailable = errors.New("op isn't available in this sandbox, set op_allowed_secret_refs in asbx's configuration")

// forward sends the request read from conn to the host on w, and
// writes back the host's answer from responses.
func forward(ctx context.Context, conn net.Conn, w io.Writer, responses <-chan Response) error {
	if err := conn.SetReadDeadline(time.Now().Add(requestTimeout)); err != nil {
		return err
	}

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return err
	}

	if err := json.NewEncoder(w).Encode(req); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-responses:
		return json.NewEncoder(conn).Encode(resp)
	}
}

// Bridge listens on socket and forwards each client's [Request] to the
// host on w, and the host's [Response] from r back to it. Clients are
// served one at a time, since the host answers in order.
//
// It returns once r is closed, which is how the host stops it. The
// goroutine reading r only exits once r does.
func Bridge(ctx context.Context, r io.Reader, w io.Writer, socket string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Left behind by a previous bridge, since the host restarts it.
	if err := os.Remove(socket); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	l, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer l.Close()

	responses := make(chan Response)
	go func() {
		defer cancel()

		dec := json.NewDecoder(r)
		for {
			var resp Response
			if err := dec.Decode(&resp); err != nil {
				return
			}

			select {
			case <-ctx.Done():
				return
			case responses <- resp:
			}
		}
	}()

	// Unblocks Accept.
	go func() {
		<-ctx.Done()
		l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return err
		}

		// A client that misbehaves only fails its own request.
		_ = forward(ctx, conn, w, responses) //nolint:errcheck // Why: see above.
		conn.Close()
	}
}

// dial connects to the bridge at socket, waiting for it to start.
func dial(ctx context.Context, socket string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	var d net.Dialer
	for {
		conn, err := d.DialContext(ctx, "unix", socket)
		if err == nil {
			return conn, nil
		}

		// Anything but the bridge not listening yet is fatal.
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("the asbx op bridge didn't start: %w", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// call sends req to the host through the bridge at socket and returns
// its answer, or its error. An empty socket means the bridge isn't
// enabled, which is [ErrUnavailable].
func call(ctx context.Context, socket string, req *Request) (*Response, error) {
	if socket == "" {
		return nil, ErrUnavailable
	}

	conn, err := dial(ctx, socket)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}

	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("the asbx op bridge didn't answer: %w", err)
	}

	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}

	return &resp, nil
}

// Read returns the secret at ref, an op:// secret reference, read by
// the host's op through the bridge at socket. An empty socket means the
// bridge isn't enabled, which is [ErrUnavailable].
func Read(ctx context.Context, socket, ref string) (string, error) {
	resp, err := call(ctx, socket, &Request{Ref: ref})
	if err != nil {
		return "", err
	}

	return resp.Value, nil
}

// ListItems returns the items with a field the container may read,
// only from the vault with the name or ID vault, if it's set. They're
// listed by the host's op through the bridge at socket. An empty socket
// means the bridge isn't enabled, which is [ErrUnavailable].
func ListItems(ctx context.Context, socket, vault string) ([]onepassword.Item, error) {
	resp, err := call(ctx, socket, &Request{Command: CommandItemList, Vault: vault})
	if err != nil {
		return nil, err
	}

	return resp.Items, nil
}
