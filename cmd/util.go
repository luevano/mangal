package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
)

func successf(cmd *cobra.Command, format string, a ...any) {
	cmd.Printf(fmt.Sprintf("%s %s\n", icon.Check.Colored(), format), a...)
}

func errorf(cmd *cobra.Command, format string, a ...any) {
	cmd.PrintErrf(fmt.Sprintf("%s %s\n", icon.Cross.Colored(), format), a...)
	os.Exit(1)
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
