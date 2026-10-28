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

// Package clipshim implements the clipboard tools asbx-clip stands in
// for inside the container: wl-copy, wl-paste, xclip, xsel, pbcopy and
// pbpaste. It supports the options agents and scripts commonly use, and
// reports any other as unsupported, rather than ignoring it.
package clipshim

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"git.rgst.io/jaredallard/asbx/internal/clipbridge"
)

// Tools are the names asbx-clip can run as.
var Tools = []string{"wl-copy", "wl-paste", "xclip", "xsel", "pbcopy", "pbpaste"}

// The operations a tool is asked to do.
const (
	opCopy  = "copy"
	opPaste = "paste"
	opTypes = "types"
)

// IO is where a tool reads its input from and writes its output to.
type IO struct {
	In  io.Reader
	Out io.Writer
}

// action is what a tool was asked to do, once its arguments are parsed.
type action struct {
	op   string
	mime string

	// text is what to copy, in place of the tool's input, when it's
	// set.
	text *string

	// files are what to copy, in place of the tool's input, when set.
	files []string

	// trim removes a trailing newline from what's copied.
	trim bool

	// noNewline leaves out the newline pasted text is ended with.
	noNewline bool
}

// unsupported returns the error for an option name doesn't support.
func unsupported(name, opt string) error {
	return fmt.Errorf("%s: %s isn't supported in asbx", name, opt)
}

// parseWlCopy parses wl-copy's arguments.
func parseWlCopy(args []string) (*action, error) {
	a := &action{op: opCopy}

	var words []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			words = append(words, args[i+1:]...)
			i = len(args)
		case arg == "-t" || arg == "--type":
			if i+1 == len(args) {
				return nil, fmt.Errorf("wl-copy: %s needs a type", arg)
			}

			i++
			a.mime = args[i]
		case strings.HasPrefix(arg, "--type="):
			a.mime = strings.TrimPrefix(arg, "--type=")
		case arg == "-n" || arg == "--trim-newline":
			a.trim = true
		case arg == "-c" || arg == "--clear":
			empty := ""
			a.text = &empty
		case arg == "-f" || arg == "--foreground" || arg == "-p" || arg == "--primary":
			// Nothing to do: the host's tool serves the clipboard.
		case strings.HasPrefix(arg, "-") && arg != "-":
			return nil, unsupported("wl-copy", arg)
		default:
			words = append(words, arg)
		}
	}

	if len(words) > 0 {
		text := strings.Join(words, " ")
		a.text = &text
	}

	return a, nil
}

// parseWlPaste parses wl-paste's arguments.
func parseWlPaste(args []string) (*action, error) {
	a := &action{op: opPaste}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-t" || arg == "--type":
			if i+1 == len(args) {
				return nil, fmt.Errorf("wl-paste: %s needs a type", arg)
			}

			i++
			a.mime = args[i]
		case strings.HasPrefix(arg, "--type="):
			a.mime = strings.TrimPrefix(arg, "--type=")
		case arg == "-l" || arg == "--list-types":
			a.op = opTypes
		case arg == "-n" || arg == "--no-newline":
			a.noNewline = true
		case arg == "-p" || arg == "--primary":
			// Nothing to do: the host's tool serves the clipboard.
		default:
			return nil, unsupported("wl-paste", arg)
		}
	}

	return a, nil
}

// parseXclip parses xclip's arguments. Its options have a single dash,
// and the ones it takes a value for are skipped over.
func parseXclip(args []string) (*action, error) {
	a := &action{op: opCopy}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-i", "-in":
			a.op = opCopy
		case "-o", "-out":
			a.op = opPaste
		case "-t", "-target":
			if i+1 == len(args) {
				return nil, fmt.Errorf("xclip: %s needs a target", arg)
			}

			i++
			a.mime = args[i]
		case "-selection", "-sel", "-display", "-d", "-loops", "-l":
			// The clipboard is all that's served, and the rest don't
			// apply.
			if i+1 == len(args) {
				return nil, fmt.Errorf("xclip: %s needs a value", arg)
			}

			i++
		case "-r", "-rmlastnl":
			a.trim = true
		case "-f", "-filter", "-q", "-quiet", "-s", "-silent", "-v", "-verbose", "-noutf8":
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				return nil, unsupported("xclip", arg)
			}

			a.files = append(a.files, arg)
		}
	}

	// TARGETS asks for the types, rather than data.
	if a.op == opPaste && a.mime == "TARGETS" {
		a.op = opTypes
		a.mime = ""
	}

	return a, nil
}

