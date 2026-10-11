package cmd

import (
	"github.com/luevano/mangal/v5/web"
	"github.com/spf13/cobra"
)

var webArgs = web.Args{}

func init() {
	rootCmd.AddCommand(webCmd)

	f := webCmd.Flags()
	f.BoolVarP(&webArgs.Open, "open", "o", false, "Open served page in the default browser")
	f.StringVar(&webArgs.Host, "host", "127.0.0.1", "Address to listen on, 0.0.0.0 for every interface (there's no authentication)")
	f.StringVarP(&webArgs.Port, "port", "p", "6969", "HTTP port to use")
}

var webCmd = &cobra.Command{
	Use:     "web",
	Short:   "Serve a browse-only web UI",
	Long:    "web, serve a web UI to search and browse the providers (no downloads or library)",
	GroupID: groupMode,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return web.Run(cmd.Context(), webArgs, cmd.ErrOrStderr())
	},
}
