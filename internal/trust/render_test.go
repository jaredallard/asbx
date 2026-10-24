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

package trust

import (
	"strings"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/term"
	"github.com/charmbracelet/colorprofile"
	"gotest.tools/v3/assert"
)

// plain is a terminal without color, 30 columns wide, so frames are easy
// to compare.
var plain = &term.Terminal{Width: 30}

// TestRenderNew verifies that a new file is drawn in full, numbered, in
// a frame naming it.
func TestRenderNew(t *testing.T) {
	got, err := renderNew("/p/.asbx.yaml", []byte("network_host: true\nextra_mounts: []\n"), plain)
	assert.NilError(t, err)

	want := `───────┬──────────────────────
       │ File: /p/.asbx.yaml (new)
───────┼──────────────────────
   1   │ network_host: true
   2   │ extra_mounts: []
───────┴──────────────────────
`
	assert.Equal(t, got, want)
}

// TestRenderChanged verifies that only the changes to a file, with the
// lines around them, are drawn, with each line numbered by where it is
// now, or was if it was removed.
func TestRenderChanged(t *testing.T) {
	tests := []struct {
		name string
		old  string
		new  string
		want string
	}{
		{
			name: "ChangedLine",
			old:  "a: 1\nnetwork_host: false\nextra_mounts: []\nclaude: {}\n",
			new:  "a: 1\nnetwork_host: false\nextra_mounts:\n  - /x\nclaude: {}\n",
			want: `───────┬──────────────────────
       │ File: /p/.asbx.yaml (changed)
───────┼──────────────────────
   1   │ a: 1
   2   │ network_host: false
   3 - │ extra_mounts: []
   3 + │ extra_mounts:
   4 + │   - /x
   5   │ claude: {}
───────┴──────────────────────
`,
		},
		{
			name: "AddedLastLine",
			old:  "a: 1\n",
			new:  "a: 1\nb: 2\n",
			want: `───────┬──────────────────────
       │ File: /p/.asbx.yaml (changed)
───────┼──────────────────────
   1   │ a: 1
   2 + │ b: 2
───────┴──────────────────────
`,
		},
		{
			// The changes are far enough apart to be separate hunks, and
			// the second is numbered after the line the first added.
			name: "SeparateChanges",
			old:  "a: 1\nb: 1\nc: 1\nd: 1\ne: 1\nf: 1\ng: 1\nh: 1\ni: 1\nj: 1\n",
			new:  "a: 2\nz: 2\nb: 1\nc: 1\nd: 1\ne: 1\nf: 1\ng: 1\nh: 1\nj: 1\n",
			want: `───────┬──────────────────────
       │ File: /p/.asbx.yaml (changed)
───────┼──────────────────────
   1 - │ a: 1
   1 + │ a: 2
   2 + │ z: 2
   3   │ b: 1
   4   │ c: 1
   5   │ d: 1
───────┼──────────────────────
   7   │ f: 1
   8   │ g: 1
   9   │ h: 1
   9 - │ i: 1
  10   │ j: 1
───────┴──────────────────────
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderChanged("/p/.asbx.yaml", []byte(tt.old), []byte(tt.new), plain)
			assert.NilError(t, err)
			assert.Equal(t, got, tt.want)
		})
	}
}

// TestRenderEscapes verifies that characters a terminal would act on,
// rather than show, are drawn escaped, so a file can't hide its contents
// from the user reviewing it.
func TestRenderEscapes(t *testing.T) {
	got, err := renderNew("/p/.asbx.yaml", []byte("a: 1\x1b[2K\r\u202e\xff\tb\n"), plain)
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(got, `   1   │ a: 1\x1b[2K\r\u202e\xff`+"\tb\n"), "renderNew() =\n%s", got)
}

// TestRenderColor verifies that, with color, the file is syntax
// highlighted and added and removed lines are marked in green and red.
func TestRenderColor(t *testing.T) {
	terminal := &term.Terminal{Profile: colorprofile.ANSI256, Width: 30}

	got, err := renderChanged("/p/.asbx.yaml", []byte("a: 1\n"), []byte("a: 2\n"), terminal)
	assert.NilError(t, err)

	assert.Assert(t, strings.Contains(got, removedStyle.Render("-")), "renderChanged() =\n%q", got)
	assert.Assert(t, strings.Contains(got, addedStyle.Render("+")), "renderChanged() =\n%q", got)

	// The key is highlighted, reduced to the terminal's 256 colors, so
	// it isn't next to the gutter as is.
	assert.Assert(t, !strings.Contains(got, frameStyle.Render(gutterLine)+" a:"), "renderChanged() =\n%q", got)
	assert.Assert(t, strings.Contains(got, "\033[38;5;"), "renderChanged() =\n%q", got)
	assert.Assert(t, !strings.Contains(got, "\033[38;2;"), "renderChanged() =\n%q", got)
}
