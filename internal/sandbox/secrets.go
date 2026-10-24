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
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/onepassword"
	"git.rgst.io/jaredallard/asbx/internal/term"
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

	// Instructions tells the user how to get the secret's value, as full
	// sentences, when they're asked to paste it. It's only used when
	// Create is set.
	Instructions string

	// Create is whether the user is offered to create Ref's item, as an
	// API Credential with Ref's field, when it doesn't exist. It's only
	// for the default refs, whose items asbx knows the shape of.
	Create bool
}

// create offers the user at t to create the item for s, which
// couldn't be read, and returns the secret they enter for it. ok is
// false, with a nil error, when the item exists, so it couldn't be read
// for another reason, or the user declined.
func create(ctx context.Context, t *term.Terminal, s *Secret) (secret string, ok bool, err error) {
	vault, item, field, err := onepassword.ParseRef(s.Ref)
	if err != nil {
		return "", false, err
	}

	exists, err := onepassword.ItemExists(ctx, vault, item)
	if err != nil {
		return "", false, fmt.Errorf("unable to check whether the 1Password item %q exists in the %q vault: %w", item, vault, err)
	}

	if exists {
		return "", false, nil
	}

	question := fmt.Sprintf("Create the 1Password item %q in the %q vault?", item, vault)
	why := fmt.Sprintf("%s isn't in 1Password at %s.", s.Name, term.Escape(s.Ref))

	ok, err = t.Confirm(question, why)
	if err != nil || !ok {
		return "", false, err
	}

	secret, ok, err = t.ReadSecret("Paste your "+s.Name, s.Instructions)
	if err != nil || !ok {
		return "", false, err
	}

	if err := onepassword.CreateItem(ctx, vault, item, field, secret); err != nil {
		return "", false, fmt.Errorf("unable to create the 1Password item for %s: %w", s.Name, err)
	}

	return secret, true, nil
}

// read returns the value of s from 1Password. If it can't be read and
// s.Create is set, the user is offered to create it, when t is
// interactive.
func read(ctx context.Context, t *term.Terminal, s *Secret) (string, error) {
	value, err := onepassword.Read(ctx, s.Ref)
	if err == nil {
		return value, nil
	}

	readErr := fmt.Errorf("unable to determine %s, %s and store it in 1Password at %q: %w", s.Name, s.Help, s.Ref, err)

	if !s.Create || !t.Interactive {
		return "", readErr
	}

	value, ok, err := create(ctx, t, s)
	if err != nil {
		return "", errors.Join(readErr, err)
	}

	if !ok {
		return "", readErr
	}

	return value, nil
}

// addSecrets reads each of secrets from 1Password and passes it into
// the container. Each is shown as a step at t before it's read,
// so the user knows why 1Password may ask to be unlocked.
func addSecrets(ctx context.Context, t *term.Terminal, run *docker.Run, secrets ...Secret) error {
	for i := range secrets {
		s := &secrets[i]
		if s.Ref == "" {
			continue
		}

		step(t, "reading "+s.Env+" from 1Password")

		value, err := read(ctx, t, s)
		if err != nil {
			return err
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

// secretsFor returns the secrets agent needs for host, after those from
// extra_secret_env_vars. They're read after the banner is shown, so the
// user knows why 1Password may ask to be unlocked.
func secretsFor(host *Host, agent Agent) ([]Secret, error) {
	extra, err := extraSecrets(host.Config.ExtraSecretEnvVars)
	if err != nil {
		return nil, err
	}

	return append(extra, agent.Secrets(host)...), nil
}
