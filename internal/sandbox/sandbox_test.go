package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/config"
	"git.rgst.io/jaredallard/asbx/internal/docker"
	"go.rgst.io/jaredallard/cmdexec/v2"
	"gotest.tools/v3/assert"
)

// fakeAgent is an Agent that adds a marker argument, or fails with err.
type fakeAgent struct {
	err error

	// paths are its protected paths.
	paths []string

	// secrets are its secrets.
	secrets []Secret
}

// Name returns "fake".
func (a *fakeAgent) Name() string {
	return "fake"
}

// ProtectedPaths returns a.paths.
func (a *fakeAgent) ProtectedPaths() []string {
	return a.paths
}

// Configure adds --fake-agent to run, or returns a.err.
func (a *fakeAgent) Configure(_ context.Context, _ *Host, run *docker.Run) error {
	if a.err != nil {
		return a.err
	}

	run.Arg("--fake-agent")

	return nil
}

// Secrets returns a.secrets.
func (a *fakeAgent) Secrets(*Host) []Secret {
	return a.secrets
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

	run, err := Build(t.Context(), host, &fakeAgent{}, nil)
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

	_, err := Build(t.Context(), newTestHost(t), &fakeAgent{err: want}, nil)
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

	run, err := Build(t.Context(), host, &fakeAgent{}, nil)
	assert.NilError(t, err)

	args := run.Args()
	for _, path := range []string{projectConfigFile, ".git/config", ".git/hooks"} {
		mount := "type=bind,source=" + host.WorkDir + "/" + path + ",target=" + host.PWD + "/" + path + ",readonly"
		assert.Assert(t, slices.Contains(args, mount), "Build() args = %q, want %q", args, mount)
	}
}

// TestBuildProtectsKnownAgents verifies that the files every known
// agent runs code from on the host are mounted read-only, not only the
// running agent's, since the host may run any of them afterwards. Paths
// shared by agents are mounted once, and ones that don't exist aren't.
func TestBuildProtectsKnownAgents(t *testing.T) {
	host := newTestHost(t)
	for _, path := range []string{".fake", ".other"} {
		assert.NilError(t, mkfile(host.WorkDir, path, true))
	}
	for _, path := range []string{".fake/settings", ".other/hooks", ".shared"} {
		assert.NilError(t, mkfile(host.WorkDir, path, false))
	}

	running := &fakeAgent{paths: []string{".fake/settings", ".shared"}}
	other := &fakeAgent{paths: []string{".other/hooks", ".shared", ".missing"}}

	run, err := Build(t.Context(), host, running, []Agent{running, other})
	assert.NilError(t, err)

	args := run.Args()
	for _, path := range []string{".fake/settings", ".other/hooks", ".shared"} {
		mount := "type=bind,source=" + host.WorkDir + "/" + path + ",target=" + host.PWD + "/" + path + ",readonly"
		count := 0
		for _, arg := range args {
			if arg == mount {
				count++
			}
		}

		assert.Equal(t, count, 1, "Build() args = %q, want %q once", args, mount)
	}

	for _, arg := range args {
		assert.Assert(t, !strings.Contains(arg, ".missing"), "Build() args = %q, want no mount of .missing", args)
	}
}

// TestBuildNamesContainer verifies that each container gets its own
// name, for the op bridge to find it by.
func TestBuildNamesContainer(t *testing.T) {
	host := newTestHost(t)

	first, err := Build(t.Context(), host, &fakeAgent{}, nil)
	assert.NilError(t, err)

	second, err := Build(t.Context(), host, &fakeAgent{}, nil)
	assert.NilError(t, err)

	assert.Assert(t, first.Name() != "", "Build() didn't name the container")
	assert.Assert(t, first.Name() != second.Name(), "Build() named two containers %q", first.Name())
	assert.Assert(t, slices.Contains(first.Args(), first.Name()), "Build() args = %q, want --name %q", first.Args(), first.Name())
}

// TestSSHAgentMissing verifies that the user is warned, rather than
// the run failing, when there's no ssh-agent to forward.
func TestSSHAgentMissing(t *testing.T) {
	host := newTestHost(t)

	run := &docker.Run{}
	sshAgent(host, run, "linux")

	assert.Equal(t, len(run.Args()), 0)
	assert.Equal(t, len(host.notices), 1)
	assert.Assert(t, host.notices[0].warn, "sshAgent() notice = %+v, want a warning", host.notices[0])
}

// TestBuildOPBridge verifies that the container is told where the op
// bridge is only when there are refs it may read.
func TestBuildOPBridge(t *testing.T) {
	mock := cmdexec.NewMockExecutor()
	mock.AddLookPath("op", "/usr/bin/op")
	cmdexec.UseMockExecutor(t, mock)

	host := newTestHost(t)
	socketEnv := "ASBX_OP_SOCKET=/run/asbx/op.sock"

	run, err := Build(t.Context(), host, &fakeAgent{}, nil)
	assert.NilError(t, err)
	assert.Assert(t, !slices.Contains(run.Args(), socketEnv), "Build() args = %q, want no %q", run.Args(), socketEnv)

	host.Config.OPAllowedSecretRefs = []string{"op://a/b/c"}
	run, err = Build(t.Context(), host, &fakeAgent{}, nil)
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

	_, err := Build(t.Context(), host, &fakeAgent{}, nil)
	assert.ErrorContains(t, err, "op isn't installed on this host")
}

