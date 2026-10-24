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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"git.rgst.io/jaredallard/asbx/internal/onepassword"
	"go.rgst.io/jaredallard/cmdexec/v2"
)

// maxRequest is the longest request line the host accepts from the
// container, so it can't make the host buffer without limit.
const maxRequest = 64 << 10 // 64 KiB.

// retryInterval is how long Run waits between checking that the
// container is running and restarting the bridge.
const retryInterval = 250 * time.Millisecond

// ReadFunc reads the secret at an op:// ref.
type ReadFunc func(ctx context.Context, ref string) (string, error)

// answer returns the Response to req, reading it with read only if
// allow allows it.
func answer(ctx context.Context, req *Request, allow *Allowlist, read ReadFunc) Response {
	if !allow.Allows(req.Ref) {
		return Response{Error: fmt.Sprintf("%q isn't in OP_ALLOWED_SECRET_REFS", req.Ref)}
	}

	value, err := read(ctx, req.Ref)
	if err != nil {
		return Response{Error: err.Error()}
	}

	return Response{Value: value}
}

// Serve answers each [Request] read from r on w, in order, until r is
// closed. Only refs allow allows are read, with read.
func Serve(ctx context.Context, r io.Reader, w io.Writer, allow *Allowlist, read ReadFunc) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(nil, maxRequest)

	enc := json.NewEncoder(w)
	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			return fmt.Errorf("invalid request from the container: %w", err)
		}

		resp := answer(ctx, &req, allow, read)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}

	return scanner.Err()
}

// running reports whether container has started and not yet stopped.
func running(ctx context.Context, container string) bool {
	out, err := cmdexec.CommandContext(ctx, "docker", "container", "inspect", "--format", "{{.State.Running}}", container).Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// serveExec starts the bridge in container and serves it until it
// exits. Its stdin and stdout are OS pipes, rather than handed to
// cmdexec as readers and writers, so that it exiting closes them
// without waiting on the other end.
func serveExec(ctx context.Context, container string, allow *Allowlist) error {
	requests, requestsW, err := os.Pipe()
	if err != nil {
		return err
	}
	defer requests.Close()

	responsesR, responses, err := os.Pipe()
	if err != nil {
		return err
	}
	defer responses.Close()

	// Serve failing stops the bridge, for Run to restart it.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// stderr isn't set, so the bridge can't write over the agent.
	cmd := cmdexec.CommandContext(ctx, "docker", "exec", "--interactive", container, "asbx-op", "bridge")
	cmd.SetStdin(responsesR)
	cmd.SetStdout(requestsW)

	served := make(chan error, 1)
	go func() {
		err := Serve(ctx, requests, responses, allow, onepassword.ReadNonInteractive)
		cancel()
		served <- err
	}()

	err = cmd.Run()

	requestsW.Close()
	responsesR.Close()

	return errors.Join(err, <-served)
}

// Run serves the bridge in container until ctx is canceled. It waits
// for container to start and restarts the bridge if it exits.
func Run(ctx context.Context, container string, allow *Allowlist) {
	for {
		// Errors are retried rather than reported, since writing to the
		// terminal at this point would write bad output. Failure here is
		// reported by the shim.
		if running(ctx, container) {
			_ = serveExec(ctx, container, allow) //nolint:errcheck // Why: see above.
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(retryInterval):
		}
	}
}
