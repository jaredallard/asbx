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

// Package sandbox builds the docker run command that isolates a coding
// agent in a container with access to only the working directory. The
// parts specific to an agent are added by its Agent.
package sandbox

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/opbridge"
	"go.rgst.io/jaredallard/cmdexec/v2"
)

const (
	// Image is the image the sandbox runs.
	Image = "git.rgst.io/jaredallard/asbx:latest"

	// Entrypoint is the command the sandbox runs, with the agent's
	// arguments.
	Entrypoint = "/opt/asbx/entrypoint.sh"

	// ReadOnlyHostDir is where host files are mounted read-only for the
	// entrypoint to copy into place.
	ReadOnlyHostDir = "/mnt/readonly_host_mnt"

	// sshAuthSock is where the ssh-agent socket is inside the container.
	// Docker for Mac and OrbStack expose the host's agent at this same
	// path in their VM, though it doesn't exist on the host itself.
	sshAuthSock = "/run/host-services/ssh-auth.sock"
)

// Agent is a coding agent that runs inside the sandbox.
type Agent interface {
	// Configure adds what the agent needs to run, such as its secrets,
	// configuration and protected paths, to run.
	Configure(ctx context.Context, host *Host, run *docker.Run) error
}

// forgejo passes the host's Forgejo token and fj configuration into the
// container. The entrypoint removes the token from the configuration,
// it comes from FORGEJO_TOKEN instead.
func forgejo(ctx context.Context, run *docker.Run) error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err
	}

	notLoggedIn := errors.New("unable to determine Forgejo token, log in to fj on this machine with 'fj auth login'")

	path := filepath.Join(configDir, "fj", "config.json")
	if _, err := os.Stat(path); err != nil {
		return notLoggedIn
	}

	cmd := cmdexec.CommandContext(ctx, "fj", "auth", "token")
	cmd.SetStderr(os.Stderr)

	out, err := cmd.Output()
	token := strings.TrimRight(string(out), "\n")
	if err != nil || token == "" {
		return notLoggedIn
	}

	run.Secret("FORGEJO_TOKEN", token)
	run.Mount(path, ReadOnlyHostDir+"/fj-config.json", true)

	return nil
}

// extraMounts mounts each of paths read-only at the same path inside
// the container.
func extraMounts(run *docker.Run, paths []string) error {
	for _, path := range paths {
		if path == "" {
			continue
		}

		if _, err := os.Stat(path); err != nil || !filepath.IsAbs(path) {
			return fmt.Errorf("EXTRA_MOUNTS entry %q must be an absolute path that exists", path)
		}

		run.Mount(path, path, true)
	}

	return nil
}

// sshAgent forwards the host's ssh-agent into the container, at
// sshAuthSock.
func sshAgent(run *docker.Run, goos string) {
	hostSock := sshAuthSock
	if goos != "darwin" {
		hostSock = os.Getenv("SSH_AUTH_SOCK")

		info, err := os.Stat(hostSock) //nolint:gosec // Why: the user's own ssh-agent socket.
		if err != nil || info.Mode()&fs.ModeSocket == 0 {
			fmt.Fprintln(os.Stderr, "Warning: no ssh-agent found at SSH_AUTH_SOCK, git over SSH and commit signing won't work")
			return
		}
	}

	run.Env("SSH_AUTH_SOCK=" + sshAuthSock)
	run.Mount(hostSock, sshAuthSock, false)
}

// opBridge lets op in the container read the refs in entries through
// the host's op. The bridge itself is started by Run, alongside the
// container.
func opBridge(run *docker.Run, entries []string) error {
	allow, err := opbridge.ParseAllowlist(entries)
	if err != nil {
		return err
	}

	if allow.Empty() {
		return nil
	}

	if _, err := cmdexec.LookPath("op"); err != nil {
		return errors.New("op_allowed_secret_refs is set but op isn't installed on this host, install the 1Password CLI")
	}

	run.Env(opbridge.SocketEnv + "=" + opbridge.Socket)

	return nil
}

