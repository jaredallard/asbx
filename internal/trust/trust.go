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

// Package trust has the user review a configuration file before asbx
// uses it, and remembers what they trusted, so they're only asked again
// when it changes. A new file is shown in full, a changed one as a diff
// against what was trusted.
package trust

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotTrusted is returned when a file isn't trusted by the user.
var ErrNotTrusted = errors.New("configuration not trusted")

// Terminal is where the user is asked to trust a file.
type Terminal struct {
	// In is where the user's answer is read from.
	In io.Reader

	// Out is where the file and the question are written to.
	Out io.Writer

	// Interactive is whether there's a user at In and Out to ask. When
	// there isn't, files that aren't trusted are refused.
	Interactive bool

	// Color is whether Out shows ANSI colors.
	Color bool

	// TrueColor is whether Out shows 24-bit colors, rather than only 256.
	TrueColor bool

	// Width is Out's width in columns, or 0 if it isn't known.
	Width int
}

// Store is where the contents of trusted files are kept, as of when
// they were trusted. Removing a file's copy from it revokes its trust.
type Store struct {
	// Dir is the directory the copies are kept in.
	Dir string
}

// copyPath returns where the trusted copy of the file at path is kept.
func (s Store) copyPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return filepath.Join(s.Dir, hex.EncodeToString(sum[:])+".yaml")
}

// confirm asks the user at term whether to trust what was shown to
// them. Only "y" or "yes" trusts it.
func confirm(term Terminal) (bool, error) {
	if _, err := fmt.Fprint(term.Out, "Trust this configuration? [y/N] "); err != nil {
		return false, err
	}

	answer, err := bufio.NewReader(term.In).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}

	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// Verify returns nil if data, the contents of the file at path, which
// should be absolute, is what the user last trusted for it. Otherwise
// it's shown to the user at term, in full if they haven't trusted the
// file before or as a diff if it has changed, and they're asked to
// trust it. If they do, data is stored as trusted. If they don't, or
// term isn't interactive, an error wrapping ErrNotTrusted is returned.
//
// data must be what the caller goes on to use, rather than the file
// being read again, so that it can't change after it's been trusted.
func (s Store) Verify(path string, data []byte, term Terminal) error {
	copyPath := s.copyPath(path)

	trusted, err := os.ReadFile(copyPath) //nolint:gosec // Why: copyPath is in the store's own directory.
	isNew := errors.Is(err, fs.ErrNotExist)
	if err != nil && !isNew {
		return err
	}

	if !isNew && bytes.Equal(trusted, data) {
		return nil
	}

	state := "changed since it was trusted"
	if isNew {
		state = "new"
	}

	if !term.Interactive {
		return fmt.Errorf("%w: %s is %s, run asbx in a terminal to review it", ErrNotTrusted, path, state)
	}

	var view string
	if isNew {
		view, err = renderNew(path, data, term)
	} else {
		view, err = renderChanged(path, trusted, data, term)
	}
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(term.Out, "%s is %s, review it before it's used:\n%s", path, state, view); err != nil {
		return err
	}

	ok, err := confirm(term)
	if err != nil {
		return err
	}

	if !ok {
		return fmt.Errorf("%w: %s", ErrNotTrusted, path)
	}

	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return err
	}

	return os.WriteFile(copyPath, data, 0o600)
}
