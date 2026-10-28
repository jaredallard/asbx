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

// Package oscfilter takes OSC 52 clipboard sequences out of a
// terminal's output, so they can be acted on rather than sent to a
// terminal that may not honor them, or that answers a query for the
// clipboard's contents.
package oscfilter

import (
	"encoding/base64"
	"io"
	"strings"
)

const (
	esc = 0x1b
	bel = 0x07
	can = 0x18
	sub = 0x1a
)

// state is where a [Filter] is in the output it's been written.
type state int

const (
	// stateText is outside of any sequence.
	stateText state = iota

	// stateEsc follows an ESC outside of a sequence.
	stateEsc

	// stateOSC is inside an OSC sequence, which may turn out to be 52.
	stateOSC

	// stateOSCEsc follows an ESC inside an OSC 52 sequence, which ends
	// it when followed by a backslash.
	stateOSCEsc

	// stateDrop is inside an OSC 52 sequence that's being discarded.
	stateDrop

	// stateDropEsc follows an ESC inside a discarded sequence, which
	// ends it when followed by a backslash.
	stateDropEsc
)

// Filter is an [io.Writer] that writes everything to a writer, except
// OSC 52 sequences that set the clipboard, which are given to a
// function instead. Queries for the clipboard and sequences that aren't
// valid are discarded.
type Filter struct {
	w     io.Writer
	copy  func([]byte)
	limit int

	state state

	// buf is the sequence read so far, after its introducer.
	buf []byte

	// out is what a call to Write passes through, kept to reuse its
	// memory.
	out []byte
}

// New returns a [Filter] that writes to w, and calls copy with the
// decoded data of each OSC 52 sequence that sets the clipboard. Data
// that would be more than maxData bytes is discarded.
func New(w io.Writer, maxData int, onCopy func(data []byte)) *Filter {
	return &Filter{w: w, copy: onCopy, limit: base64.StdEncoding.EncodedLen(maxData) + len("52;c;")}
}

// Write implements [io.Writer]. It always reports all of p as written
// unless the underlying writer fails.
func (f *Filter) Write(p []byte) (int, error) {
	out := f.out[:0]
	for _, b := range p {
		out = f.step(out, b)
	}

	f.out = out

	if len(out) > 0 {
		if _, err := f.w.Write(out); err != nil {
			return 0, err
		}
	}

	return len(p), nil
}

// step advances f by b, and returns out with anything b passes through
// added.
func (f *Filter) step(out []byte, b byte) []byte {
	switch f.state {
	case stateText:
		if b == esc {
			f.state = stateEsc
			return out
		}

		return append(out, b)
	case stateEsc:
		if b == ']' {
			f.state = stateOSC
			f.buf = f.buf[:0]
			return out
		}

		f.state = stateText
		out = append(out, esc)

		// It may start another sequence.
		return f.step(out, b)
	case stateOSC:
		return f.stepOSC(out, b)
	case stateOSCEsc:
		if b == '\\' {
			f.finish()
			return out
		}

		// The sequence was cut short by another.
		f.state = stateEsc
		f.buf = f.buf[:0]
		return f.step(out, b)
	case stateDropEsc:
		if b == '\\' {
			f.state = stateText
			return out
		}

		f.state = stateEsc
		return f.step(out, b)
	default: // stateDrop
		switch b {
		case bel, can, sub:
			f.state = stateText
		case esc:
			f.state = stateDropEsc
		}

		return out
	}
}

// stepOSC handles b inside an OSC sequence, which is held until it's
// known to be OSC 52.
func (f *Filter) stepOSC(out []byte, b byte) []byte {
	switch b {
	case bel:
		if f.is52() {
			f.finish()
			return out
		}

		return f.release(out, b)
	case esc:
		if f.is52() {
			f.state = stateOSCEsc
			return out
		}

		// It may end the sequence, or start another.
		out = f.release(out, 0)
		f.state = stateEsc

		return out
	case can, sub:
		if f.is52() {
			f.state = stateText
			f.buf = f.buf[:0]
			return out
		}

		return f.release(out, b)
	}

	f.buf = append(f.buf, b)

	switch {
	case len(f.buf) < len("52;"):
		return out
	case !f.is52():
		return f.release(out, 0)
	case len(f.buf) > f.limit:
		f.state = stateDrop
		f.buf = f.buf[:0]
	}

	return out
}

// is52 returns whether the sequence read so far is OSC 52.
func (f *Filter) is52() bool {
	return len(f.buf) >= len("52;") && string(f.buf[:3]) == "52;"
}

// release passes the sequence read so far through, followed by b if
// it's not 0, once it's known not to be OSC 52.
func (f *Filter) release(out []byte, b byte) []byte {
	out = append(out, esc, ']')
	out = append(out, f.buf...)
	if b != 0 {
		out = append(out, b)
	}

	f.buf = f.buf[:0]
	f.state = stateText

	return out
}

// finish ends an OSC 52 sequence, setting the clipboard if it asks to.
func (f *Filter) finish() {
	defer func() {
		f.buf = f.buf[:0]
		f.state = stateText
	}()

	// Pc;Pd, where Pc picks the selection, which only the clipboard is
	// supported of, so it's ignored.
	_, pd, ok := strings.Cut(string(f.buf[len("52;"):]), ";")
	if !ok || pd == "" || pd == "?" {
		return
	}

	data, err := base64.StdEncoding.DecodeString(pd)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(pd)
	}
	if err != nil {
		return
	}

	f.copy(data)
}
