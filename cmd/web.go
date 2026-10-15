package cmd

import (
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/serve"
	"github.com/spf13/cobra"
)

var webArgs = struct {
	Open bool
}{}

func init() {
	rootCmd.AddCommand(webCmd)

	f := webCmd.Flags()
	f.BoolVarP(&webArgs.Open, "open", "o", false, "Open served page in the default browser")
	f.String("host", "", "Address to listen on, 0.0.0.0 for every interface (needs a serve token)")
	configKeyFlag(f, "host", config.Web.Host.Key)
	f.IntP("port", "p", 0, "Port to listen on (not 6969, the logins' callback port)")
	configKeyFlag(f, "port", config.Web.Port.Key)
}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Serve the web UI",
	Long: `Run until stopped (ctrl+c), serving the web UI and the API v1 (as "mangal serve --web"
does, without its scheduler) on web.host:web.port, 127.0.0.1:6970 by default: the
library, the reader, downloads, watches, health, logs. The watches are those of the
mangal serve running for the same data, if any, else checked by this process.

Authentication is serve's (serve.auth, serve.loopback_scopes, the tokens of "mangal serve
token"): on loopback a browser on the same machine reads and manages the watches without
a token, file changes need a token with the write scope (the UI asks for it); off
loopback a token is required (the browser asks for it as a basic-auth password). On
loopback, requests for other host names are refused (DNS rebinding).`,
	GroupID: groupMode,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return serve.RunWeb(cmd.Context(), serve.Args{
			Host:            config.Web.Host.Get(),
			Port:            config.Web.Port.Get(),
			Token:           config.Serve.Token.Get(),
			TokenOnLoopback: config.Serve.TokenOnLoopback.Get(),
			Auth:            config.Serve.Auth.Get(),
			LoopbackScopes:  config.Serve.LoopbackScopes.Get(),
			AllowedHosts:    config.Serve.AllowedHosts.Get(),
			AllowedOrigins:  config.Serve.AllowedOrigins.Get(),
			TrustedProxies:  config.Serve.TrustedProxies.Get(),
		}, webArgs.Open, cmd.ErrOrStderr())
	},
}
