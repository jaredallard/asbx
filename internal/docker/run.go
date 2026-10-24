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

// Package docker builds and runs docker run commands.
package docker

import (
	"fmt"
	"slices"
	"strings"
)

// ExitError is returned by Exec when docker exits with a non-zero
// code. docker has already reported why, so the code is all that's
// left to pass on.
type ExitError struct {
	Code int
}

// Error implements error.
func (e *ExitError) Error() string {
	return fmt.Sprintf("docker exited with code %d", e.Code)
}

// Run is the arguments to a docker run command, built up by its
// methods. The zero value is ready to use.
type Run struct {
	args []string

	// env is the NAME=value entries docker runs with or NAME to prevent
	// leakage.
	env []string
}

// Arg adds raw arguments, such as flags without a dedicated method.
func (r *Run) Arg(args ...string) {
	r.args = append(r.args, args...)
}

// Env passes an environment variable into the container. env is either
// NAME, to pass docker's own value through, or NAME=value.
func (r *Run) Env(env string) {
	r.Arg("--env", env)
}

// Secret passes the environment variable name into the container with
// value. Unlike Env, value is never on docker's command line, where
// other processes could read it.
func (r *Run) Secret(name, value string) {
	r.Env(name)
	r.env = append(r.env, name+"="+value)
}

// Mount bind mounts source on the host at target in the container.
func (r *Run) Mount(source, target string, readonly bool) {
	mount := "type=bind,source=" + source + ",target=" + target
	if readonly {
		mount += ",readonly"
	}

	r.Arg("--mount", mount)
}

// Volume mounts a volume, in docker's src:dst form.
func (r *Run) Volume(spec string) {
	r.Arg("--volume", spec)
}

// Args returns the arguments added so far.
func (r *Run) Args() []string {
	return slices.Clone(r.args)
}

// command returns the arguments to docker that run image with cmd.
func (r *Run) command(image string, cmd []string) []string {
	return slices.Concat([]string{"run"}, r.args, []string{image}, cmd)
}

// environ returns base with the secrets added. Secrets replace any
// variable of the same name in base, rather than being duplicates that
// docker may ignore.
func (r *Run) environ(base []string) []string {
	secrets := make(map[string]bool)
	for _, kv := range r.env {
		name, _, _ := strings.Cut(kv, "=")
		secrets[name] = true
	}

	env := slices.Clone(r.env)
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if !secrets[name] {
			env = append(env, kv)
		}
	}

	return env
}
