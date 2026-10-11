package cmd

import (
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/serve"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(serveCmd)

	f := serveCmd.Flags()
	f.String("host", "", "Address to listen on, 0.0.0.0 for every interface (needs serve.token)")
	configKeyFlag(f, "host", config.Serve.Host.Key)
	f.IntP("port", "p", 0, "Port to listen on")
	configKeyFlag(f, "port", config.Serve.Port.Key)
	f.Bool("web", false, "Also serve the web UI")
	configKeyFlag(f, "web", config.Serve.Web.Key)
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Check the watches on a schedule, with an API to manage them",
	Long: `Run until stopped (SIGTERM, ctrl+c), checking each watch when it's due (watch.interval,
or its own) and the gaps when watch.gap_interval says, one check at a time (a "mangal watch
check" running elsewhere postpones it). The watch.* settings are re-read from the config
file every minute. Logs go to stderr and the log file.

The JSON API (serve.host:serve.port, 127.0.0.1 by default) is what the TUI and the "mangal
watch" commands use while it runs. On an address that isn't loopback it needs serve.token
(Authorization: Bearer <token>). One server per data db: a second instance needs another
config (--config) with its own data.path and serve.port.`,
	Example: `  mangal serve
  mangal --config ~/.config/mangal-b/mangal.toml serve --port 6978`,
	GroupID: groupMode,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return serve.Run(cmd.Context(), serve.Args{
			Host:  config.Serve.Host.Get(),
			Port:  config.Serve.Port.Get(),
			Web:   config.Serve.Web.Get(),
			Token: config.Serve.Token.Get(),
		}, cmd.ErrOrStderr())
	},
}
