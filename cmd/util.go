package cmd

import (
	"bufio"
	"fmt"
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
