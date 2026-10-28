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
	"context"
	"errors"
	"net"
	"net/rpc"
	"path/filepath"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/onepassword"
	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
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

// githubItem returns an item in the Dev vault.
func githubItem() onepassword.Item {
	return onepassword.Item{ID: "gh", Title: "github", Vault: onepassword.ItemVault{ID: "dev", Name: "Dev"}}
}

// TestServiceRead verifies that op is only asked for allowed refs, and
// that op's errors are passed on.
func TestServiceRead(t *testing.T) {
	allow, err := ParseAllowlist([]string{"op://Dev/*/token"})
	assert.NilError(t, err)

	op := &fakeOP{secrets: map[string]string{"op://Dev/github/token": "hunter2"}}
	svc := NewService(t.Context(), allow, op)

	var got string
	assert.NilError(t, svc.Read(&ReadArgs{Ref: "op://Dev/github/token"}, &got))
	assert.Equal(t, got, "hunter2")

	err = svc.Read(&ReadArgs{Ref: "op://Private/bank/password"}, &got)
	assert.Error(t, err, `"op://Private/bank/password" isn't in op_allowed_secret_refs`)

	err = svc.Read(&ReadArgs{Ref: "op://Dev/missing/token"}, &got)
	assert.Error(t, err, "item not found")

	assert.DeepEqual(t, op.refs, []string{"op://Dev/github/token", "op://Dev/missing/token"})
}

// TestServiceListItems verifies that only items with an allowed field
// are listed, from the vault asked for, and that op's errors are passed
// on.
func TestServiceListItems(t *testing.T) {
	allow, err := ParseAllowlist([]string{"op://Dev/*/token"})
	assert.NilError(t, err)

	github := githubItem()
	bank := onepassword.Item{ID: "bank", Title: "bank", Vault: onepassword.ItemVault{ID: "private", Name: "Private"}}
	op := &fakeOP{
		items: []onepassword.Item{github, bank},
		fields: map[string][]string{
			"gh":   {"op://Dev/github/token"},
			"bank": {"op://Private/bank/password"},
		},
	}
	svc := NewService(t.Context(), allow, op)

	var got []onepassword.Item
	assert.NilError(t, svc.ListItems(&ListArgs{}, &got))
	assert.DeepEqual(t, got, []onepassword.Item{github})

	got = nil
	assert.NilError(t, svc.ListItems(&ListArgs{Vault: "Private"}, &got))
	assert.Equal(t, len(got), 0, "ListItems(Private) = %v, want none", got)

	op.listErr = errors.New("not signed in")
	assert.Error(t, svc.ListItems(&ListArgs{}, &got), "not signed in")
}

// TestBridge verifies that op in the container reads secrets and lists
// items from the host through the bridge, and gets the host's errors.
func TestBridge(t *testing.T) {
	allow, err := ParseAllowlist([]string{"op://Dev/github/token"})
	assert.NilError(t, err)

	github := githubItem()
	op := &fakeOP{
		secrets: map[string]string{"op://Dev/github/token": "hunter2"},
		items:   []onepassword.Item{github},
		fields:  map[string][]string{"gh": {"op://Dev/github/token"}},
	}
	socket := filepath.Join(t.TempDir(), "op.sock")

	// The bridge's stdio, as the host and the bridge see it.
	hostConn, bridgeConn := net.Pipe()

	served := make(chan error, 1)
	go func() {
		served <- stdiobridge.Serve(t.Context(), hostConn, maxRequest, func(srv *rpc.Server) error {
			return srv.RegisterName(serviceName, NewService(t.Context(), allow, op))
		})
	}()

	bridged := make(chan error, 1)
	go func() {
		bridged <- stdiobridge.Bridge(t.Context(), bridgeConn, socket)
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
	hostConn.Close()
	assert.NilError(t, <-bridged)
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
