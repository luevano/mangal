package cmd

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestScriptDocKeepsAnExistingFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("mangal.lua", []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	scriptDocCmd.SetOut(io.Discard)
	t.Cleanup(func() { scriptDocArgs.Force = false })

	if err := scriptDocCmd.RunE(scriptDocCmd, nil); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("no --force: %v", err)
	}
	if b, _ := os.ReadFile("mangal.lua"); string(b) != "mine" {
		t.Fatalf("overwritten without --force: %q", b)
	}

	scriptDocArgs.Force = true
	if err := scriptDocCmd.RunE(scriptDocCmd, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile("mangal.lua"); string(b) == "mine" || len(b) == 0 {
		t.Fatalf("not overwritten with --force: %q", b)
	}
}
