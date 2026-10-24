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
	"fmt"
	"path"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// refPrefix starts every 1Password secret reference.
const refPrefix = "op://"

// itemPattern is the vault and item segments of an entry, as
// [doublestar.Match] patterns.
type itemPattern struct {
	vault string
	item  string
}

// Allowlist is the op:// refs the container may read. The zero value
// allows nothing.
type Allowlist struct {
	// patterns are the entries as [doublestar.Match] patterns.
	patterns []string

	// fields are the entries without their query strings, such as
	// ?attribute=otp, as [doublestar.Match] patterns. They match the
	// fields whose attributes the entries allow.
	fields []string

	// items are the vault and item segments of fields, of the entries
	// that have them.
	items []itemPattern
}

// match reports whether ref matches one of patterns. Refs that aren't
// clean paths, such as ones with a .. segment, never match, so they
// can't be used to step outside of an entry.
func match(patterns []string, ref string) bool {
	rest, ok := strings.CutPrefix(ref, refPrefix)
	if !ok || path.Clean("/"+rest) != "/"+rest {
		return false
	}

	for _, pattern := range patterns {
		matched, err := doublestar.Match(pattern, ref)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// Allows reports whether ref matches one of the entries.
func (a *Allowlist) Allows(ref string) bool {
	return match(a.patterns, ref)
}

// allowsField reports whether one of the entries allows the field ref,
// without a query string, refers to, or an attribute of it.
func (a *Allowlist) allowsField(ref string) bool {
	return match(a.fields, ref)
}

// mayAllowItem reports whether one of the entries could allow a field
// of item in vault, going by their vault and item segments alone. It
// can be wrong about one that does, but not about one that doesn't.
// Neither name may contain a /.
func (a *Allowlist) mayAllowItem(vault, item string) bool {
	for _, p := range a.items {
		vaultMatched, err := doublestar.Match(p.vault, vault)
		if err != nil || !vaultMatched {
			continue
		}

		itemMatched, err := doublestar.Match(p.item, item)
		if err == nil && itemMatched {
			return true
		}
	}

	return false
}

// Empty reports whether a allows nothing.
func (a *Allowlist) Empty() bool {
	return len(a.patterns) == 0
}

// Len returns the number of entries in a.
func (a *Allowlist) Len() int {
	return len(a.patterns)
}

// pattern returns entry as a [doublestar.Match] pattern where only *
// and ** are special, so that ? in a query string, such as
// ?attribute=otp, is matched literally.
func pattern(entry string) string {
	var b strings.Builder
	for _, r := range entry {
		if strings.ContainsRune(`?[{\`, r) {
			b.WriteByte('\\')
		}

		b.WriteRune(r)
	}

	return b.String()
}

// ParseAllowlist returns the Allowlist for op_allowed_secret_refs
// entries, which are op:// refs where * matches anything within a
// path segment and a /**/ segment matches any number of them.
func ParseAllowlist(entries []string) (*Allowlist, error) {
	allow := &Allowlist{}
	for _, entry := range entries {
		if entry == "" {
			continue
		}

		if !strings.HasPrefix(entry, refPrefix) {
			return nil, fmt.Errorf("op_allowed_secret_refs entry %q must be an op:// ref", entry)
		}

		field, _, _ := strings.Cut(entry, "?")
		allow.patterns = append(allow.patterns, pattern(entry))
		allow.fields = append(allow.fields, pattern(field))

		// A leading ** spans any vault and item. Otherwise, an entry
		// without an item segment allows no fields.
		segments := strings.Split(strings.TrimPrefix(pattern(field), refPrefix), "/")
		switch {
		case segments[0] == "**":
			allow.items = append(allow.items, itemPattern{vault: "**", item: "**"})
		case len(segments) >= 2:
			allow.items = append(allow.items, itemPattern{vault: segments[0], item: segments[1]})
		}
	}

	return allow, nil
}
