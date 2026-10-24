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

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

// The styles prompts are drawn with. They're the terminal's own colors,
// like the banner's, so prompts follow its theme.
var (
	markerStyle        = lipgloss.NewStyle().Foreground(lipgloss.Magenta)
	titleStyle         = lipgloss.NewStyle().Bold(true)
	descriptionStyle   = lipgloss.NewStyle().Faint(true).PaddingLeft(2)
	focusedButtonStyle = lipgloss.NewStyle().Foreground(lipgloss.Magenta).Bold(true).MarginLeft(2)
	blurredButtonStyle = lipgloss.NewStyle().Faint(true).MarginLeft(2)
	errorStyle         = lipgloss.NewStyle().Foreground(lipgloss.Red)
	summaryStyle       = lipgloss.NewStyle().Faint(true)
)

// The symbols prompts are drawn with.
const (
	symbolPrompt = "?"
	symbolInput  = ">"
	symbolDone   = "✓"

	// symbolMask stands in for a secret once it's entered. It's always
	// the same length, so it doesn't give away the secret's.
	symbolMask = "••••••••"
)

// titleIndent is what's before each line of a prompt's title: its
// marker on the first, and as much space on the others.
const titleIndent = "  "

// title returns s as a prompt's title, after its marker, wrapped to
// width with the lines after the first indented under it. The marker is
// rendered here, rather than by the theme, since huh styles a title as
// a whole.
func title(s string, width int) string {
	lines := strings.Split(lipgloss.Wrap(s, width-len(titleIndent), ""), "\n")
	for i, line := range lines {
		lines[i] = titleStyle.Render(line)
	}

	return markerStyle.Render(symbolPrompt) + " " + strings.Join(lines, "\n"+titleIndent)
}

// theme returns the styles prompts are drawn with. A field has no
// border; its title, marked by [title], is followed by its description,
// indented under it, and then by its answer. Its colors are the
// terminal's own, so whether its background is dark doesn't matter.
func theme(isDark bool) *huh.Styles {
	t := huh.ThemeBase(isDark)

	t.Focused.Base = lipgloss.NewStyle()
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = lipgloss.NewStyle()
	t.Focused.NoteTitle = titleStyle
	t.Focused.Description = descriptionStyle
	t.Focused.ErrorIndicator = errorStyle.SetString(" *")
	t.Focused.ErrorMessage = errorStyle.PaddingLeft(2)
	t.Focused.FocusedButton = focusedButtonStyle
	t.Focused.BlurredButton = blurredButtonStyle
	t.Focused.Next = focusedButtonStyle
	t.Focused.TextInput.Prompt = markerStyle
	t.Focused.TextInput.Cursor = markerStyle
	t.Focused.TextInput.Placeholder = summaryStyle

	t.Blurred = t.Focused

	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description

	return t
}
