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
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

// Echo is a service for the tests: it returns what it's given, fails,
// or waits.
type Echo struct {
	release chan struct{}
}

// Say sets reply to args.
func (*Echo) Say(args, reply *[]byte) error {
	*reply = *args
	return nil
}

// Fail always fails.
func (*Echo) Fail(_, _ *Empty) error {
	return errors.New("no way")
}

// Wait returns once the test releases it.
func (e *Echo) Wait(_, _ *Empty) error {
	<-e.release
	return nil
}

// bridge is a bridge that [start] set up.
type bridge struct {
	socket string

	// host is the host's end of the bridge's stdio.
	host net.Conn

	// served and bridged report the end of Serve and Bridge.
	served, bridged chan error
}

// newSession returns a session that the calls of the test use the key
// of.
func newSession(t *testing.T) *Session {
	t.Helper()

	session, err := NewSession()
	assert.NilError(t, err)
	t.Setenv(KeyEnv, session.Env())

	return session
}

// start serves an [Echo] with limit through a bridge, until the test
// ends. Closing the host ends both ends.
func start(t *testing.T, limit int64, echo *Echo) *bridge {
	t.Helper()

	session := newSession(t)

	b := &bridge{
		socket:  filepath.Join(t.TempDir(), "bridge.sock"),
		served:  make(chan error, 1),
		bridged: make(chan error, 1),
	}

	host, container := net.Pipe()
	b.host = host

	go func() {
		b.served <- Serve(t.Context(), host, session.ServerConfig(), limit, func(srv *rpc.Server) error {
			return srv.RegisterName("Echo", echo)
		})
	}()

	go func() {
		b.bridged <- Bridge(t.Context(), container, b.socket)
	}()

	return b
}

// TestCall verifies that a call reaches the service on the host and
// that its error is returned unchanged.
func TestCall(t *testing.T) {
	socket := start(t, 1<<20, &Echo{}).socket

	var got []byte
	assert.NilError(t, Call(t.Context(), socket, "Echo.Say", &[]byte{1, 2, 3}, &got))
	assert.DeepEqual(t, got, []byte{1, 2, 3})

	err := Call(t.Context(), socket, "Echo.Fail", &Empty{}, &Empty{})
	assert.Error(t, err, "no way")
}

// TestCallConcurrent verifies that clients are served in parallel, so
// that one that's waiting doesn't hold up the others.
func TestCallConcurrent(t *testing.T) {
	echo := &Echo{release: make(chan struct{})}
	socket := start(t, 1<<20, echo).socket

	var wg sync.WaitGroup
	wg.Go(func() {
		assert.Check(t, Call(t.Context(), socket, "Echo.Wait", &Empty{}, &Empty{}))
	})

	// Answered while the first is still waiting.
	var got []byte
	assert.NilError(t, Call(t.Context(), socket, "Echo.Say", &[]byte{9}, &got))
	assert.DeepEqual(t, got, []byte{9})

	close(echo.release)
	wg.Wait()
}

// TestCallLimit verifies that a client that sends more than the limit
// gets an error, and that the host keeps serving the others.
func TestCallLimit(t *testing.T) {
	socket := start(t, 1<<10, &Echo{}).socket

	var got []byte
	err := Call(t.Context(), socket, "Echo.Say", &[]byte{1}, &got)
	assert.NilError(t, err)

	big := make([]byte, 1<<12)
	assert.Assert(t, Call(t.Context(), socket, "Echo.Say", &big, &got) != nil, "a request over the limit succeeded")

	assert.NilError(t, Call(t.Context(), socket, "Echo.Say", &[]byte{2}, &got))
	assert.DeepEqual(t, got, []byte{2})
}

// TestHostCloseStopsBridge verifies that the host going away stops the
// bridge, which is how the host stops it.
func TestHostCloseStopsBridge(t *testing.T) {
	b := start(t, 1<<20, &Echo{})
	socket := b.socket

	var got []byte
	assert.NilError(t, Call(t.Context(), socket, "Echo.Say", &[]byte{1}, &got))

	b.host.Close()
	assert.NilError(t, <-b.bridged)
	assert.NilError(t, <-b.served)
}

// TestCallWaitsForBridge verifies that a client started before the
// bridge listens waits for it, since the host starts it just after the
// container.
func TestCallWaitsForBridge(t *testing.T) {
	session := newSession(t)
	socket := filepath.Join(t.TempDir(), "late.sock")
	host, container := net.Pipe()

	go func() {
		_ = Serve(t.Context(), host, session.ServerConfig(), 1<<20, func(srv *rpc.Server) error { //nolint:errcheck // Why: ends with the test.
			return srv.RegisterName("Echo", &Echo{})
		})
	}()

	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = Bridge(t.Context(), container, socket) //nolint:errcheck // Why: ends with the test.
	}()

	var got []byte
	assert.NilError(t, Call(t.Context(), socket, "Echo.Say", &[]byte{7}, &got))
	assert.DeepEqual(t, got, []byte{7})
}

