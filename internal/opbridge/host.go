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

package opbridge

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/rpc"

	"git.rgst.io/jaredallard/asbx/internal/onepassword"
	"git.rgst.io/jaredallard/asbx/internal/stdiobridge"
)

// serviceName is what [Service] is registered as.
const serviceName = "OP"

// maxRequest is the most the host reads from a client, so it can't
// make the host buffer without limit.
const maxRequest = 64 << 10 // 64 KiB.

// OP is the host's op, which [Service] answers requests with.
type OP interface {
	// Read returns the secret at an op:// ref.
	Read(ctx context.Context, ref string) (string, error)

	// ListItems returns every item, except archived ones.
	ListItems(ctx context.Context) ([]onepassword.Item, error)

	// FieldRefs returns the op:// refs of each of items' fields, by
	// item ID.
	FieldRefs(ctx context.Context, items []onepassword.Item) (map[string][]string, error)
}

// hostOP is the [OP] that runs op on the host, without prompting.
type hostOP struct{}

// Read implements [OP].
func (hostOP) Read(ctx context.Context, ref string) (string, error) {
	return onepassword.ReadNonInteractive(ctx, ref)
}

// ListItems implements [OP].
func (hostOP) ListItems(ctx context.Context) ([]onepassword.Item, error) {
	return onepassword.ListItemsNonInteractive(ctx)
}

// FieldRefs implements [OP].
func (hostOP) FieldRefs(ctx context.Context, items []onepassword.Item) (map[string][]string, error) {
	return onepassword.FieldRefsNonInteractive(ctx, items)
}

// Service is what the container calls on the host: op, which only reads
// the refs its allowlist allows and only lists items with a field it
// allows.
type Service struct {
	ctx   context.Context //nolint:containedctx // Why: net/rpc methods have no context of their own.
	allow *Allowlist
	op    OP
}

// NewService returns the [Service] that answers with op, as limited by
// allow, until ctx is canceled.
func NewService(ctx context.Context, allow *Allowlist, op OP) *Service {
	return &Service{ctx: ctx, allow: allow, op: op}
}

// Read sets reply to the secret at args.Ref, if it's allowed.
func (s *Service) Read(args *ReadArgs, reply *string) error {
	if !s.allow.Allows(args.Ref) {
		return fmt.Errorf("%q isn't in op_allowed_secret_refs", args.Ref)
	}

	value, err := s.op.Read(s.ctx, args.Ref)
	if err != nil {
		return err
	}

	*reply = value

	return nil
}

// ListItems sets reply to the items with a field that's allowed.
func (s *Service) ListItems(args *ListArgs, reply *[]onepassword.Item) error {
	items, err := allowedItems(s.ctx, s.op, s.allow, args.Vault)
	if err != nil {
		return err
	}

	*reply = items

	return nil
}

// Run serves the bridge in container until ctx is canceled, with the
// host's op, which only reads refs allow allows and only lists items
// with a field it allows. The container's calls are inside TLS, as
// tlsConfig describes it.
func Run(ctx context.Context, container string, tlsConfig *tls.Config, allow *Allowlist) {
	stdiobridge.Run(ctx, container, []string{"asbx-op", "bridge"}, tlsConfig, maxRequest, func(srv *rpc.Server) error {
		return srv.RegisterName(serviceName, NewService(ctx, allow, hostOP{}))
	})
}
