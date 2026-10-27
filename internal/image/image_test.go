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
	"errors"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/sigstore/sigstore-go/pkg/sign"
	"go.rgst.io/jaredallard/cmdexec/v2"
	"google.golang.org/protobuf/encoding/protojson"
	"gotest.tools/v3/assert"
)

// testTag is the tag images are pushed as.
const testTag = "v1.0.0"

// testOptions are the options for requests to the test registry. It's
// retried once at most, so an unreachable one fails quickly.
var testOptions = []remote.Option{remote.WithRetryBackoff(remote.Backoff{Steps: 1})}

// newRegistry starts a registry for the test, with opts, and returns
// the repository "asbx" in it.
func newRegistry(t *testing.T, opts ...registry.Option) string {
	t.Helper()

	opts = append([]registry.Option{registry.Logger(log.New(io.Discard, "", 0))}, opts...)
	srv := httptest.NewServer(registry.New(opts...))
	t.Cleanup(srv.Close)

	return strings.TrimPrefix(srv.URL, "http://") + "/asbx"
}

// unreachable returns a repository on a registry that refuses
// connections.
func unreachable(t *testing.T) string {
	t.Helper()

	srv := httptest.NewServer(nil)
	srv.Close()

	return strings.TrimPrefix(srv.URL, "http://") + "/asbx"
}

// push pushes a multi-platform image to repository as testTag and
// returns it, by digest.
func push(t *testing.T, repository string) name.Digest {
	t.Helper()

	idx, err := random.Index(256, 1, 2)
	assert.NilError(t, err)

	ref, err := name.NewTag(repository + ":" + testTag)
	assert.NilError(t, err)
	assert.NilError(t, remote.WriteIndex(ref, idx))

	digest, err := idx.Digest()
	assert.NilError(t, err)

	return ref.Context().Digest(digest.String())
}

// newKey returns a key to sign with and its PEM encoded public key.
func newKey(t *testing.T) (key *sign.EphemeralKeypair, pub []byte) {
	t.Helper()

	key, err := sign.NewEphemeralKeypair(nil)
	assert.NilError(t, err)

	pem, err := key.GetPublicKeyPem()
	assert.NilError(t, err)

	return key, []byte(pem)
}

// attach signs a statement about subject, a digest, with key and
// attaches it to d as cosign does.
func attach(t *testing.T, key *sign.EphemeralKeypair, d name.Digest, subject string) {
	t.Helper()

	algorithm, hex, _ := strings.Cut(subject, ":")
	statement := fmt.Sprintf(
		`{"_type":"https://in-toto.io/Statement/v1","subject":[{"digest":{%q:%q}}],`+
			`"predicateType":"https://sigstore.dev/cosign/sign/v1","predicate":{}}`,
		algorithm, hex,
	)

	b, err := sign.Bundle(&sign.DSSEData{Data: []byte(statement), PayloadType: "application/vnd.in-toto+json"}, key, sign.BundleOptions{})
	assert.NilError(t, err)

	data, err := protojson.Marshal(b)
	assert.NilError(t, err)

	desc, err := remote.Head(d)
	assert.NilError(t, err)

	img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: static.NewLayer(data, bundleArtifactType)})
	assert.NilError(t, err)

	img = mutate.MediaType(img, types.OCIManifestSchema1)
	img = mutate.ConfigMediaType(img, bundleArtifactType)
	img = mutate.Subject(img, *desc).(v1.Image)

	digest, err := img.Digest()
	assert.NilError(t, err)
	assert.NilError(t, remote.Write(d.Context().Digest(digest.String()), img))
}

// newResolver returns a Resolver for the test registry that verifies
// with key.
func newResolver(t *testing.T, key []byte) *Resolver {
	t.Helper()

	return &Resolver{Key: key, StateDir: t.TempDir(), Timeout: 10 * time.Second, RemoteOptions: testOptions}
}

// mockDocker has docker report whether it has the image ref, and pull
// it when it doesn't. Any other docker command panics, so pulling an
// image docker has fails the test.
func mockDocker(t *testing.T, ref string, present bool) {
	t.Helper()

	inspect := &cmdexec.MockCommand{Name: "docker", Args: []string{"image", "inspect", "--format", "{{.Id}}", ref}}
	if !present {
		inspect.Err = errors.New("exit status 1")
	}

	mock := cmdexec.NewMockExecutor(inspect)
	if !present {
		mock.AddCommand(&cmdexec.MockCommand{Name: "docker", Args: []string{"pull", ref}})
	}

	cmdexec.UseMockExecutor(t, mock)
}

// noWarnings fails the test if it's warned.
func noWarnings(t *testing.T) func(string, ...any) {
	return func(format string, args ...any) {
		t.Errorf("unexpected warning: "+format, args...)
	}
}

