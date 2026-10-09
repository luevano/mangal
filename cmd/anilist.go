package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/client/anilist"
	anilistViewer "github.com/luevano/mangal/tui/model/anilist"
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
		if _, err := tea.NewProgram(anilistViewer.New(cmd.Context(), anilist.Anilist(), true)).Run(); err != nil {
			errorf(cmd, err.Error())
		}
	},
}
