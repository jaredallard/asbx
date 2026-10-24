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

package opshim

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"git.rgst.io/jaredallard/asbx/internal/onepassword"
	"github.com/dustin/go-humanize"
)

// ListItemsFunc lists the items with a field the container may read,
// only from the vault with the name or ID vault, if it's set.
type ListItemsFunc func(ctx context.Context, vault string) ([]onepassword.Item, error)

// ListOptions are the options to [ListItems].
type ListOptions struct {
	// Vault, when set, only lists the items in the vault with this
	// name or ID.
	Vault string

	// Format is how the items are written, human (the default) or
	// json, as op item list writes them.
	Format string
}

// writeTable writes items as a table, as op item list does.
func writeTable(w io.Writer, items []onepassword.Item) error {
	tw := tabwriter.NewWriter(w, 0, 0, 4, ' ', 0)
	fmt.Fprintln(tw, "ID\tTITLE\tVAULT\tEDITED")
	for i := range items {
		item := &items[i]
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", item.ID, item.Title, item.Vault.Name, humanize.Time(item.UpdatedAt))
	}

	return tw.Flush()
}

// writeJSON writes items as a JSON array, as op item list does.
func writeJSON(w io.Writer, items []onepassword.Item) error {
	// An empty list is [], not null.
	if items == nil {
		items = []onepassword.Item{}
	}

	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}

	_, err = w.Write(append(b, '\n'))
	return err
}

// ListItems writes the items list lists to w, as op item list does.
func ListItems(ctx context.Context, list ListItemsFunc, w io.Writer, opts ListOptions) error {
	if opts.Format != "" && opts.Format != "human" && opts.Format != "json" {
		return fmt.Errorf("invalid --format %q, expected human or json", opts.Format)
	}

	items, err := list(ctx, opts.Vault)
	if err != nil {
		return err
	}

	if opts.Format == "json" {
		return writeJSON(w, items)
	}

	return writeTable(w, items)
}
