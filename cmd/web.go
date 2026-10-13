package cmd

import (
	"strconv"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/web"
	"github.com/spf13/cobra"
)

var webArgs = struct {
	Open bool
}{}

func init() {
	rootCmd.AddCommand(webCmd)

	f := webCmd.Flags()
	f.BoolVarP(&webArgs.Open, "open", "o", false, "Open served page in the default browser")
	f.String("host", "", "Address to listen on, 0.0.0.0 for every interface (there's no authentication)")
	configKeyFlag(f, "host", config.Web.Host.Key)
	f.IntP("port", "p", 0, "Port to listen on (not 6969, the logins' callback port)")
	configKeyFlag(f, "port", config.Web.Port.Key)
}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Serve a browse-only web UI",
	Long: `Run until stopped (ctrl+c), serving a web UI to search and browse the providers (no
downloads or library) on web.host:web.port, 127.0.0.1:6970 by default. It has no
authentication: on an address that isn't loopback anyone on the network can use it. On
loopback, requests for other host names are refused (DNS rebinding). "mangal serve --web"
serves the same UI next to the watches API, behind serve.token.`,
	GroupID: groupMode,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return web.Run(cmd.Context(), web.Args{
			Open: webArgs.Open,
			Host: config.Web.Host.Get(),
			Port: strconv.Itoa(config.Web.Port.Get()),
		}, cmd.ErrOrStderr())
	},
}
