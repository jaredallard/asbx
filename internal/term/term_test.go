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

package term

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"gotest.tools/v3/assert"
)

// TestEscape verifies that characters a terminal would act on, rather
// than show, are replaced with Go escapes, and everything else, newlines
// and tabs included, is kept.
func TestEscape(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{text: "a: 1\n\tb", want: "a: 1\n\tb"},
		{text: "héllo 🍱", want: "héllo 🍱"},
		{text: "\x1b[2J\r", want: `\x1b[2J\r`},
		{text: "\u202eevil", want: `\u202eevil`},
		{text: "\xff", want: `\xff`},
	}

	for _, tt := range tests {
		assert.Equal(t, Escape(tt.text), tt.want, "Escape(%q)", tt.text)
	}
}

// TestRender verifies that styled text keeps only the colors the
// terminal shows.
func TestRender(t *testing.T) {
	text := "\x1b[38;2;255;0;0mred\x1b[m"

	tests := []struct {
		name    string
		profile colorprofile.Profile
		want    string
	}{
		{name: "None", want: "red"},
		{name: "NoTTY", profile: colorprofile.NoTTY, want: "red"},
		{name: "ANSI256", profile: colorprofile.ANSI256, want: "\x1b[38;5;196mred\x1b[m"},
		{name: "TrueColor", profile: colorprofile.TrueColor, want: text},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, (&Terminal{Profile: tt.profile}).Render(text), tt.want)
		})
	}
}

// TestConfirm verifies that only a yes is a yes, and that no is the
// default.
func TestConfirm(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		want   bool
	}{
		{name: "Yes", answer: "yes\n", want: true},
		{name: "Y", answer: "Y\n", want: true},
		{name: "No", answer: "n\n"},
		{name: "Empty", answer: "\n"},
		{name: "EOF"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			terminal := &Terminal{In: strings.NewReader(tt.answer), Out: &out, Interactive: true, Accessible: true}

			got, err := terminal.Confirm("Continue?", "")
			assert.NilError(t, err)
			assert.Equal(t, got, tt.want)
			assert.Assert(t, strings.Contains(out.String(), "Continue? [y/N]"), "Confirm() wrote:\n%s", out.String())
		})
	}
}

// TestConfirmDescription verifies that an accessible prompt shows its
// description before the question, since huh leaves it out.
func TestConfirmDescription(t *testing.T) {
	var out strings.Builder
	terminal := &Terminal{In: strings.NewReader("y\n"), Out: &out, Interactive: true, Accessible: true}

	got, err := terminal.Confirm("Continue?", "It can't be undone.")
	assert.NilError(t, err)
	assert.Assert(t, got, "Confirm() = false, want true")

	description := strings.Index(out.String(), "It can't be undone.\n")
	question := strings.Index(out.String(), "Continue? [y/N]")
	assert.Assert(t, description >= 0 && description < question, "Confirm() wrote:\n%s", out.String())
}

// TestSummary verifies that an answered prompt is recorded as the
// question followed by its answer.
func TestSummary(t *testing.T) {
	got := (&Terminal{}).Render(summary("Continue?", "Yes"))
	assert.Equal(t, got, "✓ Continue? Yes\n")
}

// TestTitle verifies that a title too long for the width wraps, with
// the lines after the first indented under its marker.
func TestTitle(t *testing.T) {
	got := (&Terminal{}).Render(title("Create the item in the vault?", 20))
	assert.Equal(t, got, "? Create the item in\n  the vault?")
}
