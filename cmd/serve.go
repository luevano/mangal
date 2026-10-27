package cmd

import (
	"errors"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/serve"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(serveCmd)

	f := serveCmd.Flags()
	f.String("host", "", "Address to listen on, 0.0.0.0 for every interface")
	configKeyFlag(f, "host", config.Serve.Host.Key)
	f.IntP("port", "p", 0, "Port to listen on")
	configKeyFlag(f, "port", config.Serve.Port.Key)
	f.Bool("web", true, "Serve the web UI too")
	configKeyFlag(f, "web", config.Serve.Web.Key)
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Check the watches on a schedule, serve the web UI and the API",
	Long: `Run until stopped (SIGTERM, ctrl+c), checking each watch at the ticks of its cron
schedule (watch.schedule, every 4 hours on the hour by default, or its own; local time),
whenever the server started: checks only start on ticks, nothing runs at start (a new
watch, or one whose tick passed while the server was down, waits for its next tick). The
gaps are filled at the ticks of watch.gap_schedule (off by default). Each provider has its
own run (watch.parallel_providers at once; a provider busy elsewhere, e.g. a "mangal watch
check" or a download, is checked once it's free). The watch.* settings are re-read from
the config file every minute. Logs go to stderr and the log file.

On serve.host:serve.port (127.0.0.1:6977 by default): the web UI (serve.web), the HTTP
API (/api/v1/, its spec at /api/v1/openapi.yaml, "mangal api" calls it) and OPDS (/opds).
Every request logs in, wherever it comes from: people as users ("mangal user add"; the
web UI's login page, basic auth for OPDS readers), programs with an API token ("mangal
token add"; Authorization: Bearer). mangal on this config (the TUI, "mangal
watch", "mangal api") uses the server's key, kept in its discovery file next to the data
db. Behind a reverse proxy nothing needs setting; serve.trusted_proxies gives the logs and
the login rate limit the clients' addresses. One server per data db: a second instance
needs another config (--config) with its own data.path and serve.port.`,
	Example: `  mangal user add david
  mangal serve
  mangal --config ~/.config/mangal-b/mangal.toml serve --port 6978`,
	GroupID: groupMode,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return serve.Run(cmd.Context(), serveArgs(), cmd.ErrOrStderr())
	},
}

// serveArgs are the server's args from the config.
func serveArgs() serve.Args {
	lifetime, _ := time.ParseDuration(config.Serve.SessionLifetime.Get())
	return serve.Args{
		Host:            config.Serve.Host.Get(),
		Port:            config.Serve.Port.Get(),
		Web:             config.Serve.Web.Get(),
		Auth:            config.Serve.Auth.Get(),
		SessionLifetime: lifetime,
		TrustedProxies:  config.Serve.TrustedProxies.Get(),
		TLSCert:         config.Serve.TLSCert.Get(),
		TLSKey:          config.Serve.TLSKey.Get(),
	}
}

var serveTokenArgs = struct {
	Generate, List bool
	Name, Revoke   string
	Scopes         []string
}{}

func init() {
	serveCmd.AddCommand(serveTokenCmd)
	f := serveTokenCmd.Flags()
	f.BoolVar(&serveTokenArgs.Generate, "generate", false, "Make an API token (with --name), print it once")
	f.StringVar(&serveTokenArgs.Name, "name", "", "The token's name (what it's for: a script, an app)")
	f.StringSliceVar(&serveTokenArgs.Scopes, "scope", nil, "Its permissions: "+strings.Join(core.Scopes, ", ")+" (comma separated or repeated; read by default)")
	f.BoolVar(&serveTokenArgs.List, "list", false, "List the API tokens, with their scopes")
	f.StringVar(&serveTokenArgs.Revoke, "revoke", "", "Remove the named API token")
	serveTokenCmd.MarkFlagsMutuallyExclusive("generate", "list", "revoke")
}

// serveTokenCmd is the token command before "mangal token" (5.24), kept for scripts.
var serveTokenCmd = &cobra.Command{
	Use:        "token",
	Short:      "Manage the API tokens",
	Hidden:     true,
	Deprecated: `use "mangal token add|ls|rm"`,
	Long: `API tokens are for programs calling the API (scripts, "mangal api --server", other
apps): Authorization: Bearer <token>. They aren't logins: people log in as users ("mangal user"). --generate --name NAME --scope read,browse makes one: only its hash is kept,
it's printed once. --list shows them, --revoke NAME removes one (a running server stops
taking it within seconds). Permissions: "mangal user roles --all".`,
	Example: `  mangal serve token --generate --name claude --scope read
  mangal serve token --list
  mangal serve token --revoke claude`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		switch {
		case serveTokenArgs.List:
			return listServeTokens(cmd)
		case serveTokenArgs.Revoke != "":
			return revokeServeToken(cmd, serveTokenArgs.Revoke)
		case serveTokenArgs.Generate:
			if serveTokenArgs.Name == "" {
				return errors.New("--generate needs --name (what the token is for)")
			}
			return generateNamedToken(cmd, serveTokenArgs.Name, serveTokenArgs.Scopes)
		}
		return cmd.Help()
	},
}

func generateNamedToken(cmd *cobra.Command, name string, scopes []string) error {
	t, secret, err := core.NewUsers().AddToken(ctxOf(cmd), name, "", scopes)
	if err != nil {
		return err
	}
	cmd.Println(secret)
	successf(cmd, "saved the API token %q (%s: %s); it isn't shown again", t.Name, core.RoleOf(t.Scopes), strings.Join(t.Scopes, ", "))
	return nil
}

func revokeServeToken(cmd *cobra.Command, name string) error {
	if err := core.NewUsers().RemoveToken(ctxOf(cmd), name); err != nil {
		return err
	}
	successf(cmd, "revoked the API token %q", name)
	return nil
}

func listServeTokens(cmd *cobra.Command) error {
	tokens, err := core.NewUsers().Tokens(ctxOf(cmd))
	if err != nil {
		return err
	}
	for _, t := range tokens {
		owner := ""
		if t.Owner != "" {
			owner = ", " + t.Owner + "'s"
		}
		cmd.Printf("%s  %s: %s  (made %s%s)\n", t.Name, core.RoleOf(t.Scopes), strings.Join(t.Scopes, ", "), t.Created.Format(time.DateOnly), owner)
	}
	if len(tokens) == 0 {
		cmd.Println("no API tokens")
	}
	if config.Serve.Auth.Get() == serve.AuthNone {
		cmd.Println("serve.auth is none: the server takes no tokens")
	}
	return nil
}
