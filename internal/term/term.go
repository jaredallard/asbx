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

// Package term is the terminal asbx runs in: what it can show, and
// prompting the user at it.
package term

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	xterm "github.com/charmbracelet/x/term"
)

// Escape returns text with the characters that a terminal would act on
// rather than show, such as escape sequences, carriage returns and
// bidirectional overrides, replaced with Go escapes. Otherwise text
// from elsewhere, such as a file the user is reviewing, could hide what
// it contains or send the terminal escape sequences. Newlines and tabs
// are kept.
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

// Terminal is where the user is shown output and prompted.
type Terminal struct {
	// In is where the user's answer is read from.
	In io.Reader

	// Out is where output and prompts are written to.
	Out io.Writer

	// Interactive is whether there's a user at In and Out to prompt.
	Interactive bool

	// Accessible is whether to prompt with plain lines of text, rather
	// than interactive widgets, such as for screen readers.
	Accessible bool

	// Profile is the colors Out shows. The zero value shows none.
	Profile colorprofile.Profile

	// Width is Out's width in columns, or 0 if it isn't known.
	Width int
}

// Render returns s, which may be styled with any colors, with its colors
// reduced to those t shows, or removed if it shows none.
func (t *Terminal) Render(s string) string {
	var b strings.Builder
	w := colorprofile.Writer{Forward: &b, Profile: t.Profile}

	//nolint:errcheck // Why: writing to a strings.Builder can't fail.
	w.WriteString(s)

	return b.String()
}

// maxPromptWidth is the most columns a prompt is drawn across, so that
// its description stays easy to read on a wide terminal.
const maxPromptWidth = 80

// promptWidth returns the columns prompts are drawn across at t, Out's
// width up to [maxPromptWidth].
func (t *Terminal) promptWidth() int {
	if t.Width <= 0 {
		return maxPromptWidth
	}

	return min(t.Width, maxPromptWidth)
}

// describe returns description wrapped to fit, once indented, within
// the width of prompts at t. Otherwise huh would wrap it to the full
// width, and the indent would push the ends of its lines onto lines of
// their own.
func (t *Terminal) describe(description string) string {
	width := t.promptWidth() - descriptionStyle.GetHorizontalPadding()
	return lipgloss.Wrap(description, width, "")
}

// form returns a form with fields that prompts the user at t.
func (t *Terminal) form(fields ...huh.Field) *huh.Form {
	// The interactive widgets detect Out's colors themselves, which they
	// can only do when it's the terminal itself.
	out := t.Out
	if t.Accessible {
		out = &colorprofile.Writer{Forward: t.Out, Profile: t.Profile}
	}

	return huh.NewForm(huh.NewGroup(fields...)).
		WithInput(t.In).
		WithOutput(out).
		WithAccessible(t.Accessible).
		WithTheme(huh.ThemeFunc(theme)).
		WithWidth(t.promptWidth()).
		WithShowHelp(false)
}

// run prompts the user at t with field, which is described by
// description, if it isn't empty. Accessible prompts leave descriptions
// out, so it's written as a line before them instead.
func (t *Terminal) run(description string, field huh.Field) error {
	if t.Accessible && description != "" {
		if _, err := fmt.Fprintln(t.Out, description); err != nil {
			return err
		}
	}

	return t.form(field).Run()
}

// summary returns the line recording that the user answered question
// with answer, styled.
func summary(question, answer string) string {
	return summaryStyle.Render(symbolDone+" "+question+" "+answer) + "\n"
}

// record writes the line recording that the user answered question with
// answer, since interactive prompts are cleared once they're answered.
// Accessible prompts stay as they were, so nothing is written for them.
func (t *Terminal) record(question, answer string) error {
	if t.Accessible {
		return nil
	}

	_, err := fmt.Fprint(t.Out, t.Render(summary(question, answer)))
	return err
}

// Confirm asks the user at t question, which they answer yes or no, with
// description, if it isn't empty, telling them more about it. No is the
// default, and the user cancelling the prompt is a no.
func (t *Terminal) Confirm(question, description string) (bool, error) {
	var ok bool
	confirm := huh.NewConfirm().
		Title(title(question, t.promptWidth())).
		Description(t.describe(description)).
		Affirmative("Yes").
		Negative("No").
		WithButtonAlignment(lipgloss.Left).
		Value(&ok)

	err := t.run(description, confirm)
	if errors.Is(err, huh.ErrUserAborted) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	answer := "No"
	if ok {
		answer = "Yes"
	}

	return ok, t.record(question, answer)
}

// ReadSecret asks the user at t for a secret, such as a token, with
// prompt, without showing what they type. description, if it isn't
// empty, tells them how to get it. It can't be empty. ok is false when
// the user cancelled the prompt instead.
func (t *Terminal) ReadSecret(prompt, description string) (secret string, ok bool, err error) {
	input := huh.NewInput().
		Title(title(prompt, t.promptWidth())).
		Description(t.describe(description)).
		Prompt(symbolInput + " ").
		EchoMode(huh.EchoModePassword).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return errors.New("can't be empty")
			}

			return nil
		}).
		Value(&secret)

	err = t.run(description, input)
	if errors.Is(err, huh.ErrUserAborted) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	if err := t.record(prompt, symbolMask); err != nil {
		return "", false, err
	}

	return strings.TrimSpace(secret), true, nil
}

// New returns the terminal asbx is running in, on stdin and stderr.
// It's only interactive when both are terminals. Colors are detected
// from stderr and the environment, such as NO_COLOR and COLORTERM, and
// prompts are plain text when ACCESSIBLE is set.
func New() *Terminal {
	interactive := xterm.IsTerminal(os.Stdin.Fd()) && xterm.IsTerminal(os.Stderr.Fd())

	width, _, err := xterm.GetSize(os.Stderr.Fd())
	if err != nil {
		// Width is unknown.
		width = 0
	}

	profile := colorprofile.NoTTY
	if interactive {
		profile = colorprofile.Detect(os.Stderr, os.Environ())
	}

	return &Terminal{
		In:          os.Stdin,
		Out:         os.Stderr,
		Interactive: interactive,
		Accessible:  os.Getenv("ACCESSIBLE") != "",
		Profile:     profile,
		Width:       width,
	}
}
