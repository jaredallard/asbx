package sandbox

import (
	"errors"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/docker"
	"go.rgst.io/jaredallard/cmdexec/v2"
	"gotest.tools/v3/assert"
)

// TestExtraSecrets verifies that EXTRA_SECRET_ENV_VARS entries are
// split on their first colon, since the op:// ref has colons of its
// own, and that empty entries are skipped.
func TestExtraSecrets(t *testing.T) {
	got, err := extraSecrets([]string{"GITHUB_TOKEN:op://a/b:c/d", "", "_X:op://e/f/g"})
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []Secret{
		{Env: "GITHUB_TOKEN", Ref: "op://a/b:c/d", Name: "GITHUB_TOKEN", Help: "create it"},
		{Env: "_X", Ref: "op://e/f/g", Name: "_X", Help: "create it"},
	})
}

// TestExtraSecretsErrors verifies that malformed EXTRA_SECRET_ENV_VARS
// entries are rejected.
func TestExtraSecretsErrors(t *testing.T) {
	tests := []struct {
		name  string
		entry string
	}{
		{name: "NoColon", entry: "GITHUB_TOKEN"},
		{name: "InvalidName", entry: "1TOKEN:op://a/b/c"},
		{name: "EmptyName", entry: ":op://a/b/c"},
		{name: "NotOPRef", entry: "TOKEN:https://example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extraSecrets([]string{tt.entry})
			assert.ErrorContains(t, err, "must be NAME:op://ref")
		})
	}
}

// TestAddSecrets verifies that secrets with a ref are read from
// 1Password and passed in by name, and that ones without are skipped.
func TestAddSecrets(t *testing.T) {
	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name:   "op",
		Args:   []string{"read", "op://a/b/c"},
		Stdout: []byte("hunter2\n"),
	}))

	run := &docker.Run{}
	err := AddSecrets(t.Context(), run,
		Secret{Env: "SKIPPED"},
		Secret{Env: "TOKEN", Ref: "op://a/b/c"},
	)
	assert.NilError(t, err)
	assert.DeepEqual(t, run.Args(), []string{"--env", "TOKEN"})
}

// TestAddSecretsError verifies that a secret that can't be read tells
// the user how to create it.
func TestAddSecretsError(t *testing.T) {
	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name: "op",
		Args: []string{"read", "op://a/b/c"},
		Err:  errors.New("exit status 1"),
	}))

	err := AddSecrets(t.Context(), &docker.Run{}, Secret{
		Env:  "TOKEN",
		Ref:  "op://a/b/c",
		Name: "Token",
		Help: "create it at https://example.com",
	})
	assert.ErrorContains(t, err, `unable to determine Token, create it at https://example.com and store it in 1Password at "op://a/b/c"`)
}
