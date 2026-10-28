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
	"runtime"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"git.rgst.io/jaredallard/asbx/internal/clipbridge"
	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/opbridge"
	"git.rgst.io/jaredallard/asbx/internal/term"
)

// The styles the banner is drawn with. They're the terminal's own
// colors, so the banner follows its theme.
var (
	logoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Magenta)
	nameStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle     = lipgloss.NewStyle().Faint(true)
	statusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Green)
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
)

// The symbols the banner is drawn with.
const (
	symbolLogo    = "🍱"
	symbolDot     = "·"
	symbolWarning = "⚠"
	symbolStep    = "›"
)

// keyWidth is the width the banner's row keys are padded to, that of
// the longest.
const keyWidth = len("workdir")

// networkMode returns the container's network mode, noting when Docker
// on goos runs it in a VM, where the host network is the VM's, not the
// user's machine's.
func networkMode(hostNetwork bool, goos string) string {
	mode := "bridge"
	if hostNetwork {
		mode = "host"
	}

	switch goos {
	case "darwin", "windows":
		mode += " (VM)"
	}

	return mode
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

// render returns the banner, styled, to be reduced to the terminal's
// colors. Rows with nothing to show are left out.
func (b *banner) render() string {
	var s strings.Builder

	fmt.Fprintf(&s, "  %s  %s   %s  %s  %s\n\n",
		logoStyle.Render(symbolLogo),
		nameStyle.Render("asbx"),
		dimStyle.Render(b.version),
		dimStyle.Render(symbolDot),
		statusStyle.Render("sandbox starting"),
	)

	rows := [][2]string{
		{"agent", b.agent},
		{"workdir", term.Escape(b.workDir)},
		{"network", b.network},
		{"secrets", strings.Join(b.secrets, " ")},
		{"extras", strings.Join(b.extras, dimStyle.Render(" "+symbolDot+" "))},
	}
	for _, row := range rows {
		if row[1] == "" {
			continue
		}

		fmt.Fprintf(&s, "    %s  %s\n", dimStyle.Render(fmt.Sprintf("%-*s", keyWidth, row[0])), row[1])
	}

	for _, n := range b.notices {
		msg := term.Escape(n.msg)
		if n.warn {
			fmt.Fprintf(&s, "    %s\n", warningStyle.Render(symbolWarning+" "+msg))
		} else {
			fmt.Fprintf(&s, "    %s\n", dimStyle.Render(symbolDot+" "+msg))
		}
	}

	// Sets the banner apart from what docker and the agent show after.
	s.WriteString("\n")

	return s.String()
}

// newBanner returns the banner for agent running in the sandbox for
// host, with run and allow as built for it, and secrets to be read into
// it.
func newBanner(host *Host, agent Agent, run *docker.Run, allow *opbridge.Allowlist, secrets []Secret) *banner {
	cfg := host.Config

	workDir := host.PWD
	if rest, ok := strings.CutPrefix(workDir, host.Home+"/"); ok {
		workDir = "~/" + rest
	}

	var extras []string
	if cfg.PodmanEnabled {
		extras = append(extras, "podman")
	}

	if !allow.Empty() {
		extras = append(extras, "op bridge ("+count(allow.Len(), "ref")+")")
	}

	// Only when the bridge was set up, which it isn't without a tool.
	if slices.Contains(run.Args(), clipbridge.SocketEnv+"="+clipbridge.Socket) {
		extras = append(extras, "clipboard ("+cfg.Clipboard+")")
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

	names := run.SecretNames()
	for _, secret := range secrets {
		if secret.Ref != "" {
			names = append(names, secret.Env)
		}
	}

	return &banner{
		version: version(),
		agent:   agent.Name(),
		workDir: workDir,
		network: networkMode(cfg.NetworkHost, runtime.GOOS),
		secrets: names,
		extras:  extras,
		notices: host.notices,
	}
}

// announce shows the user at t the banner for agent running in
// the sandbox for host, as for [newBanner], when it's interactive.
// Otherwise only the notices are shown, as plain lines, so they aren't
// lost.
func announce(t *term.Terminal, host *Host, agent Agent, run *docker.Run, allow *opbridge.Allowlist, secrets []Secret) {
	if t.Interactive {
		fmt.Fprint(t.Out, t.Render(newBanner(host, agent, run, allow, secrets).render()))
		return
	}

	for _, n := range host.notices {
		kind := "Note"
		if n.warn {
			kind = "Warning"
		}

		fmt.Fprintf(t.Out, "%s: %s\n", kind, term.Escape(n.msg))
	}
}

// step shows the user at t what asbx is doing after the banner,
// as msg, when it's interactive, like the banner. msg lines up with the
// banner's name, since the logo before it is two columns wide.
func step(t *term.Terminal, msg string) {
	if !t.Interactive {
		return
	}

	fmt.Fprint(t.Out, t.Render(fmt.Sprintf("  %s   %s\n", logoStyle.Render(symbolStep), term.Escape(msg))))
}

// warn shows the user at t a warning about what asbx is doing after the
// banner, as msg, lined up like a step. Otherwise it's shown as a plain
// line, like the banner's notices, so it isn't lost.
func warn(t *term.Terminal, msg string) {
	if !t.Interactive {
		fmt.Fprintf(t.Out, "Warning: %s\n", term.Escape(msg))
		return
	}

	fmt.Fprint(t.Out, t.Render(fmt.Sprintf("  %s\n", warningStyle.Render(symbolWarning+"   "+term.Escape(msg)))))
}

// starting shows the user at t that agent is starting, as the
// last step, set apart from what docker and the agent show after.
func starting(t *term.Terminal, agent string) {
	step(t, "starting "+agent)

	if t.Interactive {
		fmt.Fprintln(t.Out)
	}
}

// goodbye shows the user at t that the agent has exited, as a step set
// apart from what docker and the agent showed before, unless it exited
// with err, which is reported instead.
func goodbye(t *term.Terminal, err error) {
	if err != nil || !t.Interactive {
		return
	}

	fmt.Fprintln(t.Out)
	step(t, "goodbye!")
}
