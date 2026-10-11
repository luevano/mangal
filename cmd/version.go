package cmd

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/spf13/cobra"
)

var versionArgs = struct {
	Short bool
}{}

func init() {
	rootCmd.AddCommand(versionCmd)

	f := versionCmd.Flags()
	f.BoolVarP(&versionArgs.Short, "short", "s", false, "Only show mangal version number")
}

var versionCmd = &cobra.Command{
	Annotations: map[string]string{startupAnnotation: startupNone},
	Use:         "version",
	Short:       "Show version information",
	Args:        cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		if versionArgs.Short {
			cmd.Println(meta.Version)
			return
		}

		cmd.Println(lipgloss.JoinVertical(lipgloss.Center, style.Bold.Accent.Render(meta.Logo), "\n"+meta.Versions()))
	},
}
