package cmd

import (
	tea "github.com/charmbracelet/bubbletea"
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
		_, err := tea.NewProgram(anilistViewer.New(cmd.Context(), true)).Run()
		return err
	},
}
