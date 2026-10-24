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
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/onepassword"
)

// Secret is an environment variable passed into the container whose
// value is read from 1Password.
type Secret struct {
	// Env is the name of the environment variable.
	Env string

	// Ref is the op:// reference to read the value from. The secret is
	// skipped when it's empty.
	Ref string

	// Name describes the secret in errors.
	Name string

	// Help tells the user how to create the secret when it can't be
	// read, as a lowercase phrase such as "create it at <url>".
	Help string
}

// AddSecrets reads each of secrets from 1Password and passes it into
// the container.
func AddSecrets(ctx context.Context, run *docker.Run, secrets ...Secret) error {
	for _, s := range secrets {
		if s.Ref == "" {
			continue
		}

		value, err := onepassword.Read(ctx, s.Ref)
		if err != nil {
			return fmt.Errorf("unable to determine %s, %s and store it in 1Password at %q: %w", s.Name, s.Help, s.Ref, err)
		}

		run.Secret(s.Env, value)
	}

	return nil
}

// envNamePattern matches valid environment variable names.
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// extraSecrets returns the secrets for extra_secret_env_vars, which
// maps environment variable names to op:// refs. They're sorted by
// name, so the container is always started the same way. Names with
// an empty ref are skipped.
func extraSecrets(refs map[string]string) ([]Secret, error) {
	var secrets []Secret
	for _, name := range slices.Sorted(maps.Keys(refs)) {
		ref := refs[name]
		if ref == "" {
			continue
		}

		if !envNamePattern.MatchString(name) {
			return nil, fmt.Errorf("extra_secret_env_vars name %q isn't a valid environment variable name", name)
		}

		if !strings.HasPrefix(ref, "op://") {
			return nil, fmt.Errorf("extra_secret_env_vars ref %q for %s must be an op:// ref", ref, name)
		}

		secrets = append(secrets, Secret{
			Env:  name,
			Ref:  ref,
			Name: name,
			Help: "create it",
		})
	}

	return secrets, nil
}
