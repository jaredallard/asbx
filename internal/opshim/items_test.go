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

package opshim

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"git.rgst.io/jaredallard/asbx/internal/onepassword"
	"gotest.tools/v3/assert"
)

// listOf returns a [ListItemsFunc] that lists items, whatever vault
// it's asked for.
func listOf(items ...onepassword.Item) ListItemsFunc {
	return func(context.Context, string) ([]onepassword.Item, error) {
		return items, nil
	}
}

// TestListItemsTable verifies that items are written as a table, as op
// item list writes them, with when they were edited relative to now.
func TestListItemsTable(t *testing.T) {
	edited := time.Now().Add(-2 * time.Hour)
	list := listOf(
		onepassword.Item{ID: "abc123", Title: "github", Vault: onepassword.ItemVault{ID: "d", Name: "Dev"}, UpdatedAt: edited},
		onepassword.Item{ID: "def456", Title: "aws", Vault: onepassword.ItemVault{ID: "p", Name: "Private"}, UpdatedAt: edited},
	)

	var out strings.Builder
	assert.NilError(t, ListItems(t.Context(), list, &out, ListOptions{}))
	assert.Equal(t, out.String(), ""+
		"ID        TITLE     VAULT      EDITED\n"+
		"abc123    github    Dev        2 hours ago\n"+
		"def456    aws       Private    2 hours ago\n")
}

// TestListItemsJSON verifies that items are written as a JSON array, as
// op item list --format json writes them, including when there are
// none.
func TestListItemsJSON(t *testing.T) {
	github := onepassword.Item{
		ID:        "abc123",
		Title:     "github",
		Vault:     onepassword.ItemVault{ID: "d", Name: "Dev"},
		UpdatedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	}
	aws := onepassword.Item{ID: "def456", Title: "aws", Vault: onepassword.ItemVault{ID: "p", Name: "Private"}}

	tests := []struct {
		name  string
		items []onepassword.Item
		want  []onepassword.Item
	}{
		{name: "Items", items: []onepassword.Item{github, aws}, want: []onepassword.Item{github, aws}},
		{name: "NoItems", want: []onepassword.Item{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			assert.NilError(t, ListItems(t.Context(), listOf(tt.items...), &out, ListOptions{Format: "json"}))

			var got []onepassword.Item
			assert.NilError(t, json.Unmarshal([]byte(out.String()), &got), "ListItems() wrote invalid JSON: %s", out.String())
			assert.DeepEqual(t, got, tt.want)
		})
	}
}

// TestListItemsVault verifies that the vault asked for is what the
// items are listed from, since the host does the filtering.
func TestListItemsVault(t *testing.T) {
	var got string
	list := func(_ context.Context, vault string) ([]onepassword.Item, error) {
		got = vault
		return nil, nil
	}

	var out strings.Builder
	assert.NilError(t, ListItems(t.Context(), list, &out, ListOptions{Vault: "Dev"}))
	assert.Equal(t, got, "Dev")
}

// TestListItemsErrors verifies that an unknown format, or the items
// not being listed, is an error.
func TestListItemsErrors(t *testing.T) {
	tests := []struct {
		name string
		list ListItemsFunc
		opts ListOptions
		want string
	}{
		{
			name: "UnknownFormat",
			list: listOf(),
			opts: ListOptions{Format: "yaml"},
			want: `invalid --format "yaml"`,
		},
		{
			name: "ListFails",
			list: func(context.Context, string) ([]onepassword.Item, error) {
				return nil, errors.New("not signed in")
			},
			want: "not signed in",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			err := ListItems(t.Context(), tt.list, &out, tt.opts)
			assert.ErrorContains(t, err, tt.want)
			assert.Equal(t, out.String(), "", "ListItems() wrote output despite failing")
		})
	}
}
