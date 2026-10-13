package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func successf(cmd *cobra.Command, format string, a ...any) {
	cmd.Printf("%s %s\n", icon.Check.Colored(), fmt.Sprintf(format, a...))
}

func completionProviderIDs(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	loaders, err := manager.Loaders()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	IDs := lo.Map(loaders, func(loader libmangal.ProviderLoader, _ int) string {
		return loader.Info().ID
	})

	return IDs, cobra.ShellCompDirectiveDefault
}

func completionConfigKeys(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	keys := config.Keys()

	filtered := lo.Filter(keys, func(key string, _ int) bool {
		return strings.HasPrefix(key, toComplete)
	})

	return filtered, cobra.ShellCompDirectiveDefault
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
