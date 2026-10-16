package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/theme/huhtheme"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// The prompts are huh fields on a terminal; without one (pipes, scripts, tests) they read
// a line each from the same reader, so several answers can be piped in.

// prompt reads a line, trimmed.
func prompt(cmd *cobra.Command, in *bufio.Reader, label string) (string, error) {
	return readLine(cmd, in, label, false)
}

// readSecret reads a line, trimmed, masked on a terminal. An empty input is "".
func readSecret(cmd *cobra.Command, in *bufio.Reader, label string) (string, error) {
	return readLine(cmd, in, label, true)
}

func readLine(cmd *cobra.Command, in *bufio.Reader, label string, secret bool) (string, error) {
	what := strings.TrimSuffix(strings.TrimSpace(label), ":")
	out := cmd.ErrOrStderr()
	f, ok := terminal(cmd.InOrStdin())
	if !ok {
		fmt.Fprint(out, label)
		line, err := in.ReadString('\n')
		if err != nil && (!errors.Is(err, io.EOF) || line == "" && !secret) {
			return "", fmt.Errorf("read %s: %w", what, err)
		}
		return strings.TrimSpace(line), nil
	}

	var value string
	input := huh.NewInput().Title(strings.TrimRight(label, " ")).Inline(true).Prompt(" ").Value(&value)
	if secret {
		input.EchoMode(huh.EchoModePassword)
	}
	if err := runPrompt(cmd, f, input); err != nil {
		return "", fmt.Errorf("read %s: %w", what, err)
	}
	// huh clears the prompt, the answer stays in the terminal as it did
	if secret {
		fmt.Fprintln(out, label)
	} else {
		fmt.Fprintln(out, label+value)
	}
	return strings.TrimSpace(value), nil
}

// confirm asks a yes/no question in the terminal, no (without asking) if it isn't one.
func confirm(cmd *cobra.Command, question string) bool {
	f, ok := terminal(os.Stdin)
	if !ok {
		return false
	}
	var yes bool
	field := huh.NewConfirm().Title(question + " ").Inline(true).WithButtonAlignment(lipgloss.Left).Value(&yes)
	if err := runPrompt(cmd, f, field); err != nil {
		// ctrl+c is a no
		return false
	}
	answer := "No"
	if yes {
		answer = "Yes"
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", question, answer)
	return yes
}

// terminal is r when it's a terminal.
func terminal(r io.Reader) (*os.File, bool) {
	f, ok := r.(*os.File)
	return f, ok && term.IsTerminal(int(f.Fd()))
}

// runPrompt runs a field alone on the terminal in, drawn on cmd's stderr; ctrl+c (or the
// cancelled ctx) ends it with an error.
func runPrompt(cmd *cobra.Command, in *os.File, field huh.Field) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return huh.NewForm(huh.NewGroup(field)).
		WithShowHelp(false).
		WithTheme(huhtheme.New(nil)).
		WithInput(in).
		WithOutput(cmd.ErrOrStderr()).
		RunWithContext(ctx)
}
