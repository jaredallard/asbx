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

package clipshim

import (
	"bytes"
	"context"
	"net"
	"net/rpc"
	"path/filepath"
	"strings"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/clipbridge"
	"git.rgst.io/jaredallard/asbx/internal/config"
	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
	"github.com/google/go-cmp/cmp"
	"gotest.tools/v3/assert"
)

// cmpAction compares the unexported fields of an action.
var cmpAction = cmp.AllowUnexported(action{})

// memory is a [clipbridge.Clipboard] that holds one value.
type memory struct {
	data []byte
}

// Copy implements [clipbridge.Clipboard].
func (m *memory) Copy(_ context.Context, _ string, data []byte) error {
	m.data = data
	return nil
}

// Paste implements [clipbridge.Clipboard].
func (m *memory) Paste(context.Context, string) ([]byte, error) {
	return m.data, nil
}

// Types implements [clipbridge.Clipboard].
func (m *memory) Types(context.Context) ([]string, error) {
	return []string{clipbridge.TextType}, nil
}

// serveMode starts a bridge at socket that's answered by the host in
// mode, with a clipboard in memory, until the test ends.
func serveMode(t *testing.T, socket, mode string) {
	t.Helper()

	session, err := stdiobridge.NewSession()
	assert.NilError(t, err)
	t.Setenv(stdiobridge.KeyEnv, session.Env())

	hostConn, bridgeConn := net.Pipe()

	// Shared, since every client is served with its own rpc server.
	cb := &memory{}

	go func() {
		//nolint:errcheck // Why: ends with the test.
		_ = stdiobridge.Serve(t.Context(), hostConn, session.ServerConfig(), 12<<20, func(srv *rpc.Server) error {
			return srv.RegisterName("Clipboard", clipbridge.NewService(t.Context(), mode, cb))
		})
	}()

	go func() {
		_ = stdiobridge.Bridge(t.Context(), bridgeConn, socket) //nolint:errcheck // Why: ends with the test.
	}()
}

// TestParse verifies that each tool's common options are understood.
func TestParse(t *testing.T) {
	text := "hi there"

	tests := []struct {
		name string
		tool string
		args []string
		want action
	}{
		{"WlCopyType", "wl-copy", []string{"--type", "image/png"}, action{op: "copy", mime: "image/png"}},
		{"WlCopyText", "wl-copy", []string{"hi", "there"}, action{op: "copy", text: &text}},
		{"WlPasteDefault", "wl-paste", nil, action{op: "paste"}},
		{"WlPasteList", "wl-paste", []string{"-l"}, action{op: "types"}},
		{"WlPasteImage", "wl-paste", []string{"--no-newline", "--type=image/png"}, action{op: "paste", mime: "image/png", noNewline: true}},
		{"XclipCopy", "xclip", []string{"-selection", "clipboard"}, action{op: "copy"}},
		{"XclipTargets", "xclip", []string{"-selection", "clipboard", "-t", "TARGETS", "-o"}, action{op: "types"}},
		{"XclipImage", "xclip", []string{"-selection", "clipboard", "-t", "image/png", "-o"}, action{op: "paste", mime: "image/png"}},
		{"XselOutput", "xsel", []string{"--clipboard", "--output"}, action{op: "paste"}},
		{"XselInput", "xsel", []string{"-b", "-i"}, action{op: "copy"}},
		{"Pbcopy", "pbcopy", nil, action{op: "copy"}},
		{"Pbpaste", "pbpaste", nil, action{op: "paste"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parse(tt.tool, tt.args)
			assert.NilError(t, err)
			assert.DeepEqual(t, *got, tt.want, cmpAction)
		})
	}
}

// TestParseUnsupported verifies that an option that isn't supported is
// an error rather than ignored.
func TestParseUnsupported(t *testing.T) {
	for tool, arg := range map[string]string{
		"wl-copy":  "--watch",
		"wl-paste": "--watch",
		"xclip":    "-bogus",
		"xsel":     "--bogus",
		"pbcopy":   "-pboard",
	} {
		_, err := parse(tool, []string{arg})
		assert.ErrorContains(t, err, "isn't supported", tool)
	}
}

// TestRunUnavailable verifies that a tool without a bridge fails with
// the reason, rather than silently doing nothing.
func TestRunUnavailable(t *testing.T) {
	var out bytes.Buffer
	err := Run(context.Background(), "wl-copy", nil, IO{In: strings.NewReader("x"), Out: &out}, "")
	assert.ErrorContains(t, err, "set clipboard")
}

// TestRunDenied verifies that a tool is told why when the host doesn't
// allow it, using a bridge that answers like the host in copy mode.
func TestRunDenied(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "clip.sock")
	serveMode(t, socket, config.ClipboardCopy)

	var out bytes.Buffer
	err := Run(t.Context(), "wl-paste", nil, IO{In: strings.NewReader(""), Out: &out}, socket)
	assert.ErrorContains(t, err, "copy_paste")
	assert.Equal(t, out.Len(), 0)

	err = Run(t.Context(), "wl-copy", nil, IO{In: strings.NewReader("hi"), Out: &out}, socket)
	assert.NilError(t, err)
}

// TestRunPasteNewline verifies that wl-paste ends text with a newline
// unless told not to, and that the other tools don't add one.
func TestRunPasteNewline(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "clip.sock")
	serveMode(t, socket, config.ClipboardCopyPaste)

	run := func(tool string, args ...string) string {
		var out bytes.Buffer
		err := Run(t.Context(), tool, args, IO{In: strings.NewReader("abc"), Out: &out}, socket)
		assert.NilError(t, err)

		return out.String()
	}

	run("pbcopy")
	assert.Equal(t, run("wl-paste"), "abc\n")
	assert.Equal(t, run("wl-paste", "-n"), "abc")
	assert.Equal(t, run("pbpaste"), "abc")
	assert.Equal(t, run("xclip", "-o"), "abc")
}
