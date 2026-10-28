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

package oscfilter_test

import (
	"bytes"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/oscfilter"
)

func TestFilter(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		out    string
		copies []string
	}{
		{"plain text", []string{"hello\r\n"}, "hello\r\n", nil},
		{"bel", []string{"a\x1b]52;c;aGk=\x07b"}, "ab", []string{"hi"}},
		{"st", []string{"a\x1b]52;c;aGk=\x1b\\b"}, "ab", []string{"hi"}},
		{"split", []string{"a\x1b", "]52;", "c;aG", "k=\x1b", "\\b"}, "ab", []string{"hi"}},
		{"unpadded", []string{"\x1b]52;c;aGk\x07"}, "", []string{"hi"}},
		{"other selection", []string{"\x1b]52;p;aGk=\x07"}, "", []string{"hi"}},
		{"query", []string{"\x1b]52;c;?\x07x"}, "x", nil},
		{"bad base64", []string{"\x1b]52;c;!!!\x07x"}, "x", nil},
		{"empty data", []string{"\x1b]52;c;\x07x"}, "x", nil},
		{"title passes", []string{"\x1b]0;title\x07x"}, "\x1b]0;title\x07x", nil},
		{"short osc passes", []string{"\x1b]0\x07x"}, "\x1b]0\x07x", nil},
		{"hyperlink passes", []string{"\x1b]8;;http://a\x1b\\t\x1b]8;;\x1b\\"}, "\x1b]8;;http://a\x1b\\t\x1b]8;;\x1b\\", nil},
		{"csi passes", []string{"\x1b[31mred\x1b[0m"}, "\x1b[31mred\x1b[0m", nil},
		{"two", []string{"\x1b]52;c;YQ==\x07\x1b]52;c;Yg==\x07"}, "", []string{"a", "b"}},
		{"oversize dropped", []string{"\x1b]52;c;" + "QUFBQUFBQUFBQQ==" + "\x07x"}, "x", nil},
		{"aborted by esc", []string{"\x1b]52;c;aGk\x1b[31mx"}, "\x1b[31mx", nil},
		{"oversize st dropped", []string{"\x1b]52;c;QUFBQUFBQUFBQQ==\x1b\\x"}, "x", nil},
		{"short prefix cut by osc 52", []string{"\x1b]5\x1b]52;c;aGk=\x07x"}, "\x1b]5x", []string{"hi"}},
		{"short prefix cancelled", []string{"\x1b]5\x18x"}, "\x1b]5\x18x", nil},
		{"cancelled", []string{"\x1b]52;c;aGk\x18x"}, "x", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			var copies []string
			f := oscfilter.New(&out, 8, func(d []byte) { copies = append(copies, string(d)) })

			for _, w := range tt.writes {
				if n, err := f.Write([]byte(w)); err != nil || n != len(w) {
					t.Fatalf("Write() = %d, %v", n, err)
				}
			}

			if out.String() != tt.out {
				t.Errorf("out = %q, want %q", out.String(), tt.out)
			}
			if len(copies) != len(tt.copies) {
				t.Fatalf("copies = %q, want %q", copies, tt.copies)
			}
			for i := range copies {
				if copies[i] != tt.copies[i] {
					t.Errorf("copies[%d] = %q, want %q", i, copies[i], tt.copies[i])
				}
			}
		})
	}
}