// TestResolve verifies that a signed image is resolved to its digest,
// pulled, with Pulling told, when docker doesn't have it, and recorded
// to fall back to.
func TestResolve(t *testing.T) {
	for _, present := range []bool{false, true} {
		t.Run(fmt.Sprintf("present=%v", present), func(t *testing.T) {
			repository := newRegistry(t)
			d := push(t, repository)
			key, pub := newKey(t)
			attach(t, key, d, d.DigestStr())

			mockDocker(t, d.String(), present)

			var pulled []string
			r := newResolver(t, pub)
			r.Pulling = func(ref string) { pulled = append(pulled, ref) }

			got, err := r.Resolve(t.Context(), repository, testTag, noWarnings(t))
			assert.NilError(t, err)
			assert.Equal(t, got, d.String())

			var want []string
			if !present {
				want = []string{d.String()}
			}
			assert.DeepEqual(t, pulled, want)

			last, err := r.load(testTag)
			assert.NilError(t, err)
			assert.Equal(t, last, d.String())
		})
	}
}

// TestVerifyErrors verifies that an image without a signature made with
// the key, for it, is an unverifiedError.
func TestVerifyErrors(t *testing.T) {
	tests := []struct {
		name string

		// sign attaches the image's signatures, if it has any.
		sign func(t *testing.T, d name.Digest, key *sign.EphemeralKeypair)
	}{
		{
			name: "unsigned",
			sign: func(*testing.T, name.Digest, *sign.EphemeralKeypair) {},
		},
		{
			name: "signed with another key",
			sign: func(t *testing.T, d name.Digest, _ *sign.EphemeralKeypair) {
				other, _ := newKey(t)
				attach(t, other, d, d.DigestStr())
			},
		},
		{
			name: "signature for another image",
			sign: func(t *testing.T, d name.Digest, key *sign.EphemeralKeypair) {
				attach(t, key, d, "sha256:"+strings.Repeat("0", 64))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := newRegistry(t)
			d := push(t, repository)
			key, pub := newKey(t)
			tt.sign(t, d, key)

			err := verify(pub, d, testOptions...)

			var unverified *unverifiedError
			assert.Assert(t, errors.As(err, &unverified), "got %v", err)
		})
	}
}

// TestVerifyAnySignature verifies that one valid signature is enough,
// whatever else is attached.
func TestVerifyAnySignature(t *testing.T) {
	repository := newRegistry(t)
	d := push(t, repository)
	key, pub := newKey(t)
	other, _ := newKey(t)
	attach(t, other, d, d.DigestStr())
	attach(t, key, d, d.DigestStr())

	assert.NilError(t, verify(pub, d, testOptions...))
}

// TestVerifyReferrersTagFallback verifies that signatures are found on
// registries without the referrers API, such as GHCR, where they're
// listed under a tag named for the image's digest instead.
func TestVerifyReferrersTagFallback(t *testing.T) {
	repository := newRegistry(t, registry.WithReferrersSupport(false))
	d := push(t, repository)
	key, pub := newKey(t)
	attach(t, key, d, d.DigestStr())

	assert.NilError(t, verify(pub, d, testOptions...))
}

// TestVerifyWithoutKey verifies that a build without a key verifies
// nothing.
func TestVerifyWithoutKey(t *testing.T) {
	repository := newRegistry(t)
	d := push(t, repository)

	assert.ErrorContains(t, verify(nil, d, testOptions...), "without a public key")
}

// TestResolveFallback verifies that the last verified image is run,
// with a warning saying why, when the latest can't be.
func TestResolveFallback(t *testing.T) {
	tests := []struct {
		name string

		// unsigned pushes an unsigned image, rather than having the
		// registry be unreachable.
		unsigned bool

		// warning is what the warning starts with.
		warning string
	}{
		{name: "unreachable", warning: "couldn't get the latest image"},
		{name: "unsigned", unsigned: true, warning: "the latest image failed signature verification"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := unreachable(t)
			if tt.unsigned {
				repository = newRegistry(t)
				push(t, repository)
			}

			last := repository + "@sha256:" + strings.Repeat("a", 64)
			mockDocker(t, last, true)

			_, pub := newKey(t)
			r := newResolver(t, pub)
			assert.NilError(t, r.save(testTag, last))

			var warnings []string
			got, err := r.Resolve(t.Context(), repository, testTag, func(format string, args ...any) {
				warnings = append(warnings, fmt.Sprintf(format, args...))
			})
			assert.NilError(t, err)
			assert.Equal(t, got, last)
			assert.Equal(t, len(warnings), 1)
			assert.Assert(t, strings.HasPrefix(warnings[0], tt.warning), "got %q", warnings[0])
		})
	}
}

// TestResolveNoFallback verifies that it's an error when the latest
// image can't be run and there's no verified one to fall back to.
func TestResolveNoFallback(t *testing.T) {
	tests := []struct {
		name string

		// last is the image recorded as the last verified, if any.
		last string

		// present is whether docker has last.
		present bool
	}{
		{name: "nothing recorded"},
		{name: "image removed", last: "@sha256:" + strings.Repeat("a", 64)},
		{name: "another repository", last: "example.com/other@sha256:" + strings.Repeat("a", 64), present: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := unreachable(t)
			_, pub := newKey(t)
			r := newResolver(t, pub)

			if tt.last != "" {
				last := tt.last
				if strings.HasPrefix(last, "@") {
					last = repository + last
				}

				assert.NilError(t, r.save(testTag, last))
				mockDocker(t, last, tt.present)
			}

			_, err := r.Resolve(t.Context(), repository, testTag, noWarnings(t))
			assert.ErrorContains(t, err, "failed to get a verified image")
		})
	}
}
