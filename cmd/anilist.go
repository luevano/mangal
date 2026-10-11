package cmd

import (
	"github.com/luevano/mangal/v5/tui/base"
	anilistViewer "github.com/luevano/mangal/v5/tui/model/anilist"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(anilistCmd)
}

var anilistCmd = &cobra.Command{
	Use:   "anilist",
	Short: "Log in to Anilist",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return base.RunStandalone(cmd.Context(), anilistViewer.New())
	},
}
