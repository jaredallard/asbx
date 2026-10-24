package onepassword

import (
	"errors"
	"testing"

	"go.rgst.io/jaredallard/cmdexec/v2"
	"gotest.tools/v3/assert"
)

// TestRead verifies that a secret is returned without the trailing
// newline op prints after it.
func TestRead(t *testing.T) {
	cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(&cmdexec.MockCommand{
		Name:   "op",
		Args:   []string{"read", "op://a/b/c"},
		Stdout: []byte("hunter2\n"),
	}))

	got, err := Read(t.Context(), "op://a/b/c")
	assert.NilError(t, err)
	assert.Equal(t, got, "hunter2")
}

// TestReadErrors verifies that a secret op can't read, or that is
// empty, is an error rather than an empty value.
func TestReadErrors(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cmdexec.MockCommand
	}{
		{
			name: "CommandFails",
			cmd:  &cmdexec.MockCommand{Err: errors.New("exit status 1")},
		},
		{
			name: "EmptySecret",
			cmd:  &cmdexec.MockCommand{Stdout: []byte("\n")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cmd.Name = "op"
			tt.cmd.Args = []string{"read", "op://a/b/c"}
			cmdexec.UseMockExecutor(t, cmdexec.NewMockExecutor(tt.cmd))

			got, err := Read(t.Context(), "op://a/b/c")
			assert.Assert(t, err != nil, "Read() = %q, want error", got)
		})
	}
}
