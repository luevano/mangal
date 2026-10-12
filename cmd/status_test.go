package cmd

import (
	"bytes"
	"testing"

	"github.com/luevano/mangal/v5/library"
	"github.com/spf13/cobra"
)

func TestStatusNotATerminal(t *testing.T) {
	defer func(v bool) { rootArgs.Verbose = v }(rootArgs.Verbose)
	for _, verbose := range []bool{false, true} {
		rootArgs.Verbose = verbose
		var errOut bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetErr(&errOut)
		st := newStatus(cmd)
		st.Progress(library.Progress{Phase: "scanning library", Done: 1, Total: 2, Item: "One Piece"})
		st.Set("scanning library: 1/2 (One Piece)")
		st.Set("AniList rate limited, retrying in 58s")
		st.Done()
		want := ""
		if verbose {
			want = "scanning library: 1/2 (One Piece)\nAniList rate limited, retrying in 58s\n"
		}
		if errOut.String() != want {
			t.Errorf("verbose %v: got %q, want %q", verbose, errOut.String(), want)
		}
	}
}

func TestStatusTerminal(t *testing.T) {
	var out bytes.Buffer
	st := &status{w: &out, tty: true, width: 12}
	st.Set("renaming: 1/2 (Berserk)")
	st.mu.Lock()
	st.redraw()
	st.mu.Unlock()
	st.Print(func() { out.WriteString("A -> B\n") })
	st.Done()
	want := "\r\x1b[2Krenaming: …" + "\r\x1b[2KA -> B\n" + "\r\x1b[2Krenaming: …" + "\r\x1b[2K"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}
