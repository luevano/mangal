package cmd

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestAnilistSecretFlagDeprecated(t *testing.T) {
	if f := anilistLoginCmd.Flags().Lookup("secret"); f == nil || f.Deprecated == "" || !f.Hidden {
		t.Errorf("--secret %+v", f)
	}
}

func TestReadSecretFromInput(t *testing.T) {
	for in, want := range map[string]string{"s3cret\n": "s3cret", "  tok  ": "tok", "": ""} {
		cmd := &cobra.Command{}
		cmd.SetIn(strings.NewReader(in))
		cmd.SetErr(io.Discard)
		got, err := readSecret(cmd, bufio.NewReader(cmd.InOrStdin()), "Secret: ")
		if err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", in, got, err, want)
		}
	}
}
