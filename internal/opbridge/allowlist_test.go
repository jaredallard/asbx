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
	"testing"

	"gotest.tools/v3/assert"
)

// TestAllowlistAllows verifies which refs an allowlist allows, in
// particular that * stays within a path segment, ** spans them, and
// that nothing else is a wildcard.
func TestAllowlistAllows(t *testing.T) {
	allow, err := ParseAllowlist([]string{
		"op://Dev/github/token",
		"op://Dev/aws/*",
		"op://Dev/*/password",
		"op://Dev/totp/code?attribute=otp",
		"op://Shared/**",
		"op://Infra/{a,b}/[k]ey",
		"",
	})
	assert.NilError(t, err)

	tests := []struct {
		ref  string
		want bool
	}{
		{ref: "op://Dev/github/token", want: true},
		{ref: "op://Dev/github/other", want: false},
		{ref: "op://dev/github/token", want: false},
		{ref: "op://Dev/aws/access-key", want: true},
		{ref: "op://Dev/aws/section/access-key", want: false},
		{ref: "op://Dev/any/password", want: true},
		{ref: "op://Dev/../password", want: false},
		{ref: "op://Dev/./password", want: false},
		{ref: "op://Dev/aws//key", want: false},
		{ref: "op://Dev/github/token?attribute=otp", want: false},
		{ref: "op://Dev/totp/code?attribute=otp", want: true},
		{ref: "op://Dev/totp/codeXattribute=otp", want: false},
		{ref: "op://Shared/item/field", want: true},
		{ref: "op://Shared/item/section/field", want: true},
		{ref: "op://Shared/../Private/bank/password", want: false},
		{ref: "op://Infra/{a,b}/[k]ey", want: true},
		{ref: "op://Infra/a/key", want: false},
		{ref: "Dev/github/token", want: false},
		{ref: "", want: false},
	}

	for _, tt := range tests {
		got := allow.Allows(tt.ref)
		assert.Check(t, got == tt.want, "Allows(%q) = %v, want %v", tt.ref, got, tt.want)
	}
}

// TestAllowlistEmpty verifies that an allowlist without entries allows
// nothing.
func TestAllowlistEmpty(t *testing.T) {
	allow, err := ParseAllowlist(nil)
	assert.NilError(t, err)
	assert.Assert(t, allow.Empty())
	assert.Assert(t, !allow.Allows("op://a/b/c"))
}

// TestParseAllowlistErrors verifies that entries that aren't op:// refs
// are rejected.
func TestParseAllowlistErrors(t *testing.T) {
	for _, entry := range []string{"Dev/github/token", "https://example.com", "*"} {
		_, err := ParseAllowlist([]string{entry})
		assert.ErrorContains(t, err, "must be an op:// ref", "ParseAllowlist(%q)", entry)
	}
}

// TestAllowlistAllowsField verifies that an entry for an attribute of a
// field, such as ?attribute=otp, allows the field itself to be listed,
// and that otherwise fields match as refs do.
func TestAllowlistAllowsField(t *testing.T) {
	allow, err := ParseAllowlist([]string{
		"op://Dev/*/token",
		"op://Dev/totp/code?attribute=otp",
	})
	assert.NilError(t, err)

	tests := []struct {
		ref  string
		want bool
	}{
		{ref: "op://Dev/github/token", want: true},
		{ref: "op://Dev/github/password", want: false},
		{ref: "op://Dev/totp/code", want: true},
		{ref: "op://Dev/totp/other", want: false},
		{ref: "op://Dev/../token", want: false},
	}

	for _, tt := range tests {
		got := allow.allowsField(tt.ref)
		assert.Check(t, got == tt.want, "allowsField(%q) = %v, want %v", tt.ref, got, tt.want)
	}
}

// TestAllowlistMayAllowItem verifies that an item is a candidate when
// an entry's vault and item segments match it, so that no item an
// entry allows a field of is missed.
func TestAllowlistMayAllowItem(t *testing.T) {
	tests := []struct {
		entry string
		vault string
		item  string
		want  bool
	}{
		{entry: "op://Dev/github/token", vault: "Dev", item: "github", want: true},
		{entry: "op://Dev/github/token", vault: "Dev", item: "gitlab", want: false},
		{entry: "op://Dev/github/token", vault: "Private", item: "github", want: false},
		{entry: "op://Dev/*/token", vault: "Dev", item: "anything", want: true},
		{entry: "op://Dev/git*/token", vault: "Dev", item: "gitea", want: true},
		{entry: "op://Dev/**", vault: "Dev", item: "anything", want: true},
		{entry: "op://**/token", vault: "Private", item: "bank", want: true},
		{entry: "op://Dev/totp/code?attribute=otp", vault: "Dev", item: "totp", want: true},
		{entry: "op://Dev/{a,b}/key", vault: "Dev", item: "a", want: false},
		{entry: "op://Dev/{a,b}/key", vault: "Dev", item: "{a,b}", want: true},
		{entry: "op://Dev", vault: "Dev", item: "github", want: false},
	}

	for _, tt := range tests {
		allow, err := ParseAllowlist([]string{tt.entry})
		assert.NilError(t, err)

		got := allow.mayAllowItem(tt.vault, tt.item)
		assert.Check(t, got == tt.want, "ParseAllowlist(%q).mayAllowItem(%q, %q) = %v, want %v", tt.entry, tt.vault, tt.item, got, tt.want)
	}
}
