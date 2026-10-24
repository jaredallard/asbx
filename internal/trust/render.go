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
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
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

// The ANSI escape sequences the frame is drawn with.
const (
	ansiReset = "\033[0m"
	ansiGrey  = "\033[90m"
	ansiRed   = "\033[31m"
	ansiGreen = "\033[32m"
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

// Escape returns text with the characters that a terminal would act on
// rather than show, such as escape sequences, carriage returns and
// bidirectional overrides, replaced with Go escapes. Otherwise a file
// could hide what it contains from the user reviewing it. Newlines and
// tabs are kept.
func Escape(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])

		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, text[i])
		case r == '\n', r == '\t':
			b.WriteRune(r)
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			quoted := strconv.QuoteRuneToASCII(r)
			b.WriteString(quoted[1 : len(quoted)-1])
		default:
			b.WriteString(text[i : i+size])
		}

		i += size
	}

	return b.String()
}

// highlight returns the lines of text, without their newlines, syntax
// highlighted as YAML when term has color.
func highlight(text string, term Terminal) ([]string, error) {
	if !term.Color {
		lines := strings.SplitAfter(text, "\n")
		for i, line := range lines {
			lines[i] = strings.TrimSuffix(line, "\n")
		}

		// The empty string after the final newline isn't a line.
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}

		return lines, nil
	}

	lexer := lexers.Get("yaml")
	if lexer == nil {
		lexer = lexers.Fallback
	}

	formatter := formatters.TTY256
	if term.TrueColor {
		formatter = formatters.TTY16m
	}

	style := styles.Get("monokai")

	// The whole text is tokenised at once, rather than each line, so
	// that values spanning lines are highlighted correctly.
	it, err := lexer.Tokenise(nil, text)
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
	b     strings.Builder
	term  Terminal
	width int
}

// paint returns s in color, when the terminal has it.
func (f *frame) paint(color, s string) string {
	if !f.term.Color {
		return s
	}

	return color + s + ansiReset
}

// rule draws a horizontal line across the frame, meeting the gutter's
// line at join.
func (f *frame) rule(join string) {
	rest := max(f.width-gutterWidth-1, 0)
	line := strings.Repeat(ruleLine, gutterWidth) + join + strings.Repeat(ruleLine, rest)
	fmt.Fprintln(&f.b, f.paint(ansiGrey, line))
}

// header draws the top of the frame, with title in it.
func (f *frame) header(title string) {
	f.rule(ruleTop)
	fmt.Fprintf(&f.b, "%*s%s %s\n", gutterWidth, "", f.paint(ansiGrey, gutterLine), title)
	f.rule(ruleMiddle)
}

// line draws content as line number n, with marker, which is "+", "-"
// or " ", in the gutter.
func (f *frame) line(n int, marker, content string) {
	number := f.paint(ansiGrey, fmt.Sprintf("%4d", n))

	switch marker {
	case "+":
		marker = f.paint(ansiGreen, marker)
	case "-":
		marker = f.paint(ansiRed, marker)
	}

	fmt.Fprintf(&f.b, "%s %s %s %s\n", number, marker, f.paint(ansiGrey, gutterLine), content)
}

// newFrame returns a frame as wide as term.
func newFrame(term Terminal) *frame {
	width := term.Width
	if width <= 0 {
		width = defaultWidth
	}

	return &frame{term: term, width: width}
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
func renderNew(path string, data []byte, term Terminal) (string, error) {
	lines, err := highlight(Escape(string(data)), term)
	if err != nil {
		return "", err
	}

	f := newFrame(term)
	f.header("File: " + Escape(path) + " (new)")

	for i, line := range lines {
		f.line(i+1, " ", line)
	}

	f.rule(ruleBottom)

	return f.b.String(), nil
}

// renderChanged returns the changes from old, the contents of the file
// at path when it was trusted, to data, its contents now. Each change is
// shown with the lines around it, and is numbered by the line it's on
// now, or was on if it was removed.
func renderChanged(path string, old, data []byte, term Terminal) (string, error) {
	before, after := Escape(string(old)), Escape(string(data))

	diff, err := udiff.ToUnifiedDiff("", "", before, udiff.Lines(before, after), contextLines)
	if err != nil {
		return "", err
	}

	beforeLines, err := highlight(before, term)
	if err != nil {
		return "", err
	}

	afterLines, err := highlight(after, term)
	if err != nil {
		return "", err
	}

	f := newFrame(term)
	f.header("File: " + Escape(path) + " (changed)")

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

	return f.b.String(), nil
}
