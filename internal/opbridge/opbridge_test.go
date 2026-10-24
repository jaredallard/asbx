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

package opbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/onepassword"
	"gotest.tools/v3/assert"
)

// fakeOP is an [OP] that has the secrets and items in it, recording
// what it was asked for.
type fakeOP struct {
	secrets map[string]string
	items   []onepassword.Item

	// fields are the refs of each item's fields, by item ID.
	fields map[string][]string

	// listErr and fieldsErr, when set, are returned by ListItems and
	// FieldRefs.
	listErr   error
	fieldsErr error

	// refs are the refs Read was asked for.
	refs []string

	// got are the items FieldRefs was asked for, in each call.
	got [][]onepassword.Item
}

// Read implements [OP].
func (f *fakeOP) Read(_ context.Context, ref string) (string, error) {
	f.refs = append(f.refs, ref)

	value, ok := f.secrets[ref]
	if !ok {
		return "", errors.New("item not found")
	}

	return value, nil
}

// ListItems implements [OP].
func (f *fakeOP) ListItems(context.Context) ([]onepassword.Item, error) {
	return f.items, f.listErr
}

// FieldRefs implements [OP].
func (f *fakeOP) FieldRefs(_ context.Context, items []onepassword.Item) (map[string][]string, error) {
	f.got = append(f.got, items)
	if f.fieldsErr != nil {
		return nil, f.fieldsErr
	}

	refs := make(map[string][]string)
	for i := range items {
		refs[items[i].ID] = f.fields[items[i].ID]
	}

	return refs, nil
}

// TestServe verifies that each request is answered in order, that op
// is only asked for allowed refs, that only items with an allowed field
// are listed, and that op's errors are passed on.
func TestServe(t *testing.T) {
	allow, err := ParseAllowlist([]string{"op://Dev/*/token"})
	assert.NilError(t, err)

	github := onepassword.Item{ID: "gh", Title: "github", Vault: onepassword.ItemVault{ID: "dev", Name: "Dev"}}
	bank := onepassword.Item{ID: "bank", Title: "bank", Vault: onepassword.ItemVault{ID: "private", Name: "Private"}}
	op := &fakeOP{
		secrets: map[string]string{"op://Dev/github/token": "hunter2"},
		items:   []onepassword.Item{github, bank},
		fields: map[string][]string{
			"gh":   {"op://Dev/github/token"},
			"bank": {"op://Private/bank/password"},
		},
	}

	var requests strings.Builder
	for _, req := range []Request{
		{Ref: "op://Dev/github/token"},
		{Ref: "op://Private/bank/password"},
		{Ref: "op://Dev/missing/token"},
		{Command: CommandItemList},
		{Command: CommandItemList, Vault: "Private"},
		{Command: "item delete", Ref: "op://Dev/github/token"},
	} {
		b, err := json.Marshal(req)
		assert.NilError(t, err)

		requests.Write(append(b, '\n'))
	}

	var out bytes.Buffer
	assert.NilError(t, Serve(t.Context(), strings.NewReader(requests.String()), &out, allow, op))

	var got []Response
	dec := json.NewDecoder(&out)
	for {
		var resp Response
		err := dec.Decode(&resp)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Serve() wrote invalid JSON: %v", err)
		}

		got = append(got, resp)
	}

	assert.DeepEqual(t, got, []Response{
		{Value: "hunter2"},
		{Error: `"op://Private/bank/password" isn't in op_allowed_secret_refs`},
		{Error: "item not found"},
		{Items: []onepassword.Item{github}},
		{},
		{Error: "op item delete isn't supported in asbx"},
	})
	assert.DeepEqual(t, op.refs, []string{"op://Dev/github/token", "op://Dev/missing/token"})
}

// TestServeInvalidRequest verifies that a request that isn't JSON stops
// the host from serving the container.
func TestServeInvalidRequest(t *testing.T) {
	op := &fakeOP{}

	err := Serve(t.Context(), strings.NewReader("not json\n"), io.Discard, &Allowlist{}, op)
	assert.ErrorContains(t, err, "invalid request from the container")
}

// TestServeRequestTooLong verifies that the container can't make the
// host buffer a request without limit.
func TestServeRequestTooLong(t *testing.T) {
	op := &fakeOP{}
	long := strings.Repeat("a", maxRequest+1)

	err := Serve(t.Context(), strings.NewReader(long), io.Discard, &Allowlist{}, op)
	assert.Assert(t, err != nil, "Serve() = nil, want error for a %d byte request", len(long))
}

// TestBridge verifies that op in the container reads secrets and lists
// items from the host through the bridge, and gets the host's errors.
func TestBridge(t *testing.T) {
	allow, err := ParseAllowlist([]string{"op://Dev/github/token"})
	assert.NilError(t, err)

	github := onepassword.Item{ID: "gh", Title: "github", Vault: onepassword.ItemVault{ID: "dev", Name: "Dev"}}
	op := &fakeOP{
		secrets: map[string]string{"op://Dev/github/token": "hunter2"},
		items:   []onepassword.Item{github},
		fields:  map[string][]string{"gh": {"op://Dev/github/token"}},
	}
	socket := filepath.Join(t.TempDir(), "op.sock")

	// toHost and toContainer are the bridge's stdout and stdin.
	hostR, toHost := io.Pipe()
	containerR, toContainer := io.Pipe()

	served := make(chan error, 1)
	go func() {
		served <- Serve(t.Context(), hostR, toContainer, allow, op)
	}()

	bridged := make(chan error, 1)
	go func() {
		bridged <- Bridge(t.Context(), containerR, toHost, socket)
	}()

	got, err := Read(t.Context(), socket, "op://Dev/github/token")
	assert.NilError(t, err)
	assert.Equal(t, got, "hunter2")

	_, err = Read(t.Context(), socket, "op://Dev/github/other")
	assert.ErrorContains(t, err, "isn't in op_allowed_secret_refs")

	items, err := ListItems(t.Context(), socket, "")
	assert.NilError(t, err)
	assert.DeepEqual(t, items, []onepassword.Item{github})

	items, err = ListItems(t.Context(), socket, "Other")
	assert.NilError(t, err)
	assert.Equal(t, len(items), 0, "ListItems(Other) = %v, want none", items)

	op.listErr = errors.New("not signed in")
	_, err = ListItems(t.Context(), socket, "")
	assert.ErrorContains(t, err, "not signed in")

	// The host going away stops the bridge.
	toContainer.Close()
	assert.NilError(t, <-bridged)

	toHost.Close()
	assert.NilError(t, <-served)
}

// TestReadUnavailable verifies that op in the container fails straight
// away when the bridge isn't enabled.
func TestReadUnavailable(t *testing.T) {
	_, err := Read(t.Context(), "", "op://a/b/c")
	assert.ErrorIs(t, err, ErrUnavailable)
}

// TestListItemsUnavailable verifies that op item list in the container
// fails straight away when the bridge isn't enabled.
func TestListItemsUnavailable(t *testing.T) {
	_, err := ListItems(t.Context(), "", "")
	assert.ErrorIs(t, err, ErrUnavailable)
}
