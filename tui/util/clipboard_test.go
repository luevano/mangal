package util

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestCopyOSC52(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-256color")
	var out bytes.Buffer
	if err := copyOSC52("https://anilist.co", &out); err != nil {
		t.Fatal(err)
	}
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("https://anilist.co")) + "\x07"
	if out.String() != want {
		t.Errorf("wrote %q, want %q", out.String(), want)
	}

	out.Reset()
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
	if err := copyOSC52("x", &out); err != nil || !strings.HasPrefix(out.String(), "\x1bPtmux;") {
		t.Errorf("tmux: %q, %v", out.String(), err)
	}

	if err := copyOSC52("x", nil); err == nil {
		t.Error("no terminal: no error")
	}
}
