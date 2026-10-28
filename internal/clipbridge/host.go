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
	"context"
	"errors"
	"fmt"
	"net/rpc"

	"git.rgst.io/jaredallard/asbx/internal/config"
	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
)

// serviceName is what [Service] is registered as.
const serviceName = "Clipboard"

// maxRequest is the most the host reads from a client, which fits
// [MaxData] and the rest of its call.
const maxRequest = MaxData + 1<<20 // 9 MiB.

// Service is what the container calls on the host: its clipboard, as
// far as its mode allows.
type Service struct {
	ctx  context.Context //nolint:containedctx // Why: net/rpc methods have no context of their own.
	mode string
	cb   Clipboard
}

// NewService returns the [Service] that uses cb, doing what mode
// allows, until ctx is canceled.
func NewService(ctx context.Context, mode string, cb Clipboard) *Service {
	return &Service{ctx: ctx, mode: mode, cb: cb}
}

// mimeOf returns mime, or [TextType] when it's empty.
func mimeOf(mime string) string {
	if mime == "" {
		return TextType
	}

	return mime
}

// canRead returns an error unless the container may read the host's
// clipboard, which may hold secrets.
func (s *Service) canRead() error {
	if s.mode != config.ClipboardCopyPaste {
		return errors.New("reading the host's clipboard isn't allowed, set clipboard to copy_paste in asbx's configuration")
	}

	return nil
}

// Copy sets the clipboard to args.Data.
func (s *Service) Copy(args *CopyArgs, _ *stdiobridge.Empty) error {
	if s.mode != config.ClipboardCopy && s.mode != config.ClipboardCopyPaste {
		return errors.New("the clipboard isn't enabled, set clipboard in asbx's configuration")
	}

	if len(args.Data) > MaxData {
		return fmt.Errorf("refusing to copy more than %d MiB", MaxData>>20)
	}

	return s.cb.Copy(s.ctx, mimeOf(args.Type), args.Data)
}

// Paste sets reply to the clipboard, if the container may read it.
func (s *Service) Paste(args *PasteArgs, reply *[]byte) error {
	if err := s.canRead(); err != nil {
		return err
	}

	data, err := s.cb.Paste(s.ctx, mimeOf(args.Type))
	if err != nil {
		return err
	}

	if len(data) > MaxData {
		return fmt.Errorf("the clipboard holds more than %d MiB", MaxData>>20)
	}

	*reply = data

	return nil
}

// Types sets reply to the types the clipboard holds, if the container
// may read it.
func (s *Service) Types(_ *stdiobridge.Empty, reply *[]string) error {
	if err := s.canRead(); err != nil {
		return err
	}

	types, err := s.cb.Types(s.ctx)
	if err != nil {
		return err
	}

	*reply = types

	return nil
}

// Run serves the bridge in container until ctx is canceled, with cb,
// doing what mode allows.
func Run(ctx context.Context, container, mode string, cb Clipboard) {
	stdiobridge.Run(ctx, container, []string{"asbx-clip", "bridge"}, maxRequest, func(srv *rpc.Server) error {
		return srv.RegisterName(serviceName, NewService(ctx, mode, cb))
	})
}