// TestBuildOPBridgeInvalidRef verifies that an allowlist entry that
// isn't an op:// ref fails the build.
func TestBuildOPBridgeInvalidRef(t *testing.T) {
	host := newTestHost(t)
	host.Config.OPAllowedSecretRefs = []string{"Private/bank/password"}

	_, err := Build(t.Context(), host, &fakeAgent{}, nil)
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

// TestBuildTrustedDir verifies that the agent is told the working
// directory, as it sees it, is trusted.
func TestBuildTrustedDir(t *testing.T) {
	host := newTestHost(t)

	run, err := Build(t.Context(), host, &fakeAgent{}, nil)
	assert.NilError(t, err)

	want := "ASBX_TRUSTED_DIR=" + host.PWD
	assert.Assert(t, slices.Contains(run.Args(), want), "Build() args = %q, want %q", run.Args(), want)
}

// TestBuildPodman verifies that Podman's seccomp profile, device and
// storage are only added when it's enabled, and that Docker's /proc
// masks are never turned off for it.
func TestBuildPodman(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("HOME", cacheDir)
	t.Setenv("XDG_CACHE_HOME", cacheDir)

	host := newTestHost(t)

	run, err := Build(t.Context(), host, &fakeAgent{}, nil)
	assert.NilError(t, err)
	assert.Assert(t, !slices.Contains(run.Args(), "/dev/net/tun"), "Build() args = %q, want no podman args", run.Args())

	host.Config.PodmanEnabled = true
	run, err = Build(t.Context(), host, &fakeAgent{}, nil)
	assert.NilError(t, err)

	args := run.Args()
	for _, want := range []string{"/dev/net/tun", "ASBX_PODMAN=1"} {
		assert.Assert(t, slices.Contains(args, want), "Build() args = %q, want %q", args, want)
	}

	for _, arg := range args {
		unsafe := strings.Contains(arg, "systempaths") || strings.Contains(arg, "SYS_ADMIN")
		assert.Assert(t, !unsafe, "Build() args = %q, want /proc masked and no CAP_SYS_ADMIN", args)

		profile, ok := strings.CutPrefix(arg, "seccomp=")
		if !ok {
			continue
		}

		data, err := os.ReadFile(profile) //nolint:gosec // Why: the profile Build wrote.
		assert.NilError(t, err)
		assert.DeepEqual(t, data, podmanSeccompProfile)

		return
	}

	t.Fatalf("Build() args = %q, want a seccomp profile", args)
}

// TestPodmanSeccompProfile verifies that the embedded profile is valid
// JSON that allows what Podman needs and still blocks the rest.
func TestPodmanSeccompProfile(t *testing.T) {
	var profile struct {
		Syscalls []struct {
			Names    []string `json:"names"`
			Action   string   `json:"action"`
			Includes *struct {
				Caps []string `json:"caps"`
			} `json:"includes"`
		} `json:"syscalls"`
	}
	assert.NilError(t, json.Unmarshal(podmanSeccompProfile, &profile))

	// The syscalls allowed without any capability.
	allowed := make(map[string]bool)
	for _, rule := range profile.Syscalls {
		if rule.Action == "SCMP_ACT_ALLOW" && (rule.Includes == nil || len(rule.Includes.Caps) == 0) {
			for _, name := range rule.Names {
				allowed[name] = true
			}
		}
	}

	for _, name := range []string{"unshare", "setns", "mount", "pivot_root", "chroot", "clone3"} {
		assert.Assert(t, allowed[name], "%s isn't allowed", name)
	}

	for _, name := range []string{"bpf", "perf_event_open", "init_module", "kexec_load", "keyctl"} {
		assert.Assert(t, !allowed[name], "%s is allowed", name)
	}
}

// TestPodmanSeccompProfileNetfilter verifies that the embedded profile
// blocks NETLINK_NETFILTER sockets and still allows the other netlink
// protocols and address families moby's profile does.
func TestPodmanSeccompProfileNetfilter(t *testing.T) {
	type arg struct {
		Index uint   `json:"index"`
		Value uint64 `json:"value"`
		Op    string `json:"op"`
	}

	var profile struct {
		Syscalls []struct {
			Names    []string `json:"names"`
			Action   string   `json:"action"`
			Args     []arg    `json:"args"`
			Includes *struct {
				Caps []string `json:"caps"`
			} `json:"includes"`
		} `json:"syscalls"`
	}
	assert.NilError(t, json.Unmarshal(podmanSeccompProfile, &profile))

	// allows reports whether socket(family, _, protocol) is allowed
	// without any capability.
	allows := func(family, protocol uint64) bool {
		for _, rule := range profile.Syscalls {
			if !slices.Contains(rule.Names, "socket") || rule.Action != "SCMP_ACT_ALLOW" ||
				(rule.Includes != nil && len(rule.Includes.Caps) > 0) {
				continue
			}

			matches := true
			for _, a := range rule.Args {
				v := map[uint]uint64{0: family, 2: protocol}[a.Index]
				switch a.Op {
				case "SCMP_CMP_EQ":
					matches = matches && v == a.Value
				case "SCMP_CMP_NE":
					matches = matches && v != a.Value
				case "SCMP_CMP_LT":
					matches = matches && v < a.Value
				default:
					t.Fatalf("socket rule compares with %s, which this test doesn't handle", a.Op)
				}
			}

			if matches {
				return true
			}
		}

		return false
	}

	const afNetlink, netlinkRoute, netlinkNetfilter = 16, 0, 12
	assert.Assert(t, !allows(afNetlink, netlinkNetfilter), "NETLINK_NETFILTER sockets are allowed")
	assert.Assert(t, allows(afNetlink, netlinkRoute), "NETLINK_ROUTE sockets aren't allowed")

	// AF_UNIX, AF_INET, AF_INET6, AF_PACKET and the families either side
	// of AF_NETLINK.
	for _, family := range []uint64{1, 2, 10, 15, 17, 37} {
		assert.Assert(t, allows(family, 0), "address family %d isn't allowed", family)
	}
}
