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
	"bytes"
	"cmp"
	"io"
	"slices"
)

// concealed replaces secrets in op run's output, as op does.
const concealed = "<concealed by 1Password>"

// MaskWriter writes to another writer with secrets replaced by
// "<concealed by 1Password>". Output that could be the start of a
// secret is held back until a later write shows whether it is, so
// [MaskWriter.Flush] must be called after the last write.
type MaskWriter struct {
	w io.Writer

	// secrets are longest first, so a secret that starts with another
	// is masked whole.
	secrets [][]byte

	held []byte
}

// partial reports whether b is the start of a secret, but not all of
// it.
func (m *MaskWriter) partial(b []byte) bool {
	for _, s := range m.secrets {
		if len(b) < len(s) && bytes.HasPrefix(s, b) {
			return true
		}
	}

	return false
}

// match returns the length of the secret b starts with, or 0.
func (m *MaskWriter) match(b []byte) int {
	for _, s := range m.secrets {
		if bytes.HasPrefix(b, s) {
			return len(s)
		}
	}

	return 0
}

// mask writes the held output with secrets masked. Unless final, it
// stops at output that could be the start of a secret, which stays
// held.
func (m *MaskWriter) mask(final bool) error {
	var out []byte
	i := 0
	for i < len(m.held) {
		rest := m.held[i:]
		if !final && m.partial(rest) {
			break
		}

		if n := m.match(rest); n > 0 {
			out = append(out, concealed...)
			i += n
			continue
		}

		out = append(out, rest[0])
		i++
	}

	m.held = append(m.held[:0], m.held[i:]...)

	_, err := m.w.Write(out)
	return err
}

// Write implements [io.Writer].
func (m *MaskWriter) Write(p []byte) (int, error) {
	m.held = append(m.held, p...)
	if err := m.mask(false); err != nil {
		return 0, err
	}

	return len(p), nil
}

// Flush writes the output held back by Write.
func (m *MaskWriter) Flush() error {
	return m.mask(true)
}

// NewMaskWriter returns a [MaskWriter] that writes to w with secrets
// masked.
func NewMaskWriter(w io.Writer, secrets []string) *MaskWriter {
	m := &MaskWriter{w: w}
	for _, s := range secrets {
		if s != "" {
			m.secrets = append(m.secrets, []byte(s))
		}
	}

	slices.SortFunc(m.secrets, func(a, b []byte) int {
		return cmp.Compare(len(b), len(a))
	})

	return m
}
