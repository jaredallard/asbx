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
	"embed"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"git.rgst.io/jaredallard/asbx/internal/term"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/aymanbagabas/go-udiff"
)

// The box-drawing characters of the frame a file is drawn in, as bat
// draws it.
const (
	ruleLine   = "─" // ─
	ruleTop    = "┬" // ┬
	ruleMiddle = "┼" // ┼
	ruleBottom = "┴" // ┴
	gutterLine = "│" // │
)

// The styles the frame is drawn with. They're the terminal's own
// colors, so the frame follows its theme.
var (
	frameStyle   = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	removedStyle = lipgloss.NewStyle().Foreground(lipgloss.Red)
	addedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Green)
)

const (
	// gutterWidth is the width of the gutter, before its line: a line
	// number, then a marker for added and removed lines.
	gutterWidth = 7

	// defaultWidth is the width of the frame when the terminal's isn't
	// known.
	defaultWidth = 80

	// contextLines is how many unchanged lines are shown around each
	// change.
	contextLines = 3
)

// lexerFS holds chroma's YAML lexer. It's built in on its own, since
// chroma's lexers package builds in those for every language, which we
// don't need (increases the binary size a lot).
//
//go:embed embed/yaml.xml
var lexerFS embed.FS

// yamlLexer highlights YAML.
var yamlLexer = chroma.MustNewXMLLexer(lexerFS, "embed/yaml.xml")

// highlight returns the lines of text, without their newlines, syntax
// highlighted as YAML in true color, to be reduced to the terminal's
// colors when the frame is rendered.
func highlight(text string) ([]string, error) {
	formatter := formatters.TTY16m
	style := styles.Get("monokai")

	// The whole text is tokenised at once, rather than each line, so
	// that values spanning lines are highlighted correctly.
	it, err := yamlLexer.Tokenise(nil, text)
	if err != nil {
		return nil, err
	}

	var lines []string
	for _, tokens := range chroma.SplitTokensIntoLines(it.Tokens()) {
		last := &tokens[len(tokens)-1]
		last.Value = strings.TrimSuffix(last.Value, "\n")

		var b strings.Builder
		if err := formatter.Format(&b, style, chroma.Literator(tokens...)); err != nil {
			return nil, err
		}

		lines = append(lines, b.String())
	}

	return lines, nil
}

// frame draws lines of a file the way bat does: in a box, with a header
// naming the file and a gutter of line numbers.
type frame struct {
	b        strings.Builder
	terminal *term.Terminal
	width    int
}

// String returns the frame drawn so far, in the terminal's colors.
func (f *frame) String() string {
	return f.terminal.Render(f.b.String())
}

// rule draws a horizontal line across the frame, meeting the gutter's
// line at join.
func (f *frame) rule(join string) {
	rest := max(f.width-gutterWidth-1, 0)
	line := strings.Repeat(ruleLine, gutterWidth) + join + strings.Repeat(ruleLine, rest)
	fmt.Fprintln(&f.b, frameStyle.Render(line))
}

// header draws the top of the frame, with title in it.
func (f *frame) header(title string) {
	f.rule(ruleTop)
	fmt.Fprintf(&f.b, "%*s%s %s\n", gutterWidth, "", frameStyle.Render(gutterLine), title)
	f.rule(ruleMiddle)
}

// line draws content as line number n, with marker, which is "+", "-"
// or " ", in the gutter.
func (f *frame) line(n int, marker, content string) {
	number := frameStyle.Render(fmt.Sprintf("%4d", n))

	switch marker {
	case "+":
		marker = addedStyle.Render(marker)
	case "-":
		marker = removedStyle.Render(marker)
	}

	fmt.Fprintf(&f.b, "%s %s %s %s\n", number, marker, frameStyle.Render(gutterLine), content)
}

// newFrame returns a frame as wide as t.
func newFrame(t *term.Terminal) *frame {
	width := t.Width
	if width <= 0 {
		width = defaultWidth
	}

	return &frame{terminal: t, width: width}
}

// lineAt returns lines[i], or fallback, without its newline, if lines is
// too short. The highlighted lines are expected to match the diff's,
// but if they don't, the line is still shown.
func lineAt(lines []string, i int, fallback string) string {
	if i < 0 || i >= len(lines) {
		return strings.TrimSuffix(fallback, "\n")
	}

	return lines[i]
}

// renderNew returns data, the contents of the file at path, drawn in
// full, for a file that hasn't been trusted before.
func renderNew(path string, data []byte, t *term.Terminal) (string, error) {
	lines, err := highlight(term.Escape(string(data)))
	if err != nil {
		return "", err
	}

	f := newFrame(t)
	f.header("File: " + term.Escape(path) + " (new)")

	for i, line := range lines {
		f.line(i+1, " ", line)
	}

	f.rule(ruleBottom)

	return f.String(), nil
}

// renderChanged returns the changes from old, the contents of the file
// at path when it was trusted, to data, its contents now. Each change is
// shown with the lines around it, and is numbered by the line it's on
// now, or was on if it was removed.
func renderChanged(path string, old, data []byte, t *term.Terminal) (string, error) {
	before, after := term.Escape(string(old)), term.Escape(string(data))

	diff, err := udiff.ToUnifiedDiff("", "", before, udiff.Lines(before, after), contextLines)
	if err != nil {
		return "", err
	}

	beforeLines, err := highlight(before)
	if err != nil {
		return "", err
	}

	afterLines, err := highlight(after)
	if err != nil {
		return "", err
	}

	f := newFrame(t)
	f.header("File: " + term.Escape(path) + " (changed)")

	// Hunks give only their line in the old file. Lines are added and
	// removed only within hunks, so the line in the new file is offset
	// from it by those before.
	offset := 0
	for i, hunk := range diff.Hunks {
		if i > 0 {
			f.rule(ruleMiddle)
		}

		oldLine := hunk.FromLine
		for _, line := range hunk.Lines {
			newLine := oldLine + offset

			switch line.Kind {
			case udiff.Delete:
				f.line(oldLine, "-", lineAt(beforeLines, oldLine-1, line.Content))
				oldLine++
				offset--
			case udiff.Insert:
				f.line(newLine, "+", lineAt(afterLines, newLine-1, line.Content))
				offset++
			case udiff.Equal:
				f.line(newLine, " ", lineAt(afterLines, newLine-1, line.Content))
				oldLine++
			}
		}
	}

	f.rule(ruleBottom)

	return f.String(), nil
}