// protectRepository mounts the files in the working directory that
// could result in execution on the host read-only.
func protectRepository(host *Host, run *docker.Run) error {
	paths := []string{projectConfigFile}

	// A symlinked .git isn't followed, so the agent can't point it
	// somewhere for us to create hooks in.
	gitDir := filepath.Join(host.WorkDir, ".git")
	if info, err := os.Lstat(gitDir); err == nil && info.IsDir() {
		// Created so it's protected before the agent can add hooks.
		if err := os.MkdirAll(filepath.Join(gitDir, "hooks"), 0o750); err != nil {
			return err
		}

		paths = append(paths, ".git/config", ".git/hooks")
	}

	host.ProtectPaths(run, paths...)

	return nil
}

// configure adds the options from the host's configuration that aren't
// specific to an agent.
func configure(ctx context.Context, host *Host, run *docker.Run) error {
	cfg := host.Config

	secrets, err := extraSecrets(cfg.ExtraSecretEnvVars)
	if err != nil {
		return err
	}

	if err := AddSecrets(ctx, run, secrets...); err != nil {
		return err
	}

	if err := opBridge(run, cfg.OPAllowedSecretRefs); err != nil {
		return err
	}

	if cfg.ForgejoEnabled {
		if err := forgejo(ctx, run); err != nil {
			return err
		}
	}

	if err := extraMounts(run, cfg.ExtraMounts); err != nil {
		return err
	}

	sshAgent(run, runtime.GOOS)

	if cfg.NetworkHost {
		run.Arg("--network", "host")
	} else {
		run.Arg("--add-host", "host.docker.internal:host-gateway")
	}

	return protectRepository(host, run)
}

// Build returns the docker run arguments that run agent in a sandbox
// for host.
func Build(ctx context.Context, host *Host, agent Agent) (*docker.Run, error) {
	run := &docker.Run{}
	run.Arg("--rm", "-it")
	run.Env("TERM")
	run.Env("COLORTERM")
	run.Mount(filepath.Join(host.Home, ".gitconfig"), ReadOnlyHostDir+"/.gitconfig", true)

	// Tools mise installs persist in volumes per working directory. The
	// checksum keeps directories with the same name from sharing them.
	id := fmt.Sprintf("%s-%d", filepath.Base(host.PWD), crc32.ChecksumIEEE([]byte(host.PWD)))
	run.Volume("asbx-" + id + "-mise-shims:/home/ubuntu/.local/share/mise")
	run.Volume("asbx-" + id + "-mise-state:/home/ubuntu/.local/state/mise")
	run.Volume("asbx-" + id + "-mise-cache:/home/ubuntu/.cache/mise")

	// Named so the op bridge can find it. The random suffix lets more
	// than one run in the same directory.
	run.SetName("asbx-" + id + "-" + strings.ToLower(rand.Text()[:8]))

	if err := configure(ctx, host, run); err != nil {
		return nil, err
	}

	if err := agent.Configure(ctx, host, run); err != nil {
		return nil, err
	}

	// The working directory is at the same path inside the container,
	// so paths, and agents' per-directory state, match the host's.
	run.Volume(host.PWD + ":" + host.PWD)
	run.Arg("--workdir", host.PWD)
	run.Arg(
		"--cap-drop", "ALL",
		"--cap-add", "CHOWN",
		"--cap-add", "DAC_OVERRIDE",
		"--cap-add", "FOWNER",
		"--cap-add", "SETGID",
		"--cap-add", "SETUID",
	)

	return run, nil
}

// Run runs agent in a sandbox for host with args, attached to the
// terminal, until it exits. A non-zero exit is returned as a
// [docker.ExitError].
func Run(ctx context.Context, host *Host, agent Agent, args []string) error {
	run, err := Build(ctx, host, agent)
	if err != nil {
		return err
	}

	allow, err := opbridge.ParseAllowlist(host.Config.OPAllowedSecretRefs)
	if err != nil {
		return err
	}

	cmd := append([]string{Entrypoint}, args...)
	if allow.Empty() {
		return docker.Exec(ctx, run, Image, cmd)
	}

	// The bridge runs for as long as the container does.
	bridgeCtx, stopBridge := context.WithCancel(ctx)

	var wg sync.WaitGroup
	wg.Go(func() {
		opbridge.Run(bridgeCtx, run.Name(), allow)
	})

	err = docker.Exec(ctx, run, Image, cmd)
	stopBridge()
	wg.Wait()

	return err
}
