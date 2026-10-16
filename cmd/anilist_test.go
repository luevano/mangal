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

// TestPromptsFromInput: without a terminal the prompts read a line each from one reader
// (the id, then the secret).
func TestPromptsFromInput(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("my-id\ns3cret\n"))
	cmd.SetErr(io.Discard)
	in := bufio.NewReader(cmd.InOrStdin())
	id, err := prompt(cmd, in, "ID: ")
	if err != nil || id != "my-id" {
		t.Fatalf("id %q, %v", id, err)
	}
	if secret, err := readSecret(cmd, in, "Secret: "); err != nil || secret != "s3cret" {
		t.Errorf("secret %q, %v", secret, err)
	}
	if _, err := prompt(cmd, in, "More: "); err == nil {
		t.Error("prompt at EOF: no error")
	}
}
