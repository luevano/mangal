package util

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/aymanbagabas/go-osc52/v2"
	"golang.org/x/term"
)

// Copy puts text in the clipboard: the system's (xclip, pbcopy...), else the terminal's
// through OSC 52 (over SSH, no xclip), which can't tell whether the terminal took it.
// viaTerminal is true then.
func Copy(text string) (viaTerminal bool, err error) {
	sysErr := clipboard.WriteAll(text)
	if sysErr == nil {
		return false, nil
	}
	if err := copyOSC52(text, terminal()); err != nil {
		return false, fmt.Errorf("copy: %w (and %w)", sysErr, err)
	}
	return true, nil
}

// CopyNotice is the notification of a Copy, done says what was copied.
func CopyNotice(text, done string) string {
	switch viaTerminal, err := Copy(text); {
	case err != nil:
		return "Copy: " + err.Error()
	case viaTerminal:
		return done + " (through the terminal, OSC 52)"
	}
	return done
}

// terminal is where the TUI draws (stdout), else stderr; nil when neither is a terminal.
func terminal() io.Writer {
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		if term.IsTerminal(int(f.Fd())) {
			return f
		}
	}
	return nil
}

func copyOSC52(text string, out io.Writer) error {
	if out == nil {
		return errors.New("no terminal for OSC 52")
	}
	seq := osc52.New(text)
	switch {
	case os.Getenv("TMUX") != "":
		seq = seq.Tmux()
	case strings.HasPrefix(os.Getenv("TERM"), "screen"):
		seq = seq.Screen()
	}
	_, err := seq.WriteTo(out)
	return err
}
