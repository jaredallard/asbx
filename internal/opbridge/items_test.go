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
	"errors"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/onepassword"
	"gotest.tools/v3/assert"
)

// testItems returns an op with items in a few vaults, whose fields are
// at the refs op gives them.
func testItems() *fakeOP {
	dev := onepassword.ItemVault{ID: "dev-id", Name: "Dev"}
	return &fakeOP{
		items: []onepassword.Item{
			{ID: "github-id", Title: "github", Vault: dev},
			{ID: "aws-id", Title: "aws", Vault: dev},
			{ID: "totp-id", Title: "totp", Vault: dev},
			{ID: "bank-id", Title: "bank", Vault: onepassword.ItemVault{ID: "private-id", Name: "Private"}},
			{ID: "db-id", Title: "db", Vault: onepassword.ItemVault{ID: "agent-id", Name: "Agent"}},
			{ID: "slash-id", Title: "db/conn", Vault: onepassword.ItemVault{ID: "agent-id", Name: "Agent"}},
			{ID: "query-id", Title: "a?b", Vault: dev},
		},
		fields: map[string][]string{
			"github-id": {"op://Dev/github/token", "op://Dev/github/password"},
			"aws-id":    {"op://Dev/aws/access-key"},
			"totp-id":   {"op://Dev/totp/one-time password?attribute=otp"},
			"bank-id":   {"op://Private/bank/password"},
			"db-id":     {"op://Agent/db/conn/url"},
			"slash-id":  {"op://Agent/slash-id/url"},
			"query-id":  {"op://Dev/query-id/password"},
		},
	}
}

// TestAllowedItems verifies that only items with a field the allowlist
// allows are listed, however the entries refer to them, and only from
// the vault asked for, if any.
func TestAllowedItems(t *testing.T) {
	tests := []struct {
		name    string
		entries []string
		vault   string
		want    []string
	}{
		{name: "Field", entries: []string{"op://Dev/github/token"}, want: []string{"github-id"}},
		{name: "AnyItemWithField", entries: []string{"op://Dev/*/token"}, want: []string{"github-id"}},
		{name: "AnyFieldOfItem", entries: []string{"op://Dev/aws/*"}, want: []string{"aws-id"}},
		{name: "ByID", entries: []string{"op://private-id/bank-id/password"}, want: []string{"bank-id"}},
		{name: "Attribute", entries: []string{"op://Dev/totp/one-time password?attribute=otp"}, want: []string{"totp-id"}},
		{name: "SectionNotInStar", entries: []string{"op://Agent/db/*"}},
		{name: "SectionInDoubleStar", entries: []string{"op://Agent/db/**"}, want: []string{"db-id"}},
		{name: "SectionNotTitleWithSlash", entries: []string{"op://Agent/db/conn/url"}, want: []string{"db-id"}},
		{name: "QueryInTitleNotCut", entries: []string{"op://Dev/*", "op://Dev/*/token"}, want: []string{"github-id"}},
		{name: "QueryInTitleByID", entries: []string{"op://Dev/query-id/password"}, want: []string{"query-id"}},
		{
			name:    "Everything",
			entries: []string{"op://**"},
			want:    []string{"github-id", "aws-id", "totp-id", "bank-id", "db-id", "slash-id", "query-id"},
		},
		{name: "Nothing", entries: []string{"op://Other/**"}},
		{name: "VaultByName", entries: []string{"op://**"}, vault: "Private", want: []string{"bank-id"}},
		{name: "VaultByID", entries: []string{"op://**"}, vault: "agent-id", want: []string{"db-id", "slash-id"}},
		{name: "OtherVault", entries: []string{"op://**"}, vault: "Other"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allow, err := ParseAllowlist(tt.entries)
			assert.NilError(t, err)

			items, err := allowedItems(t.Context(), testItems(), allow, tt.vault)
			assert.NilError(t, err)

			var got []string
			for _, item := range items {
				got = append(got, item.ID)
			}

			assert.DeepEqual(t, got, tt.want)
		})
	}
}

// TestAllowedItemsCandidates verifies that op is only asked for the
// fields of items an entry could allow, in the vault asked for, and not
// at all when there are none, so it doesn't fetch every item in the
// account.
func TestAllowedItemsCandidates(t *testing.T) {
	allow, err := ParseAllowlist([]string{"op://Dev/github/*"})
	assert.NilError(t, err)

	op := testItems()
	_, err = allowedItems(t.Context(), op, allow, "")
	assert.NilError(t, err)
	assert.DeepEqual(t, op.got, [][]onepassword.Item{{op.items[0]}})

	allow, err = ParseAllowlist([]string{"op://**"})
	assert.NilError(t, err)

	op = testItems()
	_, err = allowedItems(t.Context(), op, allow, "Private")
	assert.NilError(t, err)
	assert.DeepEqual(t, op.got, [][]onepassword.Item{{op.items[3]}})

	allow, err = ParseAllowlist([]string{"op://Other/**"})
	assert.NilError(t, err)

	op = testItems()
	_, err = allowedItems(t.Context(), op, allow, "")
	assert.NilError(t, err)
	assert.Equal(t, len(op.got), 0, "FieldRefs() calls = %v, want none", op.got)
}

// TestAllowedItemsErrors verifies that op failing to list items, or to
// get their fields, is an error rather than no items.
func TestAllowedItemsErrors(t *testing.T) {
	allow, err := ParseAllowlist([]string{"op://**"})
	assert.NilError(t, err)

	tests := []struct {
		name string
		op   *fakeOP
	}{
		{name: "ListFails", op: &fakeOP{listErr: errors.New("not signed in")}},
		{name: "FieldsFail", op: &fakeOP{items: testItems().items, fieldsErr: errors.New("not signed in")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := allowedItems(t.Context(), tt.op, allow, "")
			assert.ErrorContains(t, err, "not signed in", "allowedItems() = %v", got)
		})
	}
}
