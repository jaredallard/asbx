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
	"strings"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/opbridge"
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
	assert.Equal(t, b.render(false), want)
}

// TestBannerRenderOmitsEmptyRows verifies that rows with nothing to
// show, such as no secrets, aren't drawn.
func TestBannerRenderOmitsEmptyRows(t *testing.T) {
	b := banner{version: "dev", agent: "claude", workDir: "/w", network: "host"}

	got := b.render(false)
	assert.Assert(t, !strings.Contains(got, "secrets"), "render() =\n%s", got)
	assert.Assert(t, !strings.Contains(got, "extras"), "render() =\n%s", got)
}

// TestBannerRenderColor verifies that, with color, the banner is drawn
// with the terminal's own colors.
func TestBannerRenderColor(t *testing.T) {
	b := banner{
		version: "dev",
		agent:   "claude",
		workDir: "/w",
		network: "bridge",
		notices: []notice{{warn: true, msg: "careful"}},
	}

	got := b.render(true)
	for _, want := range []string{
		ansiMagenta + symbolLogo + ansiReset,
		ansiGreen + "sandbox starting" + ansiReset,
		ansiDim + "agent  " + ansiReset,
		ansiYellow + symbolWarning + " careful" + ansiReset,
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

	got := b.render(false)
	assert.Assert(t, !strings.Contains(got, "\x1b"), "render() =\n%q", got)
	assert.Assert(t, strings.Contains(got, `workdir  /w/\x1b[2Jrepo`), "render() =\n%q", got)
	assert.Assert(t, strings.Contains(got, `in /w/\x1b[2Jrepo`), "render() =\n%q", got)
}

// TestNewBanner verifies that the banner shows the working directory
// relative to home, the secrets passed in, and the options that are on.
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

	got := newBanner(host, &fakeAgent{}, run, allow)
	assert.DeepEqual(t, got, &banner{
		version: version(),
		agent:   "fake",
		workDir: "~/src/repo",
		network: "host",
		secrets: []string{"GH_TOKEN"},
		extras:  []string{"podman", "op bridge (1 ref)", "2 mounts"},
		notices: []notice{{warn: true, msg: "no ssh-agent"}},
	}, cmp.AllowUnexported(banner{}, notice{}))
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
