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

package urlbridge

import (
	"context"
	"crypto/tls"
	"net/rpc"

	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
)

// serviceName is what [Service] is registered as.
const serviceName = "URL"

// maxRequest is the most the host reads from a client, which fits
// [MaxURL] and the rest of its call.
const maxRequest = MaxURL + 4<<10

// Service is what the container calls on the host: its browser.
type Service struct {
	ctx    context.Context //nolint:containedctx // Why: net/rpc methods have no context of their own.
	opener Opener
}

// NewService returns the [Service] that opens URLs with opener, until
// ctx is canceled.
func NewService(ctx context.Context, opener Opener) *Service {
	return &Service{ctx: ctx, opener: opener}
}

// Open opens args.URL, if it's one that may be opened. It's checked
// here too, since the container can't be trusted to have.
func (s *Service) Open(args *OpenArgs, _ *stdiobridge.Empty) error {
	if err := Validate(args.URL); err != nil {
		return err
	}

	return s.opener.Open(s.ctx, args.URL)
}

// Run serves the bridge in container until ctx is canceled, opening
// URLs with opener. The container's calls are inside TLS, as tlsConfig
// describes it.
func Run(ctx context.Context, container string, tlsConfig *tls.Config, opener Opener) {
	stdiobridge.Run(ctx, container, []string{"asbx-open", "bridge"}, tlsConfig, maxRequest, func(srv *rpc.Server) error {
		return srv.RegisterName(serviceName, NewService(ctx, opener))
	})
}
