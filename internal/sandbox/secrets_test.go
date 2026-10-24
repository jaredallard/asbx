package sandbox

import (
	"errors"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/docker"
	"go.rgst.io/jaredallard/cmdexec/v2"
	"gotest.tools/v3/assert"
)

// TestExtraSecrets verifies that extra_secret_env_vars entries become
// secrets sorted by name, and that names with an empty ref are skipped.
func TestExtraSecrets(t *testing.T) {
	got, err := extraSecrets(map[string]string{
		"_X":           "op://e/f/g",
		"SKIPPED":      "",
		"GITHUB_TOKEN": "op://a/b:c/d",
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []Secret{
		{Env: "GITHUB_TOKEN", Ref: "op://a/b:c/d", Name: "GITHUB_TOKEN", Help: "create it"},
		{Env: "_X", Ref: "op://e/f/g", Name: "_X", Help: "create it"},
	})
}

// TestExtraSecretsErrors verifies that extra_secret_env_vars entries
// with an invalid name or a ref that isn't op:// are rejected.
func TestExtraSecretsErrors(t *testing.T) {
	tests := []struct {
		name    string
		envName string
		ref     string
		want    string
	}{
		{name: "InvalidName", envName: "1TOKEN", ref: "op://a/b/c", want: `name "1TOKEN" isn't a valid environment variable name`},
		{name: "NameWithEquals", envName: "A=B", ref: "op://a/b/c", want: `name "A=B" isn't a valid environment variable name`},
		{name: "NotOPRef", envName: "TOKEN", ref: "https://example.com", want: `ref "https://example.com" for TOKEN must be an op:// ref`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extraSecrets(map[string]string{tt.envName: tt.ref})
			assert.ErrorContains(t, err, tt.want)
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