// parseXsel parses xsel's arguments, which are the same whether the
// primary or the clipboard selection is given.
func parseXsel(args []string) (*action, error) {
	a := &action{op: opPaste}
	for _, arg := range args {
		switch arg {
		case "-i", "--input":
			a.op = opCopy
		case "-o", "--output":
			a.op = opPaste
		case "-c", "--clear":
			a.op = opCopy
			empty := ""
			a.text = &empty
		case "-b", "--clipboard", "-p", "--primary", "-s", "--secondary", "-n", "--nodetach":
		default:
			return nil, unsupported("xsel", arg)
		}
	}

	return a, nil
}

// parseMac parses the arguments of pbcopy and pbpaste, which take none.
func parseMac(name string, args []string) (*action, error) {
	if len(args) > 0 {
		return nil, unsupported(name, args[0])
	}

	op := opCopy
	if name == "pbpaste" {
		op = opPaste
	}

	return &action{op: op}, nil
}

// parse returns what the tool name was asked to do by args.
func parse(name string, args []string) (*action, error) {
	switch name {
	case "wl-copy":
		return parseWlCopy(args)
	case "wl-paste":
		return parseWlPaste(args)
	case "xclip":
		return parseXclip(args)
	case "xsel":
		return parseXsel(args)
	case "pbcopy", "pbpaste":
		return parseMac(name, args)
	default:
		return nil, fmt.Errorf("asbx-clip doesn't stand in for %q", name)
	}
}

// readData returns the data a copy is for.
func readData(a *action, in io.Reader) ([]byte, error) {
	var data []byte
	switch {
	case a.text != nil:
		data = []byte(*a.text)
	case len(a.files) > 0:
		for _, name := range a.files {
			b, err := os.ReadFile(name) //nolint:gosec // Why: copying the file the user named is the point.
			if err != nil {
				return nil, err
			}

			data = append(data, b...)
		}
	default:
		// One more than allowed, so that too much is noticed.
		b, err := io.ReadAll(io.LimitReader(in, clipbridge.MaxData+1))
		if err != nil {
			return nil, err
		}

		data = b
	}

	if a.trim {
		data = []byte(strings.TrimSuffix(string(data), "\n"))
	}

	return data, nil
}

// Run runs the tool name with args, using the bridge at socket, which
// is empty when it isn't enabled.
func Run(ctx context.Context, name string, args []string, stdio IO, socket string) error {
	a, err := parse(name, args)
	if err != nil {
		return err
	}

	switch a.op {
	case opCopy:
		data, err := readData(a, stdio.In)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}

		if err := clipbridge.Copy(ctx, socket, a.mime, data); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	case opTypes:
		types, err := clipbridge.Types(ctx, socket)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}

		for _, t := range types {
			fmt.Fprintln(stdio.Out, t)
		}
	case opPaste:
		data, err := clipbridge.Paste(ctx, socket, a.mime)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}

		// Like wl-paste, text is ended with a newline unless it's told
		// not to. The other tools never add one.
		if name == "wl-paste" && !a.noNewline && (a.mime == "" || strings.HasPrefix(a.mime, "text/")) &&
			!strings.HasSuffix(string(data), "\n") {
			data = append(data, '\n')
		}

		if _, err := stdio.Out.Write(data); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	default:
		return errors.New("unreachable")
	}

	return nil
}
