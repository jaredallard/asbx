package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/config"
	"go.rgst.io/jaredallard/cmdexec/v2"
	"gotest.tools/v3/assert"
)

// TestUnsafeWorkDir verifies that working directories that would give
// the agent write access to the home directory or its config are
// refused, and that others, including look-alike siblings, aren't.
func TestUnsafeWorkDir(t *testing.T) {
	tests := []struct {
		workDir string
		want    bool
	}{
		{workDir: "/", want: true},
		{workDir: "/home", want: true},
		{workDir: "/home/u", want: true},
		{workDir: "/home/u/.config", want: true},
		{workDir: "/home/u/.config/asbx", want: true},
		{workDir: "/home/u/Code/x", want: false},
		{workDir: "/home/u/Code/.x", want: false},
		{workDir: "/home/user", want: false},
		{workDir: "/tmp", want: false},
	}

	for _, tt := range tests {
		got := unsafeWorkDir(tt.workDir, "/home/u")
		assert.Equal(t, got, tt.want, "unsafeWorkDir(%q, %q)", tt.workDir, "/home/u")
	}
}

// TestProjectRoot verifies that the project root is the top of the git
// repository the working directory is in.
func TestProjectRoot(t *testing.T) {
	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name:   "git",
		Args:   []string{"-C", "/repo/sub", "rev-parse", "--show-toplevel"},
		Stdout: []byte("/repo\n"),
	}))

	assert.Equal(t, projectRoot(t.Context(), "/repo/sub"), "/repo")
}

// TestProjectRootOutsideRepository verifies that the working directory
// is the project root when it isn't in a git repository.
func TestProjectRootOutsideRepository(t *testing.T) {
	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name: "git",
		Args: []string{"-C", "/dir", "rev-parse", "--show-toplevel"},
		Err:  errors.New("exit status 128"),
	}))

	assert.Equal(t, projectRoot(t.Context(), "/dir"), "/dir")
}

// TestCheckLegacyConfig verifies that an old KEY=value configuration
// file is refused with the file to convert it to, so its options don't
// silently stop applying, and that its absence is fine.
func TestCheckLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, ".asbx")
	path := filepath.Join(dir, projectConfigFile)

	assert.NilError(t, checkLegacyConfig(legacy, path))

	assert.NilError(t, os.WriteFile(legacy, []byte("NETWORK_HOST=true\n"), 0o600))
	err := checkLegacyConfig(legacy, path)
	assert.ErrorContains(t, err, legacy+`" uses the old KEY=value format, which is no longer read, convert it to "`+path)
}

// TestWriteDefaultConfig verifies that a user without a configuration
// gets the example one, in a directory that's created for it.
func TestWriteDefaultConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asbx", userConfigFile)

	wrote, err := writeDefaultConfig(path)
	assert.NilError(t, err)
	assert.Assert(t, wrote, "writeDefaultConfig(%q) = false, want true", path)

	got, err := os.ReadFile(path)
	assert.NilError(t, err)
	assert.Equal(t, string(got), string(config.Example))

	info, err := os.Stat(path)
	assert.NilError(t, err)
	assert.Equal(t, info.Mode().Perm(), os.FileMode(0o600))
}

// TestWriteDefaultConfigExists verifies that the user's configuration
// is never overwritten.
func TestWriteDefaultConfigExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), userConfigFile)
	assert.NilError(t, os.WriteFile(path, []byte("network_host: true\n"), 0o600))

	wrote, err := writeDefaultConfig(path)
	assert.NilError(t, err)
	assert.Assert(t, !wrote, "writeDefaultConfig(%q) = true, want false", path)

	got, err := os.ReadFile(path)
	assert.NilError(t, err)
	assert.Equal(t, string(got), "network_host: true\n")
}
