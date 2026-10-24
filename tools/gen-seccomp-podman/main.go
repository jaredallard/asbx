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

// Package main implements gen-seccomp-podman. It generates the seccomp
// profile the sandbox runs with when Podman is enabled, from moby's
// default profile, and writes it to the path it's given.
//
// Rootless Podman creates user namespaces and mounts in them, which
// moby's profile only allows with CAP_SYS_ADMIN. The sandbox doesn't
// have it, and only root in a user namespace Podman creates does, so
// those syscalls are allowed without it.
//
// That also lets the agent reach the kernel's netfilter code as root in
// a user namespace, which is where many container escapes have come
// from, so NETLINK_NETFILTER sockets are blocked. pasta, Podman's
// rootless networking, only needs rtnetlink; only Podman's bridge
// networks need netfilter.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"time"
)

const (
	// version is the moby/profiles tag the profile is generated from.
	version = "seccomp/v0.2.4"

	// commit pins the above version
	commit = "245180c51918481c0525424b3ee025d2b435d46c"

	// source is moby's default profile at commit.
	source = "https://raw.githubusercontent.com/moby/profiles/" + commit + "/seccomp/default.json"
)

// allowed are the syscalls Podman needs that moby's profile only allows
// with CAP_SYS_ADMIN, or CAP_SYS_CHROOT for chroot, which is how
// containers/storage unpacks image layers.
var allowed = []string{
	"chroot",
	"clone",
	"clone3",
	"fsconfig",
	"fsmount",
	"fsopen",
	"fspick",
	"mount",
	"mount_setattr",
	"move_mount",
	"open_tree",
	"pivot_root",
	"setdomainname",
	"sethostname",
	"setns",
	"umount",
	"umount2",
	"unshare",
}

const (
	// afNetlink is AF_NETLINK, socket's first argument.
	afNetlink = 16

	// netlinkNetfilter is NETLINK_NETFILTER, socket's third argument for
	// AF_NETLINK.
	netlinkNetfilter = 12
)

// rule is a rule in a seccomp profile.
type rule struct {
	Names    []string `json:"names"`
	Action   string   `json:"action"`
	Args     []arg    `json:"args"`
	Excludes *struct {
		Caps []string `json:"caps"`
	} `json:"excludes"`
}

// arg is a comparison of a syscall's argument in a rule.
type arg struct {
	Index uint   `json:"index"`
	Value uint64 `json:"value"`
	Op    string `json:"op"`
}

// socketRule returns a rule that allows socket when every one of args
// matches.
func socketRule(args ...arg) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"names":  []string{"socket"},
		"action": "SCMP_ACT_ALLOW",
		"args":   args,
	})
}

// restrictNetfilter replaces the rule in rules that allows socket for
// every address family below some limit with rules that allow the same
// families, except AF_NETLINK with NETLINK_NETFILTER. A rule can't
// compare an argument twice, so each family above AF_NETLINK gets its
// own.
func restrictNetfilter(rules []json.RawMessage) ([]json.RawMessage, error) {
	i := slices.IndexFunc(rules, func(raw json.RawMessage) bool {
		var r rule
		if err := json.Unmarshal(raw, &r); err != nil {
			return false
		}

		return slices.Equal(r.Names, []string{"socket"}) && r.Action == "SCMP_ACT_ALLOW" &&
			len(r.Args) == 1 && r.Args[0].Index == 0 && r.Args[0].Op == "SCMP_CMP_LT" && r.Args[0].Value > afNetlink
	})
	if i == -1 {
		return nil, fmt.Errorf("no rule allows socket for AF_NETLINK and the families below it")
	}

	var r rule
	if err := json.Unmarshal(rules[i], &r); err != nil {
		return nil, err
	}

	below, err := socketRule(arg{Index: 0, Value: afNetlink, Op: "SCMP_CMP_LT"})
	if err != nil {
		return nil, err
	}

	netlink, err := socketRule(
		arg{Index: 0, Value: afNetlink, Op: "SCMP_CMP_EQ"},
		arg{Index: 2, Value: netlinkNetfilter, Op: "SCMP_CMP_NE"},
	)
	if err != nil {
		return nil, err
	}

	replacement := []json.RawMessage{below, netlink}
	for family := uint64(afNetlink + 1); family < r.Args[0].Value; family++ {
		above, err := socketRule(arg{Index: 0, Value: family, Op: "SCMP_CMP_EQ"})
		if err != nil {
			return nil, err
		}
		replacement = append(replacement, above)
	}

	return slices.Replace(rules, i, i+1, replacement...), nil
}

// generate returns moby's profile, def, with allowed allowed and
// NETLINK_NETFILTER sockets blocked. The rules that limit clone's
// namespace flags, or fail clone3, without CAP_SYS_ADMIN are removed,
// since they'd conflict.
func generate(def []byte) ([]byte, error) {
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(def, &profile); err != nil {
		return nil, err
	}

	var rules []json.RawMessage
	if err := json.Unmarshal(profile["syscalls"], &rules); err != nil {
		return nil, err
	}

	rules = slices.DeleteFunc(rules, func(raw json.RawMessage) bool {
		var r rule
		if err := json.Unmarshal(raw, &r); err != nil || r.Excludes == nil {
			return false
		}

		isClone := slices.Contains(r.Names, "clone") || slices.Contains(r.Names, "clone3")
		return isClone && slices.Contains(r.Excludes.Caps, "CAP_SYS_ADMIN")
	})

	rules, err := restrictNetfilter(rules)
	if err != nil {
		return nil, err
	}

	asbx, err := json.Marshal(map[string]any{
		"names":   allowed,
		"action":  "SCMP_ACT_ALLOW",
		"comment": "asbx: rootless podman needs these without CAP_SYS_ADMIN or CAP_SYS_CHROOT",
	})
	if err != nil {
		return nil, err
	}

	syscalls, err := json.Marshal(append(rules, asbx))
	if err != nil {
		return nil, err
	}
	profile["syscalls"] = syscalls

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "\t")
	if err := enc.Encode(profile); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// fetch returns moby's default profile.
func fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // Why: read-only.

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch %s: %s", source, resp.Status)
	}

	return io.ReadAll(resp.Body)
}

// run writes the generated profile to out.
func run(ctx context.Context, out string) error {
	def, err := fetch(ctx)
	if err != nil {
		return err
	}

	profile, err := generate(def)
	if err != nil {
		return fmt.Errorf("failed to generate from moby/profiles %s: %w", version, err)
	}

	return os.WriteFile(out, profile, 0o644) //nolint:gosec // Why: a checked in source file.
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: gen-seccomp-podman <output path>")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	err := run(ctx, os.Args[1])
	cancel()

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
