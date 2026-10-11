package cmd

import (
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cacheClearCmd)
}

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the cache",
	Args:  cobra.NoArgs,
}

var cacheClearCmd = &cobra.Command{
	Use:               "clear [provider]",
	Short:             "Clear the whole cache, or only a provider's",
	Long:              "Clear the whole cache, or only the cache of the given provider (or metadata provider, e.g. anilist). Title bindings, logins and history aren't cache, they're kept.",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completionProviderIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := cache.Storage()
		if err != nil {
			return err
		}
		id := ""
		if len(args) == 1 {
			id = args[0]
		}
		n, err := st.ClearCache(id)
		if err != nil {
			return err
		}
		cmd.Printf("deleted %d cache entries\n", n)
		return nil
	},
}
