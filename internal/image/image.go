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

// Package image resolves the image the sandbox runs to one whose
// signature has been verified. Released images are rebuilt daily under
// the same tag, so the tag is checked on every run, and the image is
// run by digest so docker can't run anything but what was verified.
package image

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"go.rgst.io/jaredallard/cmdexec/v2"
)

// publicKey is the PEM encoded public key the images are signed with,
// by cosign, in CI.
//
//go:embed embed/cosign.pub
var publicKey []byte

// checkTimeout is how long checking the registry for a tag's image may
// take, so an unreachable registry doesn't hold up starting for long.
// Pulling the image isn't limited.
const checkTimeout = 15 * time.Second

// Resolver resolves tags to images verified to be signed with Key.
type Resolver struct {
	// Key is the PEM encoded public key images must be signed with.
	Key []byte

	// StateDir is where the last image verified for each tag is kept.
	StateDir string

	// Timeout is how long checking the registry may take.
	Timeout time.Duration

	// RemoteOptions are passed to every request to the registry.
	RemoteOptions []remote.Option
}

// New returns a Resolver for the images asbx runs, keeping its state in
// the user's cache directory.
func New() (*Resolver, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}

	return &Resolver{
		Key:      publicKey,
		StateDir: filepath.Join(cacheDir, "asbx"),
		Timeout:  checkTimeout,
	}, nil
}

// Resolve returns the image to run for repository's tag, by digest.
// That's the image the registry has for the tag, once its signature is
// verified and docker has pulled it. When any of that fails, it's the
// last image that was, if docker still has it, and warn is told why.
func (r *Resolver) Resolve(ctx context.Context, repository, tag string, warn func(format string, args ...any)) (string, error) {
	ref, err := r.latest(ctx, repository, tag)
	if err == nil {
		if err := r.save(tag, ref); err != nil {
			warn("couldn't record the verified image to fall back to: %v", err)
		}

		return ref, nil
	}

	last, lerr := r.load(tag)
	if lerr != nil || !strings.HasPrefix(last, repository+"@") || !has(ctx, last) {
		return "", fmt.Errorf("failed to get a verified image: %w", err)
	}

	var unverified *unverifiedError
	if errors.As(err, &unverified) {
		warn("the latest image failed signature verification, running the last verified one: %v", err)
	} else {
		warn("couldn't get the latest image, running the last verified one: %v", err)
	}

	return last, nil
}

// latest returns the image the registry has for repository's tag, by
// digest, once its signature is verified and docker has it.
func (r *Resolver) latest(ctx context.Context, repository, tag string) (string, error) {
	t, err := name.NewTag(repository + ":" + tag)
	if err != nil {
		return "", err
	}

	checkCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	opts := slices.Concat(r.RemoteOptions, []remote.Option{remote.WithContext(checkCtx)})

	desc, err := remote.Head(t, opts...)
	if err != nil {
		return "", fmt.Errorf("failed to look up %s: %w", t, err)
	}

	ref := repository + "@" + desc.Digest.String()
	d, err := name.NewDigest(ref)
	if err != nil {
		return "", err
	}

	if err := verify(r.Key, d, opts...); err != nil {
		return "", err
	}

	if !has(ctx, ref) {
		if err := pull(ctx, ref); err != nil {
			return "", fmt.Errorf("failed to pull %s: %w", ref, err)
		}
	}

	return ref, nil
}

// has returns whether docker has the image ref.
func has(ctx context.Context, ref string) bool {
	return cmdexec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", ref).Run() == nil
}

// pull has docker pull the image ref, showing its progress.
func pull(ctx context.Context, ref string) error {
	c := cmdexec.CommandContext(ctx, "docker", "pull", ref)
	c.UseOSStreams(false)

	return c.Run()
}
