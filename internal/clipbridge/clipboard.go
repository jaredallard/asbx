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

package clipbridge

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.rgst.io/jaredallard/cmdexec/v2"
)

// ErrNoTool is returned by [Detect] when the host has no clipboard
// tool.
var ErrNoTool = errors.New("no clipboard tool found on this host, install pbcopy, wl-clipboard, xclip or xsel")

// Clipboard is the host's clipboard, which [Serve] answers requests
// with.
type Clipboard interface {
	// Copy sets the clipboard to data, of the MIME type mime.
	Copy(ctx context.Context, mime string, data []byte) error

	// Paste returns the clipboard as the MIME type mime.
	Paste(ctx context.Context, mime string) ([]byte, error)

	// Types returns the MIME types the clipboard holds.
	Types(ctx context.Context) ([]string, error)
}

// output returns what name run with args writes to stdout.
func output(ctx context.Context, name string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer

	cmd := cmdexec.CommandContext(ctx, name, args...)
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%s: %w: %s", name, err, msg)
		}

		return nil, fmt.Errorf("%s: %w", name, err)
	}

	return stdout.Bytes(), nil
}

// feed runs name with args and data as its stdin, without waiting on
// its output. The tools that serve a selection, such as xclip and
// wl-copy, fork into the background holding what they were given
// open, which would hang waiting for them to close it.
func feed(ctx context.Context, data []byte, name string, args ...string) error {
	cmd := cmdexec.CommandContext(ctx, name, args...)
	cmd.SetStdin(bytes.NewReader(data))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	return nil
}

// textOnly returns an error unless mime is text.
func textOnly(tool, mime string) error {
	if mime != TextType {
		return fmt.Errorf("%s only supports %s, not %s", tool, TextType, mime)
	}

	return nil
}

// wayland is the clipboard of wl-clipboard.
type wayland struct{}

// Copy implements [Clipboard].
func (wayland) Copy(ctx context.Context, mime string, data []byte) error {
	return feed(ctx, data, "wl-copy", "--type", mime)
}

// Paste implements [Clipboard].
func (wayland) Paste(ctx context.Context, mime string) ([]byte, error) {
	return output(ctx, "wl-paste", "--no-newline", "--type", mime)
}

// Types implements [Clipboard].
func (wayland) Types(ctx context.Context) ([]string, error) {
	out, err := output(ctx, "wl-paste", "--list-types")
	if err != nil {
		return nil, err
	}

	return strings.Fields(string(out)), nil
}

// xclip is the clipboard of xclip.
type xclip struct{}

// Copy implements [Clipboard].
func (xclip) Copy(ctx context.Context, mime string, data []byte) error {
	return feed(ctx, data, "xclip", "-selection", "clipboard", "-in", "-target", mime)
}

// Paste implements [Clipboard].
func (xclip) Paste(ctx context.Context, mime string) ([]byte, error) {
	return output(ctx, "xclip", "-selection", "clipboard", "-out", "-target", mime)
}

// Types implements [Clipboard].
func (xclip) Types(ctx context.Context) ([]string, error) {
	out, err := output(ctx, "xclip", "-selection", "clipboard", "-out", "-target", "TARGETS")
	if err != nil {
		return nil, err
	}

	return strings.Fields(string(out)), nil
}

// xsel is the clipboard of xsel, which only holds text.
type xsel struct{}

// Copy implements [Clipboard].
func (xsel) Copy(ctx context.Context, mime string, data []byte) error {
	if err := textOnly("xsel", mime); err != nil {
		return err
	}

	return feed(ctx, data, "xsel", "--clipboard", "--input")
}

// Paste implements [Clipboard].
func (xsel) Paste(ctx context.Context, mime string) ([]byte, error) {
	if err := textOnly("xsel", mime); err != nil {
		return nil, err
	}

	return output(ctx, "xsel", "--clipboard", "--output")
}

// Types implements [Clipboard].
func (xsel) Types(context.Context) ([]string, error) {
	return []string{TextType}, nil
}

// macOS is the clipboard of macOS, through pbcopy and pbpaste for text
// and osascript for PNG images.
type macOS struct{}

// pngData matches osascript's hex dump of a PNG on the clipboard.
var pngData = regexp.MustCompile(`^«data PNGf([0-9A-Fa-f]+)»\s*$`)

// Copy implements [Clipboard].
func (macOS) Copy(ctx context.Context, mime string, data []byte) error {
	if err := textOnly("pbcopy", mime); err != nil {
		return err
	}

	return feed(ctx, data, "pbcopy")
}

// Paste implements [Clipboard].
func (macOS) Paste(ctx context.Context, mime string) ([]byte, error) {
	if mime == TextType {
		return output(ctx, "pbpaste")
	}

	if mime != "image/png" {
		return nil, fmt.Errorf("the macOS clipboard only supports %s and image/png, not %s", TextType, mime)
	}

	out, err := output(ctx, "osascript", "-e", "the clipboard as «class PNGf»")
	if err != nil {
		return nil, err
	}

	m := pngData.FindSubmatch(out)
	if m == nil {
		return nil, errors.New("the clipboard doesn't hold a PNG")
	}

	return hex.DecodeString(string(m[1]))
}

// Types implements [Clipboard].
func (macOS) Types(ctx context.Context) ([]string, error) {
	out, err := output(ctx, "osascript", "-e", "clipboard info")
	if err != nil {
		return nil, err
	}

	info := string(out)

	var types []string
	if strings.Contains(info, "PNGf") {
		types = append(types, "image/png")
	}

	if strings.Contains(info, "utf8") || strings.Contains(info, "string") || strings.Contains(info, "Unicode text") {
		types = append(types, TextType)
	}

	return types, nil
}

// Detect returns the clipboard of the tools installed on the host, or
// [ErrNoTool].
func Detect(goos string) (Clipboard, error) {
	has := func(name string) bool {
		_, err := cmdexec.LookPath(name)
		return err == nil
	}

	switch {
	case goos == "darwin" && has("pbcopy") && has("pbpaste"):
		return macOS{}, nil
	case has("wl-copy") && has("wl-paste"):
		return wayland{}, nil
	case has("xclip"):
		return xclip{}, nil
	case has("xsel"):
		return xsel{}, nil
	default:
		return nil, ErrNoTool
	}
}
