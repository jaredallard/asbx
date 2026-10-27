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

package image

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	sgverify "github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"
)

// bundleArtifactType is the artifact type of the Sigstore bundles
// cosign attaches to images, through the referrers API, as their
// signatures.
const bundleArtifactType = "application/vnd.dev.sigstore.bundle.v0.3+json"

// maxBundleSize is the largest bundle that's read, so a registry can't
// have one read without end.
const maxBundleSize = 1 << 20

// unverifiedError is returned when an image has no signature made with
// the key, as opposed to the registry not being reachable to find out.
type unverifiedError struct {
	err error
}

// Error implements error.
func (e *unverifiedError) Error() string {
	return "no valid signature: " + e.err.Error()
}

// Unwrap returns why no signature was valid.
func (e *unverifiedError) Unwrap() error {
	return e.err
}

// newVerifier returns the verifier of bundles signed with key, a PEM
// encoded public key. They're signed without a transparency log or
// timestamps, so the key is all there is to check them against.
func newVerifier(key []byte) (*sgverify.Verifier, error) {
	if len(bytes.TrimSpace(key)) == 0 {
		return nil, errors.New("asbx was built without a public key to verify images with")
	}

	pub, err := cryptoutils.UnmarshalPEMToPublicKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to parse the public key images are verified with: %w", err)
	}

	sv, err := signature.LoadDefaultVerifier(pub)
	if err != nil {
		return nil, fmt.Errorf("failed to load the public key images are verified with: %w", err)
	}

	// The key never expires, it's replaced with a release of asbx.
	material := root.NewTrustedPublicKeyMaterial(func(string) (root.TimeConstrainedVerifier, error) {
		return root.NewExpiringKey(sv, time.Time{}, time.Time{}), nil
	})

	return sgverify.NewVerifier(material, sgverify.WithNoObserverTimestamps())
}

// verify returns nil if the image at d has a signature made with key, a
// PEM encoded public key. Any one is enough, there's one per time it
// was signed. When none is valid, the error is an [unverifiedError].
func verify(key []byte, d name.Digest, opts ...remote.Option) error {
	verifier, err := newVerifier(key)
	if err != nil {
		return err
	}

	algorithm, hexDigest, _ := strings.Cut(d.DigestStr(), ":")
	digest, err := hex.DecodeString(hexDigest)
	if err != nil {
		return fmt.Errorf("failed to decode digest %s: %w", d.DigestStr(), err)
	}

	// The bundle's statement must be about this image, so a signature
	// for another image can't be attached to it.
	policy := sgverify.NewPolicy(sgverify.WithArtifactDigest(algorithm, digest), sgverify.WithKey())

	idx, err := remote.Referrers(d, slices.Concat(opts, []remote.Option{remote.WithFilter("artifactType", bundleArtifactType)})...)
	if err != nil {
		return fmt.Errorf("failed to list the signatures of %s: %w", d, err)
	}

	manifest, err := idx.IndexManifest()
	if err != nil {
		return fmt.Errorf("failed to list the signatures of %s: %w", d, err)
	}

	var errs []error
	for i := range manifest.Manifests {
		desc := &manifest.Manifests[i]
		if desc.ArtifactType != bundleArtifactType {
			continue
		}

		b, err := fetchBundle(d.Context().Digest(desc.Digest.String()), opts...)
		if err == nil {
			_, err = verifier.Verify(b, policy)
		}

		if err == nil {
			return nil
		}

		errs = append(errs, fmt.Errorf("signature %s: %w", desc.Digest, err))
	}

	if len(errs) == 0 {
		return &unverifiedError{errors.New("the image isn't signed")}
	}

	return &unverifiedError{errors.Join(errs...)}
}

// fetchBundle returns the bundle in the artifact at d, which cosign
// pushes with the bundle as its only layer.
func fetchBundle(d name.Digest, opts ...remote.Option) (*bundle.Bundle, error) {
	img, err := remote.Image(d, opts...)
	if err != nil {
		return nil, err
	}

	layers, err := img.Layers()
	if err != nil {
		return nil, err
	}

	if len(layers) != 1 {
		return nil, fmt.Errorf("has %d layers, not the bundle alone", len(layers))
	}

	size, err := layers[0].Size()
	if err != nil {
		return nil, err
	}

	if size > maxBundleSize {
		return nil, fmt.Errorf("bundle is %d bytes, more than the %d allowed", size, maxBundleSize)
	}

	rc, err := layers[0].Compressed()
	if err != nil {
		return nil, err
	}
	defer rc.Close() //nolint:errcheck // Why: only read from.

	data, err := io.ReadAll(io.LimitReader(rc, maxBundleSize))
	if err != nil {
		return nil, err
	}

	var b bundle.Bundle
	if err := b.UnmarshalJSON(data); err != nil {
		return nil, fmt.Errorf("failed to decode bundle: %w", err)
	}

	return &b, nil
}
