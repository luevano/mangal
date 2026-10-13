package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/serve"
	"github.com/luevano/mangal/v5/util/cache"
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
	Long: `Run until stopped (SIGTERM, ctrl+c), checking each watch at the ticks of its cron
schedule (watch.schedule, every 4 hours on the hour by default, or its own; local time),
whenever the server started: checks only start on ticks, nothing runs at start (a new
watch, or one whose tick passed while the server was down, waits for its next tick). The
gaps are filled at the
ticks of watch.gap_schedule (off by default). Each provider has its own run
(watch.parallel_providers at once; a "mangal watch check" of a provider running elsewhere
postpones that provider). The watch.* settings are re-read from the config file every
minute. Logs go to stderr and the log file; GET /api/events streams the live progress.

The JSON API (serve.host:serve.port, 127.0.0.1 by default) is what the TUI and the "mangal
watch" commands use while it runs. On an address that isn't loopback it needs serve.token
(Authorization: Bearer <token>), on loopback only with serve.token_on_loopback. Requests
from another site's pages (a foreign Origin) are refused, bodies have to be JSON. One
server per data db: a second instance needs another config (--config) with its own
data.path and serve.port.`,
	Example: `  mangal serve
  mangal --config ~/.config/mangal-b/mangal.toml serve --port 6978`,
	GroupID: groupMode,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		token, err := watch.Token()
		if err != nil {
			return err
		}
		return serve.Run(cmd.Context(), serve.Args{
			Host:            config.Serve.Host.Get(),
			Port:            config.Serve.Port.Get(),
			Web:             config.Serve.Web.Get(),
			Token:           token,
			TokenOnLoopback: config.Serve.TokenOnLoopback.Get(),
		}, cmd.ErrOrStderr())
	},
}

var serveTokenArgs = struct {
	Generate, Clear bool
}{}

func init() {
	serveCmd.AddCommand(serveTokenCmd)
	f := serveTokenCmd.Flags()
	f.BoolVar(&serveTokenArgs.Generate, "generate", false, "Generate a random token, save it and print it")
	f.BoolVar(&serveTokenArgs.Clear, "clear", false, "Remove the saved token")
}

var serveTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Save the token of the serve API",
	Long: `Save the token the serve API asks for when it listens on an address that isn't loopback
(Authorization: Bearer <token>); the TUI and "mangal watch" on the same config use it too.
It's kept with the logins (the OS keyring with auth.keyring); serve.token is only used when
none is saved. The token is read from the terminal without echo, or from stdin.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		var token string
		switch {
		case serveTokenArgs.Clear:
			if err := cache.SetServeToken(""); err != nil {
				return err
			}
			successf(cmd, "removed the saved serve token")
			return nil
		case serveTokenArgs.Generate:
			b := make([]byte, 24)
			if _, err := rand.Read(b); err != nil {
				return err
			}
			token = hex.EncodeToString(b)
		default:
			var err error
			if token, err = readSecret(cmd, "Serve token: "); err != nil {
				return err
			}
			if token == "" {
				return errors.New("no token given (--generate makes one, --clear removes the saved one)")
			}
		}
		if err := cache.SetServeToken(token); err != nil {
			return err
		}
		if serveTokenArgs.Generate {
			cmd.Println(token)
		}
		successf(cmd, "saved the serve token")
		return nil
	},
}
