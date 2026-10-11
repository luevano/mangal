package cmd

import (
	"github.com/luevano/mangal/v5/meta"
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
	Use:   "version",
	Short: "Show version information",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		if versionArgs.Short {
			cmd.Println(meta.Version)
			return
		}

		cmd.Println(meta.PrettyVersion())
	},
}
