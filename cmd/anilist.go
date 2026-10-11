package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
	anilistViewer "github.com/luevano/mangal/v5/tui/model/anilist"
	"github.com/luevano/mangal/v5/util"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(anilistCmd)
}

var anilistCmd = &cobra.Command{
	Use:   "anilist",
	Short: "Anilist auth commands",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		if _, err := tea.NewProgram(anilistViewer.New(cmd.Context(), true)).Run(); err != nil {
			errorf(cmd, "%s", util.ErrorTree(err))
		}
	},
}
