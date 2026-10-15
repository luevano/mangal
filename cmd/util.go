package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func successf(cmd *cobra.Command, format string, a ...any) {
	fmt.Fprintf(styled(cmd.OutOrStderr()), "%s %s\n", icon.Check.Colored(), fmt.Sprintf(format, a...))
}

// styled is w for styled text (theme styles, colored icons). lipgloss v2 renders full
// colors: the writer brings them down to what w shows, none when it's not a terminal.
func styled(w io.Writer) io.Writer {
	return colorprofile.NewWriter(w, os.Environ())
}

var detectBackground sync.Once

// styledAdaptive is styled for text with colors that depend on the background
// (color.LightDark): a terminal's is asked once. Get it before rendering.
func styledAdaptive(w io.Writer) io.Writer {
	if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		detectBackground.Do(func() {
			color.SetDark(lipgloss.HasDarkBackground(os.Stdin, f))
		})
	}
	return styled(w)
}

// completionProviderIDs completes a provider id (a flag's value).
func completionProviderIDs(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	loaders, err := manager.Loaders()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	IDs := lo.Map(loaders, func(loader libmangal.ProviderLoader, _ int) string {
		return loader.Info().ID
	})

	return IDs, cobra.ShellCompDirectiveNoFileComp
}

// completionFirstProvider completes the first argument with a provider id, nothing after
// it (manga ids, queries).
func completionFirstProvider(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completionProviderIDs(cmd, args, toComplete)
}

// completionConfigKeys completes the first argument with a config key.
func completionConfigKeys(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	keys := config.Keys()

	filtered := lo.Filter(keys, func(key string, _ int) bool {
		return strings.HasPrefix(key, toComplete)
	})

	return filtered, cobra.ShellCompDirectiveNoFileComp
}

// readSecret reads a line from in, without echo when cmd's input is a terminal. An empty
// input is "".
func readSecret(cmd *cobra.Command, in *bufio.Reader, label string) (string, error) {
	fmt.Fprint(cmd.ErrOrStderr(), label)
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		line, err := in.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read %s: %w", strings.TrimSuffix(strings.TrimSpace(label), ":"), err)
		}
		return strings.TrimSpace(line), nil
	}
	secret, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", fmt.Errorf("read the secret: %w", err)
	}
	return strings.TrimSpace(string(secret)), nil
}

// confirm asks a yes/no question in the terminal, no (without asking) if it isn't one.
func confirm(cmd *cobra.Command, question string) bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	cmd.Printf("%s [y/N] ", question)
	answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}
