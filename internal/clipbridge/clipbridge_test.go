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

package clipbridge

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"path/filepath"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/config"
	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
	"gotest.tools/v3/assert"
)

// fake is a [Clipboard] that holds one value.
type fake struct {
	mime string
	data []byte
	err  error
}

// Copy implements [Clipboard].
func (f *fake) Copy(_ context.Context, mime string, data []byte) error {
	f.mime, f.data = mime, data
	return f.err
}

// Paste implements [Clipboard].
func (f *fake) Paste(context.Context, string) ([]byte, error) {
	return f.data, f.err
}

// Types implements [Clipboard].
func (f *fake) Types(context.Context) ([]string, error) {
	return []string{f.mime}, f.err
}

// TestServiceCopy verifies that copying sets the clipboard, defaulting
// to text, in every mode but off.
func TestServiceCopy(t *testing.T) {
	for _, mode := range []string{config.ClipboardCopy, config.ClipboardCopyPaste} {
		cb := &fake{}
		err := NewService(t.Context(), mode, cb).Copy(&CopyArgs{Data: []byte("hi")}, &stdiobridge.Empty{})
		assert.NilError(t, err)
		assert.Equal(t, cb.mime, TextType)
		assert.Equal(t, string(cb.data), "hi")
	}

	cb := &fake{}
	err := NewService(t.Context(), config.ClipboardOff, cb).Copy(&CopyArgs{Data: []byte("hi")}, &stdiobridge.Empty{})
	assert.ErrorContains(t, err, "isn't enabled")
	assert.Equal(t, len(cb.data), 0)
}

// TestServiceCopyTooLarge verifies that data over the limit isn't
// copied.
func TestServiceCopyTooLarge(t *testing.T) {
	cb := &fake{}
	err := NewService(t.Context(), config.ClipboardCopy, cb).Copy(&CopyArgs{Data: make([]byte, MaxData+1)}, &stdiobridge.Empty{})
	assert.ErrorContains(t, err, "refusing")
	assert.Equal(t, len(cb.data), 0)
}

// TestServicePasteNeedsCopyPaste verifies that the container can only
// read the host's clipboard in copy_paste mode, since it may hold
// secrets.
func TestServicePasteNeedsCopyPaste(t *testing.T) {
	cb := &fake{mime: TextType, data: []byte("secret")}

	for _, mode := range []string{config.ClipboardOff, config.ClipboardCopy} {
		svc := NewService(t.Context(), mode, cb)

		var data []byte
		assert.ErrorContains(t, svc.Paste(&PasteArgs{}, &data), "copy_paste", mode)
		assert.Equal(t, len(data), 0)

		var types []string
		assert.ErrorContains(t, svc.Types(&stdiobridge.Empty{}, &types), "copy_paste", mode)
		assert.Equal(t, len(types), 0)
	}

	svc := NewService(t.Context(), config.ClipboardCopyPaste, cb)

	var data []byte
	assert.NilError(t, svc.Paste(&PasteArgs{}, &data))
	assert.Equal(t, string(data), "secret")

	var types []string
	assert.NilError(t, svc.Types(&stdiobridge.Empty{}, &types))
	assert.DeepEqual(t, types, []string{TextType})
}

// TestServiceErrors verifies that clipboard failures are reported
// rather than dropped.
func TestServiceErrors(t *testing.T) {
	svc := NewService(t.Context(), config.ClipboardCopyPaste, &fake{err: errors.New("no display")})

	assert.Error(t, svc.Copy(&CopyArgs{}, &stdiobridge.Empty{}), "no display")

	var data []byte
	assert.Error(t, svc.Paste(&PasteArgs{}, &data), "no display")
}

// TestBridgeRoundTrip verifies that a client's copy, paste and listing
// reach the host's clipboard through the bridge and back, including
// the largest copy and an empty clipboard.
func TestBridgeRoundTrip(t *testing.T) {
	cb := &fake{}
	socket := filepath.Join(t.TempDir(), "clip.sock")

	session, err := stdiobridge.NewSession()
	assert.NilError(t, err)
	t.Setenv(stdiobridge.KeyEnv, session.Env())

	hostConn, bridgeConn := net.Pipe()
	go func() {
		//nolint:errcheck // Why: ends with the test.
		_ = stdiobridge.Serve(t.Context(), hostConn, session.ServerConfig(), maxRequest, func(srv *rpc.Server) error {
			return srv.RegisterName(serviceName, NewService(t.Context(), config.ClipboardCopyPaste, cb))
		})
	}()

	go func() {
		_ = stdiobridge.Bridge(t.Context(), bridgeConn, socket) //nolint:errcheck // Why: ends with the test.
	}()

	assert.NilError(t, Copy(t.Context(), socket, "text/plain", []byte("hello")))
	assert.Equal(t, string(cb.data), "hello")

	got, err := Paste(t.Context(), socket, "text/plain")
	assert.NilError(t, err)
	assert.Equal(t, string(got), "hello")

	types, err := Types(t.Context(), socket)
	assert.NilError(t, err)
	assert.DeepEqual(t, types, []string{"text/plain"})

	big := make([]byte, MaxData)
	big[0], big[len(big)-1] = 1, 2
	assert.NilError(t, Copy(t.Context(), socket, "image/png", big))
	assert.Equal(t, len(cb.data), MaxData)

	got, err = Paste(t.Context(), socket, "image/png")
	assert.NilError(t, err)
	assert.Equal(t, len(got), MaxData)

	cb.data = nil
	got, err = Paste(t.Context(), socket, "")
	assert.NilError(t, err)
	assert.Equal(t, len(got), 0)

	cb.err = errors.New("boom")
	assert.ErrorContains(t, Copy(t.Context(), socket, "text/plain", nil), "boom")
}

// TestCallUnavailable verifies that a client without a socket reports
// that the bridge isn't enabled.
func TestCallUnavailable(t *testing.T) {
	assert.ErrorIs(t, Copy(t.Context(), "", TextType, []byte("x")), ErrUnavailable)

	_, err := Paste(t.Context(), "", TextType)
	assert.ErrorIs(t, err, ErrUnavailable)

	_, err = Types(t.Context(), "")
	assert.ErrorIs(t, err, ErrUnavailable)
}
