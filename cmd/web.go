package cmd

import (
	"github.com/luevano/mangal/v5/util"
	"github.com/luevano/mangal/v5/web"
	"github.com/spf13/cobra"
)

var webArgs = web.Args{}

func init() {
	rootCmd.AddCommand(webCmd)

	f := webCmd.Flags()
	f.BoolVarP(&webArgs.Open, "open", "o", false, "Open served page in the default browser")
	f.StringVarP(&webArgs.Port, "port", "p", "6969", "HTTP port to use")
}

var webCmd = &cobra.Command{
	Use:     "web",
	Short:   "Start server",
	Long:    "web, start server",
	GroupID: groupMode,
	Args:    cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		if err := web.Run(cmd.Context(), webArgs); err != nil {
			errorf(cmd, "%s", util.ErrorTree(err))
		}
	},
}
