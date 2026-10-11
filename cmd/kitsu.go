package cmd

import (
	"github.com/luevano/mangal/v5/tui/base"
	kitsuViewer "github.com/luevano/mangal/v5/tui/model/kitsu"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(kitsuCmd)
}

var kitsuCmd = &cobra.Command{
	Use:   "kitsu",
	Short: "Log in to Kitsu",
	Long: `Log in to Kitsu with your email (or user name) and password, to sync the reading
progress (read.history.kitsu). Only the login token is saved, never the password.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return base.RunStandalone(cmd.Context(), kitsuViewer.New())
	},
}
