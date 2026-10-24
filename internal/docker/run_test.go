package docker

import (
	"testing"

	"gotest.tools/v3/assert"
)

// TestRunSecret verifies that a secret's value is only in docker's
// environment, never on its command line.
func TestRunSecret(t *testing.T) {
	var run Run
	run.Secret("TOKEN", "hunter2")

	assert.DeepEqual(t, run.command("image", []string{"cmd"}), []string{"run", "--env", "TOKEN", "image", "cmd"})
	assert.DeepEqual(t, run.environ(nil), []string{"TOKEN=hunter2"})
}

// TestRunEnviron verifies that a secret replaces a host variable of the
// same name, since docker reads the first of duplicate variables.
func TestRunEnviron(t *testing.T) {
	var run Run
	run.Secret("TOKEN", "new")

	got := run.environ([]string{"HOME=/home/u", "TOKEN=old", "TOKENS=other"})
	assert.DeepEqual(t, got, []string{"TOKEN=new", "HOME=/home/u", "TOKENS=other"})
}

// TestRunMount verifies the --mount arguments for read-only and
// read-write bind mounts.
func TestRunMount(t *testing.T) {
	var run Run
	run.Mount("/a", "/b", true)
	run.Mount("/c", "/d", false)

	assert.DeepEqual(t, run.Args(), []string{
		"--mount", "type=bind,source=/a,target=/b,readonly",
		"--mount", "type=bind,source=/c,target=/d",
	})
}

// TestRunSetName verifies that a named container is run with --name.
func TestRunSetName(t *testing.T) {
	var run Run
	run.SetName("asbx-a")

	assert.Equal(t, run.Name(), "asbx-a")
	assert.DeepEqual(t, run.Args(), []string{"--name", "asbx-a"})
}
