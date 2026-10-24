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
	"slices"
	"strings"

	"git.rgst.io/jaredallard/asbx/internal/onepassword"
)

// itemNames returns the names item and its vault can be referred to by
// in a ref: their names and their IDs. Names with a / or ? are left
// out, since they'd be taken as more than one segment of a ref, or as
// the start of its query string, and op uses the ID instead.
func itemNames(item *onepassword.Item) (vaults, items []string) {
	unusable := func(name string) bool { return strings.ContainsAny(name, "/?") }
	vaults = slices.DeleteFunc([]string{item.Vault.Name, item.Vault.ID}, unusable)
	items = slices.DeleteFunc([]string{item.Title, item.ID}, unusable)

	return vaults, items
}

// mayAllow reports whether allow could allow a field of item, however
// it and its vault are referred to.
func mayAllow(allow *Allowlist, item *onepassword.Item) bool {
	vaults, items := itemNames(item)
	for _, vault := range vaults {
		for _, name := range items {
			if allow.mayAllowItem(vault, name) {
				return true
			}
		}
	}

	return false
}

// allowsAnyField reports whether allow allows one of refs, the refs of
// item's fields, however it and its vault are referred to. op uses
// names or IDs in refs, but an entry can use either.
func allowsAnyField(allow *Allowlist, item *onepassword.Item, refs []string) bool {
	vaults, items := itemNames(item)
	for _, ref := range refs {
		// op://vault/item/[section/]field[?query], of which only the
		// field, and its section, is kept.
		parts := strings.SplitN(strings.TrimPrefix(ref, refPrefix), "/", 3)
		if len(parts) != 3 {
			continue
		}

		field, _, _ := strings.Cut(parts[2], "?")
		for _, vault := range vaults {
			for _, name := range items {
				if allow.allowsField(refPrefix + vault + "/" + name + "/" + field) {
					return true
				}
			}
		}
	}

	return false
}

// allowedItems returns the items, listed with op, that have a field
// allow allows, only from the vault with the name or ID vault, if it's
// set. Only the items allow could allow are given to op to get their
// fields.
//
// vault isn't given to op, since op failing to find it would tell the
// container whether a vault it may not read from exists.
func allowedItems(ctx context.Context, op OP, allow *Allowlist, vault string) ([]onepassword.Item, error) {
	items, err := op.ListItems(ctx)
	if err != nil {
		return nil, err
	}

	var candidates []onepassword.Item
	for i := range items {
		inVault := vault == "" || items[i].Vault.Name == vault || items[i].Vault.ID == vault
		if inVault && mayAllow(allow, &items[i]) {
			candidates = append(candidates, items[i])
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	refs, err := op.FieldRefs(ctx, candidates)
	if err != nil {
		return nil, err
	}

	var allowed []onepassword.Item
	for i := range candidates {
		if allowsAnyField(allow, &candidates[i], refs[candidates[i].ID]) {
			allowed = append(allowed, candidates[i])
		}
	}

	return allowed, nil
}
