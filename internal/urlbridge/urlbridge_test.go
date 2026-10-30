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

package urlbridge

import (
	"context"
	"errors"
	"net"
	"net/rpc"
	"path/filepath"
	"strings"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
	"go.rgst.io/jaredallard/cmdexec/v2"
	"gotest.tools/v3/assert"
)

// fake is an [Opener] that remembers what it opened.
type fake struct {
	opened []string
	err    error
}

// Open implements [Opener].
func (f *fake) Open(_ context.Context, url string) error {
	f.opened = append(f.opened, url)
	return f.err
}

// TestValidate verifies that only http and https URLs with a host can
// be opened.
func TestValidate(t *testing.T) {
	tests := []struct {
		url     string
		wantErr string
	}{
		{url: "https://example.com"},
		{url: "http://localhost:8080/callback?code=abc"},
		{url: "HTTPS://example.com/a#b"},
		{url: "file:///etc/passwd", wantErr: "only http and https"},
		{url: "javascript:alert(1)", wantErr: "only http and https"},
		{url: "mailto:a@example.com", wantErr: "only http and https"},
		{url: "vscode://file/etc/passwd", wantErr: "only http and https"},
		{url: "/etc/passwd", wantErr: "only http and https"},
		{url: "-a Calculator", wantErr: "only http and https"},
		{url: "", wantErr: "only http and https"},
		{url: "http:example.com", wantErr: "no host"},
		{url: "https:///path", wantErr: "no host"},
		{url: "https://example.com/\n", wantErr: "invalid control character"},
		{url: "https://example.com/" + strings.Repeat("a", MaxURL), wantErr: "longer than"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			err := Validate(tt.url)
			if tt.wantErr == "" {
				assert.NilError(t, err)
				return
			}

			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

// TestServiceOpen verifies that the host opens valid URLs, and checks
// them itself rather than trusting the container to have.
func TestServiceOpen(t *testing.T) {
	opener := &fake{}
	svc := NewService(t.Context(), opener)

	assert.NilError(t, svc.Open(&OpenArgs{URL: "https://example.com"}, &stdiobridge.Empty{}))
	assert.ErrorContains(t, svc.Open(&OpenArgs{URL: "file:///etc/passwd"}, &stdiobridge.Empty{}), "only http and https")
	assert.DeepEqual(t, opener.opened, []string{"https://example.com"})

	opener.err = errors.New("no browser")
	assert.Error(t, svc.Open(&OpenArgs{URL: "https://example.com"}, &stdiobridge.Empty{}), "no browser")
}

// TestBridgeRoundTrip verifies that a client's URL reaches the host's
// opener through the bridge, and its failure comes back.
func TestBridgeRoundTrip(t *testing.T) {
	opener := &fake{}
	socket := filepath.Join(t.TempDir(), "open.sock")

	session, err := stdiobridge.NewSession()
	assert.NilError(t, err)
	t.Setenv(stdiobridge.KeyEnv, session.Env())

	hostConn, bridgeConn := net.Pipe()
	go func() {
		//nolint:errcheck // Why: ends with the test.
		_ = stdiobridge.Serve(t.Context(), hostConn, session.ServerConfig(), maxRequest, func(srv *rpc.Server) error {
			return srv.RegisterName(serviceName, NewService(t.Context(), opener))
		})
	}()

	go func() {
		_ = stdiobridge.Bridge(t.Context(), bridgeConn, socket) //nolint:errcheck // Why: ends with the test.
	}()

	assert.NilError(t, Open(t.Context(), socket, "https://example.com/login"))
	assert.DeepEqual(t, opener.opened, []string{"https://example.com/login"})

	long := "https://example.com/" + strings.Repeat("a", MaxURL-len("https://example.com/"))
	assert.NilError(t, Open(t.Context(), socket, long))
	assert.Equal(t, opener.opened[1], long)

	opener.err = errors.New("boom")
	assert.ErrorContains(t, Open(t.Context(), socket, "https://example.com"), "boom")
}

// TestOpenRefusedLocally verifies that a URL that can't be opened is
// refused before it's sent, and that a client without a socket reports
// that the bridge isn't enabled.
func TestOpenRefusedLocally(t *testing.T) {
	assert.ErrorIs(t, Open(t.Context(), "", "https://example.com"), ErrUnavailable)

	// There's nothing listening, so reaching the bridge would fail
	// differently.
	socket := filepath.Join(t.TempDir(), "open.sock")
	assert.ErrorContains(t, Open(t.Context(), socket, "file:///etc/passwd"), "only http and https")
}

// TestDetect verifies that macOS opens URLs with open and everything
// else with xdg-open, and that a host without it has no opener.
func TestDetect(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		has     bool
		want    Opener
		wantErr error
	}{
		{name: "Darwin", goos: "darwin", has: true, want: tool("open")},
		{name: "Linux", goos: "linux", has: true, want: tool("xdg-open")},
		{name: "LinuxNoTool", goos: "linux", wantErr: ErrNoTool},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := cmdexec.NewMockExecutor()
			for _, name := range []string{"open", "xdg-open"} {
				if tt.has {
					mock.AddLookPath(name, "/usr/bin/"+name)
				} else {
					mock.AddLookPathError(name, errors.New("executable file not found in $PATH"))
				}
			}
			cmdexec.UseMockExecutor(t, mock)

			got, err := Detect(tt.goos)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, got, tt.want)
		})
	}
}

// TestToolOpen verifies that the tool is run with only the URL, and
// that its failure is reported with what it said.
func TestToolOpen(t *testing.T) {
	mock := cmdexec.NewMockExecutor()
	mock.AddCommand(&cmdexec.MockCommand{Name: "xdg-open", Args: []string{"https://example.com"}})
	mock.AddCommand(&cmdexec.MockCommand{
		Name: "xdg-open",
		Args: []string{"https://example.com/fail"},
		Err:  errors.New("exit status 3"),
	})
	cmdexec.UseMockExecutor(t, mock)

	assert.NilError(t, tool("xdg-open").Open(t.Context(), "https://example.com"))
	assert.ErrorContains(t, tool("xdg-open").Open(t.Context(), "https://example.com/fail"), "xdg-open: exit status 3")
}
