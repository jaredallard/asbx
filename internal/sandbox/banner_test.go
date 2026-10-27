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

package sandbox

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/opbridge"
	"git.rgst.io/jaredallard/asbx/internal/term"
	"github.com/google/go-cmp/cmp"
	"gotest.tools/v3/assert"
)

// TestBannerRender verifies the banner's layout without color: the
// header, then aligned rows, then the notices.
func TestBannerRender(t *testing.T) {
	b := banner{
		version: "v1.2.3",
		agent:   "claude",
		workDir: "~/src/repo",
		network: "bridge",
		secrets: []string{"GH_TOKEN", "FORGEJO_TOKEN"},
		extras:  []string{"podman", "2 mounts"},
		notices: []notice{{warn: true, msg: "no ssh-agent"}, {msg: "pinned it"}},
	}

	want := "  🍱  asbx   v1.2.3  ·  sandbox starting\n" +
		"\n" +
		"    agent    claude\n" +
		"    workdir  ~/src/repo\n" +
		"    network  bridge\n" +
		"    secrets  GH_TOKEN FORGEJO_TOKEN\n" +
		"    extras   podman · 2 mounts\n" +
		"    ⚠ no ssh-agent\n" +
		"    · pinned it\n" +
		"\n"
	assert.Equal(t, (&term.Terminal{}).Render(b.render()), want)
}

// TestBannerRenderOmitsEmptyRows verifies that rows with nothing to
// show, such as no secrets, aren't drawn.
func TestBannerRenderOmitsEmptyRows(t *testing.T) {
	b := banner{version: "dev", agent: "claude", workDir: "/w", network: "host"}

	got := b.render()
	assert.Assert(t, !strings.Contains(got, "secrets"), "render() =\n%s", got)
	assert.Assert(t, !strings.Contains(got, "extras"), "render() =\n%s", got)
}

// TestBannerRenderColor verifies that the banner is drawn with the
// terminal's own colors.
func TestBannerRenderColor(t *testing.T) {
	b := banner{
		version: "dev",
		agent:   "claude",
		workDir: "/w",
		network: "bridge",
		notices: []notice{{warn: true, msg: "careful"}},
	}

	got := b.render()
	for _, want := range []string{
		"\033[35m" + symbolLogo + "\033[m",
		"\033[32msandbox starting\033[m",
		"\033[2magent  \033[m",
		"\033[33m" + symbolWarning + " careful\033[m",
	} {
		assert.Assert(t, strings.Contains(got, want), "render() =\n%q\nwant it to contain %q", got, want)
	}
}

// TestBannerRenderEscapes verifies that the working directory and
// notices are drawn with control characters escaped, so a directory's
// name can't send the terminal escape sequences.
func TestBannerRenderEscapes(t *testing.T) {
	b := banner{
		version: "dev",
		agent:   "claude",
		workDir: "/w/\x1b[2Jrepo",
		network: "bridge",
		notices: []notice{{msg: "in /w/\x1b[2Jrepo"}},
	}

	got := (&term.Terminal{}).Render(b.render())
	assert.Assert(t, !strings.Contains(got, "\x1b"), "render() =\n%q", got)
	assert.Assert(t, strings.Contains(got, `workdir  /w/\x1b[2Jrepo`), "render() =\n%q", got)
	assert.Assert(t, strings.Contains(got, `in /w/\x1b[2Jrepo`), "render() =\n%q", got)
}

