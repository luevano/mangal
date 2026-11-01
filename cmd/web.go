package cmd

import (
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/serve"
	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/cobra"
)

var webArgs = struct {
	Open bool
}{}

func init() {
	rootCmd.AddCommand(webCmd)

	f := webCmd.Flags()
	f.BoolVarP(&webArgs.Open, "open", "o", false, "Open the web UI in the default browser")
	f.String("host", "", "Address to listen on, 0.0.0.0 for every interface")
	configKeyFlag(f, "host", config.Serve.Host.Key)
	f.IntP("port", "p", 0, "Port to listen on")
	configKeyFlag(f, "port", config.Serve.Port.Key)
}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Serve the web UI",
	Long: `The web UI: the running mangal serve of this config serves it (serve.web), so with one
running this only says where it is (--open opens it). Else it runs until stopped (ctrl+c),
serving the web UI and the API v1 as "mangal serve" does on serve.host:serve.port
(127.0.0.1:6977 by default), with its logins, without checking the watches on schedule.
The library, the reader, downloads, watches, health, logs. On a server (the connection, --remote)
it says the server's address (--open opens it, the browser logs in there).`,
	GroupID: groupMode,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		url, err := remoteTarget(cmd).Address()
		if err != nil {
			return err
		}
		if url != "" {
			cmd.Printf("the web UI of %s: %s\n", remote.Name(url), url)
			if webArgs.Open {
				return open.Start(url)
			}
			return nil
		}
		return serve.RunWeb(cmd.Context(), serveArgs(), webArgs.Open, cmd.ErrOrStderr())
	},
}
