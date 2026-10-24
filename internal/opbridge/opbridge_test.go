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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"
)

// fakeOP is a [ReadFunc] that returns the secrets in it, recording
// the refs it was asked for.
type fakeOP struct {
	secrets map[string]string
	refs    []string
}

// read implements [ReadFunc].
func (f *fakeOP) read(_ context.Context, ref string) (string, error) {
	f.refs = append(f.refs, ref)

	value, ok := f.secrets[ref]
	if !ok {
		return "", errors.New("item not found")
	}

	return value, nil
}

// TestServe verifies that each request is answered in order, that op
// is only asked for allowed refs, and that its errors are passed on.
func TestServe(t *testing.T) {
	allow, err := ParseAllowlist([]string{"op://Dev/*/token"})
	assert.NilError(t, err)

	op := &fakeOP{secrets: map[string]string{"op://Dev/github/token": "hunter2"}}

	var requests strings.Builder
	for _, ref := range []string{"op://Dev/github/token", "op://Private/bank/password", "op://Dev/missing/token"} {
		b, err := json.Marshal(Request{Ref: ref})
		assert.NilError(t, err)

		requests.Write(append(b, '\n'))
	}

	var out bytes.Buffer
	assert.NilError(t, Serve(t.Context(), strings.NewReader(requests.String()), &out, allow, op.read))

	var got []Response
	dec := json.NewDecoder(&out)
	for {
		var resp Response
		err := dec.Decode(&resp)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Serve() wrote invalid JSON: %v", err)
		}

		got = append(got, resp)
	}

	assert.DeepEqual(t, got, []Response{
		{Value: "hunter2"},
		{Error: `"op://Private/bank/password" isn't in OP_ALLOWED_SECRET_REFS`},
		{Error: "item not found"},
	})
	assert.DeepEqual(t, op.refs, []string{"op://Dev/github/token", "op://Dev/missing/token"})
}

// TestServeInvalidRequest verifies that a request that isn't JSON stops
// the host from serving the container.
func TestServeInvalidRequest(t *testing.T) {
	op := &fakeOP{}

	err := Serve(t.Context(), strings.NewReader("not json\n"), io.Discard, &Allowlist{}, op.read)
	assert.ErrorContains(t, err, "invalid request from the container")
}

// TestServeRequestTooLong verifies that the container can't make the
// host buffer a request without limit.
func TestServeRequestTooLong(t *testing.T) {
	op := &fakeOP{}
	long := strings.Repeat("a", maxRequest+1)

	err := Serve(t.Context(), strings.NewReader(long), io.Discard, &Allowlist{}, op.read)
	assert.Assert(t, err != nil, "Serve() = nil, want error for a %d byte request", len(long))
}

// TestBridgeRead verifies that op in the container reads secrets from
// the host through the bridge, and gets the host's errors.
func TestBridgeRead(t *testing.T) {
	allow, err := ParseAllowlist([]string{"op://Dev/github/token"})
	assert.NilError(t, err)

	op := &fakeOP{secrets: map[string]string{"op://Dev/github/token": "hunter2"}}
	socket := filepath.Join(t.TempDir(), "op.sock")

	// toHost and toContainer are the bridge's stdout and stdin.
	hostR, toHost := io.Pipe()
	containerR, toContainer := io.Pipe()

	served := make(chan error, 1)
	go func() {
		served <- Serve(t.Context(), hostR, toContainer, allow, op.read)
	}()

	bridged := make(chan error, 1)
	go func() {
		bridged <- Bridge(t.Context(), containerR, toHost, socket)
	}()

	got, err := Read(t.Context(), socket, "op://Dev/github/token")
	assert.NilError(t, err)
	assert.Equal(t, got, "hunter2")

	_, err = Read(t.Context(), socket, "op://Dev/github/other")
	assert.ErrorContains(t, err, "isn't in OP_ALLOWED_SECRET_REFS")

	// The host going away stops the bridge.
	toContainer.Close()
	assert.NilError(t, <-bridged)

	toHost.Close()
	assert.NilError(t, <-served)
}

// TestReadUnavailable verifies that op in the container fails straight
// away when the bridge isn't enabled.
func TestReadUnavailable(t *testing.T) {
	_, err := Read(t.Context(), "", "op://a/b/c")
	assert.ErrorIs(t, err, ErrUnavailable)
}
