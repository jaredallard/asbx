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

package stdiobridge

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/rpc"
	"os"
	"syscall"
	"time"

	"github.com/hashicorp/yamux"
)

// startTimeout is how long a client waits for the bridge to start,
// since the host starts it just after the container.
const startTimeout = 10 * time.Second

// relay copies between conn, a client, and a new stream to the host,
// until either ends.
func relay(session *yamux.Session, conn net.Conn) {
	defer conn.Close()

	stream, err := session.OpenStream()
	if err != nil {
		return
	}
	defer stream.Close()

	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(stream, conn) //nolint:errcheck // Why: either end closing is how it's finished.
		done <- struct{}{}
	}()

	go func() {
		_, _ = io.Copy(conn, stream) //nolint:errcheck // Why: see above.
		done <- struct{}{}
	}()

	<-done
}

// Bridge listens on socket and relays each client that connects to the
// host, which stdio is connected to. It returns once stdio is closed,
// which is how the host stops it, or ctx is canceled.
func Bridge(ctx context.Context, stdio io.ReadWriteCloser, socket string) error {
	// Left behind by a previous bridge, since the host restarts it.
	if err := os.Remove(socket); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	l, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer l.Close()

	session, err := yamux.Client(stdio, config())
	if err != nil {
		return err
	}
	defer session.Close()

	// Unblocks Accept.
	go func() {
		select {
		case <-ctx.Done():
		case <-session.CloseChan():
		}

		l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil || session.IsClosed() {
				return nil
			}

			return err
		}

		go relay(session, conn)
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
			return nil, fmt.Errorf("the asbx bridge didn't start: %w", err)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Call calls the rpc method on the host through the bridge at socket,
// with args, and sets reply to its answer, as [CallTLS] does with the
// key in [KeyEnv]. The host's error is returned as is.
func Call(ctx context.Context, socket, method string, args, reply any) error {
	tlsConfig, err := ClientConfig(os.Getenv(KeyEnv))
	if err != nil {
		return err
	}

	return CallTLS(ctx, socket, tlsConfig, method, args, reply)
}

// CallTLS calls the rpc method on the host through the bridge at
// socket, with args, and sets reply to its answer. The call is inside
// TLS, as tlsConfig describes it, so that the bridge can't read it. The
// host's error is returned as is.
func CallTLS(ctx context.Context, socket string, tlsConfig *tls.Config, method string, args, reply any) error {
	raw, err := dial(ctx, socket)
	if err != nil {
		return err
	}

	conn := tls.Client(raw, tlsConfig)
	defer conn.Close()

	if err := conn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("the asbx bridge didn't answer: %w", err)
	}

	client := rpc.NewClient(conn)
	defer client.Close()

	call := client.Go(method, args, reply, make(chan *rpc.Call, 1))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-call.Done:
	}

	var remote rpc.ServerError
	if call.Error != nil && !errors.As(call.Error, &remote) {
		return fmt.Errorf("the asbx bridge didn't answer: %w", call.Error)
	}

	return call.Error
}
