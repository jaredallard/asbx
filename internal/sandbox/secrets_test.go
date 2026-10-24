package sandbox

import (
	"errors"
	"strings"
	"testing"

	"git.rgst.io/jaredallard/asbx/internal/docker"
	"git.rgst.io/jaredallard/asbx/internal/term"
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
	err := addSecrets(t.Context(), &term.Terminal{}, run,
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

	err := addSecrets(t.Context(), &term.Terminal{}, &docker.Run{}, Secret{
		Env:  "TOKEN",
		Ref:  "op://a/b/c",
		Name: "Token",
		Help: "create it at https://example.com",
	})
	assert.ErrorContains(t, err, `unable to determine Token, create it at https://example.com and store it in 1Password at "op://a/b/c"`)
}

// TestAddSecretsCreateNotInteractive verifies that, without a terminal
// to ask at, a secret asbx could create isn't offered, and the user is
// told how to create it instead.
func TestAddSecretsCreateNotInteractive(t *testing.T) {
	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name: "op",
		Args: []string{"read", "op://a/b/c"},
		Err:  errors.New("exit status 1"),
	}))

	err := addSecrets(t.Context(), &term.Terminal{}, &docker.Run{}, Secret{
		Env:    "TOKEN",
		Ref:    "op://a/b/c",
		Name:   "Token",
		Help:   "create it at https://example.com",
		Create: true,
	})
	assert.ErrorContains(t, err, `unable to determine Token, create it at https://example.com and store it in 1Password at "op://a/b/c"`)
}

// TestAddSecretsSteps verifies that each secret is shown as a step
// before it's read, but only when there's a terminal to show it at.
func TestAddSecretsSteps(t *testing.T) {
	tests := []struct {
		name        string
		interactive bool
		want        string
	}{
		{name: "Interactive", interactive: true, want: "  ›   reading TOKEN from 1Password\n"},
		{name: "NotInteractive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
				Name:   "op",
				Args:   []string{"read", "op://a/b/c"},
				Stdout: []byte("hunter2\n"),
			}))

			var out strings.Builder
			terminal := &term.Terminal{Out: &out, Interactive: tt.interactive}

			err := addSecrets(t.Context(), terminal, &docker.Run{}, Secret{Env: "TOKEN", Ref: "op://a/b/c"})
			assert.NilError(t, err)
			assert.Equal(t, out.String(), tt.want)
		})
	}
}

// listItems returns the mock of op listing vault "a", which has the item
// titled title.
func listItems(title string) *cmdexec.MockCommand {
	return &cmdexec.MockCommand{
		Name:   "op",
		Args:   []string{"item", "list", "--vault", "a", "--format", "json"},
		Stdout: []byte(`[{"title":"` + title + `"}]`),
	}
}

// TestCreateNotOffered verifies that the user isn't asked to create an
// item that exists, since the secret couldn't be read for another
// reason, or one they decline to create.
func TestCreateNotOffered(t *testing.T) {
	tests := []struct {
		name   string
		list   *cmdexec.MockCommand
		answer string
		asked  bool
	}{
		{name: "ItemExists", list: listItems("b")},
		{name: "Declined", list: listItems("other"), answer: "n\n", asked: true},
		{name: "DeclinedByDefault", list: listItems("other"), answer: "\n", asked: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(tt.list))

			var out strings.Builder
			terminal := &term.Terminal{In: strings.NewReader(tt.answer), Out: &out, Interactive: true, Accessible: true}

			_, ok, err := create(t.Context(), terminal, &Secret{Ref: "op://a/b/c", Name: "Token", Help: "create it"})
			assert.NilError(t, err)
			assert.Assert(t, !ok, "create() = ok, want not ok")

			asked := strings.Contains(out.String(), `Create the 1Password item "b" in the "a" vault?`)
			assert.Equal(t, asked, tt.asked, "create() wrote:\n%s", out.String())

			why := strings.Contains(out.String(), "Token isn't in 1Password at op://a/b/c.")
			assert.Equal(t, why, tt.asked, "create() wrote:\n%s", out.String())
		})
	}
}

// TestCreateErrors verifies that a ref asbx can't create the item for,
// or a vault op can't list, is an error rather than an offer.
func TestCreateErrors(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		cmds []*cmdexec.MockCommand
		want string
	}{
		{
			name: "SectionRef",
			ref:  "op://a/b/s/c",
			want: "isn't an op://vault/item/field ref",
		},
		{
			name: "ListFails",
			ref:  "op://a/b/c",
			cmds: []*cmdexec.MockCommand{{
				Name: "op",
				Args: []string{"item", "list", "--vault", "a", "--format", "json"},
				Err:  errors.New("exit status 1"),
			}},
			want: "exit status 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(tt.cmds...))

			var out strings.Builder
			terminal := &term.Terminal{In: strings.NewReader("y\n"), Out: &out, Interactive: true, Accessible: true}

			_, _, err := create(t.Context(), terminal, &Secret{Ref: tt.ref, Name: "Token"})
			assert.ErrorContains(t, err, tt.want)
			assert.Equal(t, out.String(), "")
		})
	}
}
