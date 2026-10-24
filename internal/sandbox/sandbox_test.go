package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/config"
	"git.rgst.io/jaredallard/asbx/internal/docker"
	"go.rgst.io/jaredallard/cmdexec/v2"
	"gotest.tools/v3/assert"
)

// fakeAgent is an Agent that adds a marker argument, or fails with err.
type fakeAgent struct {
	err error
}

// Configure adds --fake-agent to run, or returns a.err.
func (a *fakeAgent) Configure(_ context.Context, _ *Host, run *docker.Run) error {
	if a.err != nil {
		return a.err
	}

	run.Arg("--fake-agent")

	return nil
}

// mkfile creates path under dir, as a directory if isDir.
func mkfile(dir, path string, isDir bool) error {
	path = filepath.Join(dir, path)
	if isDir {
		return os.MkdirAll(path, 0o750)
	}

	return os.WriteFile(path, nil, 0o600)
}

// newTestHost returns a Host with an empty configuration in temporary
// directories.
func newTestHost(t *testing.T) *Host {
	t.Helper()

	// Keeps the result independent of the host's ssh-agent.
	t.Setenv("SSH_AUTH_SOCK", "")

	dir := t.TempDir()
	return &Host{
		Home:      t.TempDir(),
		PWD:       dir,
		WorkDir:   dir,
		ConfigDir: t.TempDir(),
		Config:    &config.Config{},
	}
}

// TestBuildAgentOrder verifies that the agent's arguments come after
// the generic ones and before the working directory is mounted.
func TestBuildAgentOrder(t *testing.T) {
	host := newTestHost(t)

	run, err := Build(t.Context(), host, &fakeAgent{})
	assert.NilError(t, err)

	args := run.Args()
	network := slices.Index(args, "host.docker.internal:host-gateway")
	agent := slices.Index(args, "--fake-agent")
	workDir := slices.Index(args, host.PWD+":"+host.PWD)
	assert.Assert(t, network != -1 && agent != -1 && workDir != -1, "Build() args = %q", args)
	assert.Assert(t, network < agent && agent < workDir, "Build() args = %q, want agent args between network and working directory", args)
}

// TestBuildAgentError verifies that an agent that fails to configure
// fails the build.
func TestBuildAgentError(t *testing.T) {
	want := errors.New("agent failed")

	_, err := Build(t.Context(), newTestHost(t), &fakeAgent{err: want})
	assert.ErrorIs(t, err, want)
}

// TestBuildProtectsRepository verifies that files in the working
// directory that could run code on the host are mounted read-only, and
// that the hooks directory is created so it's protected too.
func TestBuildProtectsRepository(t *testing.T) {
	host := newTestHost(t)
	for _, path := range []string{".git", projectConfigFile} {
		assert.NilError(t, mkfile(host.WorkDir, path, path == ".git"))
	}
	assert.NilError(t, mkfile(host.WorkDir, ".git/config", false))

	run, err := Build(t.Context(), host, &fakeAgent{})
	assert.NilError(t, err)

	args := run.Args()
	for _, path := range []string{projectConfigFile, ".git/config", ".git/hooks"} {
		mount := "type=bind,source=" + host.WorkDir + "/" + path + ",target=" + host.PWD + "/" + path + ",readonly"
		assert.Assert(t, slices.Contains(args, mount), "Build() args = %q, want %q", args, mount)
	}
}

// TestBuildNamesContainer verifies that each container gets its own
// name, for the op bridge to find it by.
func TestBuildNamesContainer(t *testing.T) {
	host := newTestHost(t)

	first, err := Build(t.Context(), host, &fakeAgent{})
	assert.NilError(t, err)

	second, err := Build(t.Context(), host, &fakeAgent{})
	assert.NilError(t, err)

	assert.Assert(t, first.Name() != "", "Build() didn't name the container")
	assert.Assert(t, first.Name() != second.Name(), "Build() named two containers %q", first.Name())
	assert.Assert(t, slices.Contains(first.Args(), first.Name()), "Build() args = %q, want --name %q", first.Args(), first.Name())
}

