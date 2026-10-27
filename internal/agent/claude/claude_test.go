package claude

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/config"
	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/sandbox"
	"gotest.tools/v3/assert"
)

// mountedClaudeEntries returns the names of the ~/.claude entries args
// mounts.
func mountedClaudeEntries(args []string) []string {
	prefix := "target=" + sandbox.ReadOnlyHostDir + "/.claude/"

	var names []string
	for _, arg := range args {
		if _, target, ok := strings.Cut(arg, prefix); ok {
			names = append(names, strings.TrimSuffix(target, ",readonly"))
		}
	}

	return names
}

// TestMountConfig verifies that ~/.claude is mounted without the
// entries that hold other projects' sessions, memory or history, and
// that dotfiles are included.
func TestMountConfig(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude")
	for _, name := range []string{"projects", "plans"} {
		assert.NilError(t, os.MkdirAll(filepath.Join(dir, name), 0o750))
	}
	for _, name := range []string{"settings.json", ".credentials.json", "history.jsonl"} {
		assert.NilError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
	}

	run := &docker.Run{}
	assert.NilError(t, mountConfig(home, run))
	assert.DeepEqual(t, mountedClaudeEntries(run.Args()), []string{".credentials.json", "settings.json"})
}

// TestMountConfigMissing verifies that a missing ~/.claude isn't an
// error.
func TestMountConfigMissing(t *testing.T) {
	run := &docker.Run{}
	assert.NilError(t, mountConfig(t.TempDir(), run))
	assert.Equal(t, len(mountedClaudeEntries(run.Args())), 0)
}

// TestManagedSettingsPathUnsupported verifies that mirroring managed
// settings is refused on operating systems Claude Code has no managed
// settings path for.
func TestManagedSettingsPathUnsupported(t *testing.T) {
	_, err := managedSettingsPath("plan9")
	assert.ErrorContains(t, err, "isn't supported on plan9")
}

// newProjectHost returns a Host whose working directory has a Claude
// Code project directory with its memory symlinked to target.
func newProjectHost(t *testing.T, target string) (host *sandbox.Host, memory string) {
	t.Helper()

	host = &sandbox.Host{
		Home:      t.TempDir(),
		PWD:       "/work/my.repo",
		ConfigDir: t.TempDir(),
		Config:    &config.Config{},
	}

	dir := filepath.Join(host.Home, ".claude", "projects", "-work-my-repo")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("MkdirAll(%q) = %v", dir, err)
	}

	memory = filepath.Join(dir, "memory")
	if err := os.Symlink(target, memory); err != nil {
		t.Fatalf("Symlink(%q, %q) = %v", target, memory, err)
	}

	return host, memory
}

// TestMirrorProjectsPinsMemoryLink verifies that a symlinked memory
// directory is mounted at its target and pinned the first time it's
// seen, and that a changed target is refused afterwards so the agent
// can't redirect what the host reads.
func TestMirrorProjectsPinsMemoryLink(t *testing.T) {
	target := t.TempDir()
	host, memory := newProjectHost(t, target)

	// The temporary directory can itself be behind a symlink (/var on
	// macOS), and it's the resolved path that's mounted.
	resolved, err := filepath.EvalSymlinks(target)
	assert.NilError(t, err)

	run := &docker.Run{}
	assert.NilError(t, mirrorProjects(host, run))
	assert.Assert(t, slices.Contains(run.Args(), resolved+":"+target), "mirrorProjects() args = %q", run.Args())

	pin, err := os.ReadFile(filepath.Join(host.ConfigDir, "memory-links", "-work-my-repo"))
	assert.NilError(t, err)
	assert.Equal(t, string(pin), target+"\n")

	// Pinned to the same target, it's still allowed.
	assert.NilError(t, mirrorProjects(host, &docker.Run{}))

	assert.NilError(t, os.Remove(memory))
	assert.NilError(t, os.Symlink(t.TempDir(), memory))

	err = mirrorProjects(host, &docker.Run{})
	assert.ErrorContains(t, err, "but was pinned to "+target)
}

// TestMirrorProjectsRelativeMemoryLink verifies that a memory symlink
// to a relative path is refused, since it would resolve differently in
// the container.
func TestMirrorProjectsRelativeMemoryLink(t *testing.T) {
	host, _ := newProjectHost(t, "../elsewhere")

	err := mirrorProjects(host, &docker.Run{})
	assert.ErrorContains(t, err, "must be a symlink to an absolute path")
}