// TestCallCanceled verifies that a call stops waiting for the host when
// its context is canceled.
func TestCallCanceled(t *testing.T) {
	echo := &Echo{release: make(chan struct{})}
	defer close(echo.release)

	socket := start(t, 1<<20, echo).socket

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	err := Call(ctx, socket, "Echo.Wait", &Empty{}, &Empty{})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// tapped is a connection that records what's written to it.
type tapped struct {
	net.Conn
	mu      *sync.Mutex
	written *bytes.Buffer
}

// Write implements [net.Conn].
func (c tapped) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.written.Write(p)
	c.mu.Unlock()

	return c.Conn.Write(p)
}

// TestCallRejectsOtherSessions verifies that the host only answers
// clients that hold its own session's key.
func TestCallRejectsOtherSessions(t *testing.T) {
	socket := start(t, 1<<20, &Echo{}).socket

	other, err := NewSession()
	assert.NilError(t, err)

	otherConfig, err := ClientConfig(other.Env())
	assert.NilError(t, err)

	noCert := otherConfig.Clone()
	noCert.Certificates = nil

	// The host's own key, but without its certificate.
	own, err := ClientConfig(os.Getenv(KeyEnv))
	assert.NilError(t, err)

	ownNoCert := own.Clone()
	ownNoCert.Certificates = nil

	tests := []struct {
		name   string
		config *tls.Config
	}{
		{name: "another session's key", config: otherConfig},
		{name: "no certificate", config: ownNoCert},
		{name: "another session's host and no certificate", config: noCert},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []byte
			err := CallTLS(t.Context(), socket, tt.config, "Echo.Say", &[]byte{1}, &got)
			assert.Assert(t, err != nil, "the call succeeded")
		})
	}
}

// TestCallIsEncrypted verifies that what the bridge relays can't be
// read, since it isn't trusted with the call.
func TestCallIsEncrypted(t *testing.T) {
	session := newSession(t)
	socket := filepath.Join(t.TempDir(), "tap.sock")
	host, container := net.Pipe()

	var mu sync.Mutex
	seen := &bytes.Buffer{}
	go func() {
		_ = Serve(t.Context(), host, session.ServerConfig(), 1<<20, func(srv *rpc.Server) error { //nolint:errcheck // Why: ends with the test.
			return srv.RegisterName("Echo", &Echo{})
		})
	}()

	// The bridge's end, so that all it relays is recorded.
	go func() {
		_ = Bridge(t.Context(), tapped{Conn: container, mu: &mu, written: seen}, socket) //nolint:errcheck // Why: ends with the test.
	}()

	secret := []byte("correct horse battery staple")

	var got []byte
	assert.NilError(t, Call(t.Context(), socket, "Echo.Say", &secret, &got))
	assert.DeepEqual(t, got, secret)
	mu.Lock()
	defer mu.Unlock()

	assert.Assert(t, !bytes.Contains(seen.Bytes(), secret), "the bridge saw the reply in plaintext")
}

// TestPlaintextClientIsDropped verifies that a client that doesn't
// speak TLS gets no answer and doesn't hold up the host.
func TestPlaintextClientIsDropped(t *testing.T) {
	b := start(t, 1<<20, &Echo{})

	conn, err := dial(t.Context(), b.socket)
	assert.NilError(t, err)
	defer conn.Close()

	_, err = conn.Write([]byte("not tls at all, but long enough to be a record header"))
	assert.NilError(t, err)

	assert.NilError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))

	// An alert at most, which isn't an rpc answer.
	buf := make([]byte, 1<<10)
	n, _ := conn.Read(buf) //nolint:errcheck // Why: closing or a failure are both fine.
	assert.Assert(t, !bytes.Contains(buf[:n], []byte("Echo")), "plaintext got an answer")

	var got []byte
	assert.NilError(t, Call(t.Context(), b.socket, "Echo.Say", &[]byte{3}, &got))
}

// TestClientConfigErrors verifies that a shim without a usable key
// fails rather than talking without one.
func TestClientConfigErrors(t *testing.T) {
	for _, value := range []string{"", "%%%", "aGVsbG8="} {
		_, err := ClientConfig(value)
		assert.Assert(t, err != nil, "ClientConfig(%q) succeeded", value)
	}

	t.Setenv(KeyEnv, "")
	err := Call(t.Context(), "/nonexistent.sock", "Echo.Say", &Empty{}, &Empty{})
	assert.ErrorContains(t, err, KeyEnv)
}
