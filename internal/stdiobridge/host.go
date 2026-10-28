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

// Package stdiobridge lets programs inside the container call services
// that run on the host, over the stdin and stdout of a docker exec.
//
// Unix sockets can't be shared with the container on every Docker
// host, OrbStack included, so the host runs the container's bridge with
// docker exec and talks to it over its stdin and stdout. Only asbx
// holds those, so nothing else on the host can reach the services. The
// bridge listens on a Unix socket inside the container, and each
// client that connects to it gets a yamux stream to the host, which
// serves it with net/rpc and gob, inside TLS that only the container's
// shims have the key for, so the bridge only ever relays ciphertext.
package stdiobridge

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/rpc"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/yamux"
	"go.rgst.io/jaredallard/cmdexec/v2"
)

// retryInterval is how long Run waits between checking that the
// container is running and restarting the bridge.
const retryInterval = 250 * time.Millisecond

// handshakeTimeout is how long a client has to finish its TLS
// handshake, so that one that never does can't hold a stream open.
const handshakeTimeout = 5 * time.Second

// Empty is the argument or reply of an rpc method that has none.
type Empty struct{}

// config returns the yamux configuration of both ends. It's quiet,
// since it would otherwise write over the agent in the terminal.
func config() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard

	return cfg
}

// Stdio returns the process's stdin and stdout as one connection, for
// [Bridge].
func Stdio() io.ReadWriteCloser {
	return struct {
		io.ReadCloser
		io.Writer
	}{os.Stdin, os.Stdout}
}

// handshake completes the TLS handshake of conn, which is over stream,
// within [handshakeTimeout].
func handshake(stream net.Conn, conn *tls.Conn) error {
	if err := stream.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return err
	}

	if err := conn.Handshake(); err != nil {
		return err
	}

	return stream.SetDeadline(time.Time{})
}

// serveStream serves one client's rpc calls on stream, with the
// services register adds, until it closes. The client has to prove it
// holds the session's key, which tlsConfig is for. Reading more than
// limit bytes ends it.
func serveStream(stream net.Conn, tlsConfig *tls.Config, limit int64, register func(*rpc.Server) error) {
	conn := tls.Server(stream, tlsConfig)
	defer conn.Close()

	if err := handshake(stream, conn); err != nil {
		return
	}

	srv := rpc.NewServer()
	if err := register(srv); err != nil {
		return
	}

	// Only what's read is limited, so that the container can't make the
	// host buffer a request without limit.
	srv.ServeConn(struct {
		io.Reader
		io.Writer
		io.Closer
	}{io.LimitReader(conn, limit), conn, conn})
}

// Serve serves the rpc calls of every client that the bridge on rwc
// relays, until rwc is closed or ctx is canceled. Each client has to
// prove it holds the session's key, which tlsConfig is for. register
// adds the services to the [rpc.Server] of each client, which can't read
// more than limit bytes.
func Serve(ctx context.Context, rwc io.ReadWriteCloser, tlsConfig *tls.Config, limit int64, register func(*rpc.Server) error) error {
	session, err := yamux.Server(rwc, config())
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

		session.Close()
	}()

	for {
		stream, err := session.AcceptStream()
		if err != nil {
			if ctx.Err() != nil || session.IsClosed() || errors.Is(err, io.EOF) {
				return nil
			}

			return err
		}

		go serveStream(stream, tlsConfig, limit, register)
	}
}

// running reports whether container has started and not yet stopped.
func running(ctx context.Context, container string) bool {
	out, err := cmdexec.CommandContext(ctx, "docker", "container", "inspect", "--format", "{{.State.Running}}", container).Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// serveExec starts the bridge in container, as bridgeCmd, and serves it
// until it exits. Its stdin and stdout are OS pipes, rather than handed
// to cmdexec as readers and writers, so that it exiting closes them
// without waiting on the other end.
func serveExec(
	ctx context.Context, container string, bridgeCmd []string, tlsConfig *tls.Config, limit int64, register func(*rpc.Server) error,
) error {
	requests, requestsW, err := os.Pipe()
	if err != nil {
		return err
	}
	defer requests.Close()

	responsesR, responses, err := os.Pipe()
	if err != nil {
		return err
	}
	defer responses.Close()

	// Serve failing stops the bridge, for Run to restart it.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// stderr isn't set, so the bridge can't write over the agent.
	cmd := cmdexec.CommandContext(ctx, "docker", append([]string{"exec", "--interactive", container}, bridgeCmd...)...)
	cmd.SetStdin(responsesR)
	cmd.SetStdout(requestsW)

	served := make(chan error, 1)
	go func() {
		err := Serve(ctx, struct {
			io.ReadCloser
			io.Writer
		}{requests, responses}, tlsConfig, limit, register)
		cancel()
		served <- err
	}()

	err = cmd.Run()

	requestsW.Close()
	responsesR.Close()

	return errors.Join(err, <-served)
}

// Run serves the bridge in container, started as bridgeCmd, until ctx
// is canceled, as for [Serve]. It waits for container to start and
// restarts the bridge if it exits.
func Run(ctx context.Context, container string, bridgeCmd []string, tlsConfig *tls.Config, limit int64, register func(*rpc.Server) error) {
	for {
		// Errors are retried rather than reported, since writing to the
		// terminal at this point would write bad output. Failure here is
		// reported to the client.
		if running(ctx, container) {
			_ = serveExec(ctx, container, bridgeCmd, tlsConfig, limit, register) //nolint:errcheck // Why: see above.
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(retryInterval):
		}
	}
}
