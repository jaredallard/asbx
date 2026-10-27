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
	"encoding/json"
	"os"
	"path/filepath"
)

// state is what's kept between runs about a tag, for when its image
// can't be resolved again.
type state struct {
	// Ref is the last image verified for the tag, by digest.
	Ref string `json:"ref"`
}

// statePath returns the path of tag's state.
func (r *Resolver) statePath(tag string) string {
	return filepath.Join(r.StateDir, "image-"+tag+".json")
}

// load returns the last image verified for tag, by digest.
func (r *Resolver) load(tag string) (string, error) {
	data, err := os.ReadFile(r.statePath(tag))
	if err != nil {
		return "", err
	}

	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return "", err
	}

	return s.Ref, nil
}

// save records ref, by digest, as the last image verified for tag.
func (r *Resolver) save(tag, ref string) error {
	data, err := json.Marshal(state{Ref: ref})
	if err != nil {
		return err
	}

	if err := os.MkdirAll(r.StateDir, 0o750); err != nil {
		return err
	}

	// Written beside it then renamed, so a run started at the same time
	// never reads part of it.
	f, err := os.CreateTemp(r.StateDir, ".image-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) //nolint:errcheck // Why: it's already gone once renamed.

	if _, err := f.Write(data); err != nil {
		f.Close() //nolint:errcheck,gosec // Why: the write already failed.
		return err
	}

	if err := f.Close(); err != nil {
		return err
	}

	return os.Rename(f.Name(), r.statePath(tag))
}