// TestBuildOPBridge verifies that the container is told where the op
// bridge is only when there are refs it may read.
func TestBuildOPBridge(t *testing.T) {
	mock := cmdexec.NewMockExecutor()
	mock.AddLookPath("op", "/usr/bin/op")
	cmdexec.UseMockExecutor(t, mock)

	host := newTestHost(t)
	socketEnv := "ASBX_OP_SOCKET=/run/asbx/op.sock"

	run, err := Build(t.Context(), host, &fakeAgent{})
	assert.NilError(t, err)
	assert.Assert(t, !slices.Contains(run.Args(), socketEnv), "Build() args = %q, want no %q", run.Args(), socketEnv)

	host.Config.OPAllowedSecretRefs = []string{"op://a/b/c"}
	run, err = Build(t.Context(), host, &fakeAgent{})
	assert.NilError(t, err)
	assert.Assert(t, slices.Contains(run.Args(), socketEnv), "Build() args = %q, want %q", run.Args(), socketEnv)
}

// TestBuildOPBridgeWithoutOP verifies that asbx refuses to run when
// refs are allowed but there's no op on the host to read them with.
func TestBuildOPBridgeWithoutOP(t *testing.T) {
	mock := cmdexec.NewMockExecutor()
	mock.AddLookPathError("op", errors.New("executable file not found in $PATH"))
	cmdexec.UseMockExecutor(t, mock)

	host := newTestHost(t)
	host.Config.OPAllowedSecretRefs = []string{"op://a/b/c"}

	_, err := Build(t.Context(), host, &fakeAgent{})
	assert.ErrorContains(t, err, "op isn't installed on this host")
}

// TestBuildOPBridgeInvalidRef verifies that an allowlist entry that
// isn't an op:// ref fails the build.
func TestBuildOPBridgeInvalidRef(t *testing.T) {
	host := newTestHost(t)
	host.Config.OPAllowedSecretRefs = []string{"Private/bank/password"}

	_, err := Build(t.Context(), host, &fakeAgent{})
	assert.ErrorContains(t, err, "must be an op:// ref")
}

// fjConfigPath points the user config directory at a temporary one and
// returns where fj's configuration is in it.
func fjConfigPath(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir() = %v", err)
	}

	return filepath.Join(configDir, "fj", "config.json")
}

// TestForgejo verifies that fj's token and configuration are passed in
// when fj is logged in.
func TestForgejo(t *testing.T) {
	path := fjConfigPath(t)
	assert.NilError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	assert.NilError(t, os.WriteFile(path, []byte("{}"), 0o600))

	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name:   "fj",
		Args:   []string{"auth", "token"},
		Stdout: []byte("hunter2\n"),
	}))

	run := &docker.Run{}
	assert.NilError(t, forgejo(t.Context(), run))
	assert.DeepEqual(t, run.Args(), []string{
		"--env", "FORGEJO_TOKEN",
		"--mount", "type=bind,source=" + path + ",target=" + ReadOnlyHostDir + "/fj-config.json,readonly",
	})
}

// TestForgejoWithoutConfig verifies that Forgejo is skipped, without an
// error, when fj has no configuration.
func TestForgejoWithoutConfig(t *testing.T) {
	fjConfigPath(t)
	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor())

	run := &docker.Run{}
	assert.NilError(t, forgejo(t.Context(), run))
	assert.Equal(t, len(run.Args()), 0)
}

// TestForgejoNotLoggedIn verifies that Forgejo is skipped, without an
// error, when fj can't print a token.
func TestForgejoNotLoggedIn(t *testing.T) {
	path := fjConfigPath(t)
	assert.NilError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	assert.NilError(t, os.WriteFile(path, []byte("{}"), 0o600))

	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name: "fj",
		Args: []string{"auth", "token"},
		Err:  errors.New("exit status 1"),
	}))

	run := &docker.Run{}
	assert.NilError(t, forgejo(t.Context(), run))
	assert.Equal(t, len(run.Args()), 0)
}

// TestGitHub verifies that gh's token is passed in as GH_TOKEN when gh
// is logged in.
func TestGitHub(t *testing.T) {
	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name:   "gh",
		Args:   []string{"auth", "token"},
		Stdout: []byte("hunter2\n"),
	}))

	run := &docker.Run{}
	github(t.Context(), run)
	assert.DeepEqual(t, run.Args(), []string{"--env", "GH_TOKEN"})
}

// TestGitHubNotLoggedIn verifies that GitHub is skipped when gh can't
// print a token.
func TestGitHubNotLoggedIn(t *testing.T) {
	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name: "gh",
		Args: []string{"auth", "token"},
		Err:  errors.New("exit status 1"),
	}))

	run := &docker.Run{}
	github(t.Context(), run)
	assert.Equal(t, len(run.Args()), 0)
}
