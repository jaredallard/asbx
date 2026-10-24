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
	"fmt"
	"strconv"
	"strings"

	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/opbridge"
	"git.rgst.io/jaredallard/asbx/internal/trust"
)

// The ANSI escape sequences the banner is drawn with. They're the
// terminal's own colors, so the banner follows its theme.
const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiMagenta = "\033[35m"
)

// The symbols the banner is drawn with.
const (
	symbolLogo    = "🍱"
	symbolDot     = "·"
	symbolWarning = "⚠"
)

// keyWidth is the width the banner's row keys are padded to, that of
// the longest.
const keyWidth = len("workdir")

// paint returns s in the color code, when color is on.
func paint(color bool, code, s string) string {
	if !color {
		return s
	}

	return code + s + ansiReset
}

// count returns n followed by noun, made plural when n isn't 1.
func count(n int, noun string) string {
	if n != 1 {
		noun += "s"
	}

	return strconv.Itoa(n) + " " + noun
}

// banner is what's shown when the sandbox starts: the version of asbx
// and, briefly, what the agent has access to.
type banner struct {
	version string
	agent   string
	workDir string
	network string

	// secrets are the names of the environment variables passed in as
	// secrets.
	secrets []string

	// extras are the options that are on beyond the defaults, such as
	// podman.
	extras []string

	notices []notice
}

// render returns the banner, in color when color is on. Rows with
// nothing to show are left out.
func (b *banner) render(color bool) string {
	var s strings.Builder

	fmt.Fprintf(&s, "  %s  %s   %s  %s  %s\n\n",
		paint(color, ansiMagenta, symbolLogo),
		paint(color, ansiBold, "asbx"),
		paint(color, ansiDim, b.version),
		paint(color, ansiDim, symbolDot),
		paint(color, ansiGreen, "sandbox starting"),
	)

	rows := [][2]string{
		{"agent", b.agent},
		{"workdir", trust.Escape(b.workDir)},
		{"network", b.network},
		{"secrets", strings.Join(b.secrets, " ")},
		{"extras", strings.Join(b.extras, paint(color, ansiDim, " "+symbolDot+" "))},
	}
	for _, row := range rows {
		if row[1] == "" {
			continue
		}

		fmt.Fprintf(&s, "    %s  %s\n", paint(color, ansiDim, fmt.Sprintf("%-*s", keyWidth, row[0])), row[1])
	}

	for _, n := range b.notices {
		msg := trust.Escape(n.msg)
		if n.warn {
			fmt.Fprintf(&s, "    %s\n", paint(color, ansiYellow, symbolWarning+" "+msg))
		} else {
			fmt.Fprintf(&s, "    %s\n", paint(color, ansiDim, symbolDot+" "+msg))
		}
	}

	// Sets the banner apart from what docker and the agent show after.
	s.WriteString("\n")

	return s.String()
}

// newBanner returns the banner for agent running in the sandbox for
// host, with run and allow as built for it.
func newBanner(host *Host, agent Agent, run *docker.Run, allow *opbridge.Allowlist) *banner {
	cfg := host.Config

	workDir := host.PWD
	if rest, ok := strings.CutPrefix(workDir, host.Home+"/"); ok {
		workDir = "~/" + rest
	}

	network := "bridge"
	if cfg.NetworkHost {
		network = "host"
	}

	var extras []string
	if cfg.PodmanEnabled {
		extras = append(extras, "podman")
	}

	if !allow.Empty() {
		extras = append(extras, "op bridge ("+count(allow.Len(), "ref")+")")
	}

	mounts := 0
	for _, path := range cfg.ExtraMounts {
		if path != "" {
			mounts++
		}
	}

	if mounts > 0 {
		extras = append(extras, count(mounts, "mount"))
	}

	return &banner{
		version: version(),
		agent:   agent.Name(),
		workDir: workDir,
		network: network,
		secrets: run.SecretNames(),
		extras:  extras,
		notices: host.notices,
	}
}

// announce shows the user the banner for agent running in the sandbox
// for host, when asbx is running in a terminal. Otherwise only the
// notices are shown, as plain lines, so they aren't lost.
func announce(host *Host, agent Agent, run *docker.Run, allow *opbridge.Allowlist) {
	term := terminal()
	if term.Interactive {
		fmt.Fprint(term.Out, newBanner(host, agent, run, allow).render(term.Color))
		return
	}

	for _, n := range host.notices {
		kind := "Note"
		if n.warn {
			kind = "Warning"
		}

		fmt.Fprintf(term.Out, "%s: %s\n", kind, trust.Escape(n.msg))
	}
}