// TestNewBanner verifies that the banner shows the working directory
// relative to home, the secrets passed in or to be read, and the options
// that are on.
func TestNewBanner(t *testing.T) {
	host := newTestHost(t)
	host.PWD = host.Home + "/src/repo"
	host.Config.NetworkHost = true
	host.Config.PodmanEnabled = true
	host.Config.ExtraMounts = []string{"/a", "", "/b"}
	host.Warn("no %s", "ssh-agent")

	run := &docker.Run{}
	run.Secret("GH_TOKEN", "secret")

	allow, err := opbridge.ParseAllowlist([]string{"op://vault/item/field"})
	assert.NilError(t, err)

	secrets := []Secret{{Env: "SKIPPED"}, {Env: "TOKEN", Ref: "op://a/b/c"}}

	got := newBanner(host, &fakeAgent{}, run, allow, secrets)
	assert.DeepEqual(t, got, &banner{
		version: version(),
		agent:   "fake",
		workDir: "~/src/repo",
		network: networkMode(true, runtime.GOOS),
		secrets: []string{"GH_TOKEN", "TOKEN"},
		extras:  []string{"podman", "op bridge (1 ref)", "2 mounts"},
		notices: []notice{{warn: true, msg: "no ssh-agent"}},
	}, cmp.AllowUnexported(banner{}, notice{}))
}

// TestNetworkMode verifies that the network mode is the one configured,
// noted as being in a VM where Docker runs in one.
func TestNetworkMode(t *testing.T) {
	tests := []struct {
		goos        string
		hostNetwork bool
		want        string
	}{
		{goos: "linux", want: "bridge"},
		{goos: "linux", hostNetwork: true, want: "host"},
		{goos: "darwin", want: "bridge (VM)"},
		{goos: "darwin", hostNetwork: true, want: "host (VM)"},
		{goos: "windows", want: "bridge (VM)"},
		{goos: "windows", hostNetwork: true, want: "host (VM)"},
	}

	for _, tt := range tests {
		assert.Equal(t, networkMode(tt.hostNetwork, tt.goos), tt.want, "networkMode(%v, %q)", tt.hostNetwork, tt.goos)
	}
}

// TestVersion verifies that releases report their tag and other builds
// report that they're a development build.
func TestVersion(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		want string
	}{
		{name: "release", tag: "v1.2.3", want: "v1.2.3"},
		{name: "dev", tag: "latest", want: "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saved := imageTag
			imageTag = tt.tag
			t.Cleanup(func() { imageTag = saved })

			got := version()
			assert.Assert(t, strings.HasPrefix(got, tt.want), "version() with tag %q = %q, want prefix %q", tt.tag, got, tt.want)
		})
	}
}

// TestStarting verifies that the agent starting is shown as a step,
// lined up with the banner's name and set apart from what follows, but
// only when there's a terminal to show it at.
func TestStarting(t *testing.T) {
	tests := []struct {
		name        string
		interactive bool
		want        string
	}{
		{name: "Interactive", interactive: true, want: "  ›   starting claude\n\n"},
		{name: "NotInteractive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			starting(&term.Terminal{Out: &out, Interactive: tt.interactive}, "claude")
			assert.Equal(t, out.String(), tt.want)
		})
	}
}

// TestGoodbye verifies that the agent exiting is shown as a step, set
// apart from what came before, but only when it exited without an error
// and there's a terminal to show it at.
func TestGoodbye(t *testing.T) {
	tests := []struct {
		name        string
		interactive bool
		err         error
		want        string
	}{
		{name: "Interactive", interactive: true, want: "\n  ›   goodbye!\n"},
		{name: "Error", interactive: true, err: errors.New("exit status 1")},
		{name: "NotInteractive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			goodbye(&term.Terminal{Out: &out, Interactive: tt.interactive}, tt.err)
			assert.Equal(t, out.String(), tt.want)
		})
	}
}

// TestWarn verifies that a warning after the banner is lined up like a
// step when there's a terminal to show it at, and is a plain line
// otherwise, so it isn't lost.
func TestWarn(t *testing.T) {
	tests := []struct {
		name        string
		interactive bool
		want        string
	}{
		{name: "Interactive", interactive: true, want: "  ⚠   couldn't get the latest image\n"},
		{name: "NotInteractive", want: "Warning: couldn't get the latest image\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			warn(&term.Terminal{Out: &out, Interactive: tt.interactive}, "couldn't get the latest image")
			assert.Equal(t, out.String(), tt.want)
		})
	}
}
