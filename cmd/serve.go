package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

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
(watch.parallel_providers at once; a provider busy elsewhere, e.g. a "mangal watch
check" or a download, is checked once it's free). The watch.* settings are re-read from the config file every
minute. Logs go to stderr and the log file; GET /api/events streams the live progress.

The HTTP API (serve.host:serve.port, 127.0.0.1 by default; /api/v1/, its spec at
/api/v1/openapi.yaml, "mangal api" calls it) is what the TUI and the "mangal watch"
commands use while it runs. Requests need a token (Authorization: Bearer <token>; "mangal
serve token") with the scope of what they do (read, watch, progress, write), except from
the same machine, which gets serve.loopback_scopes (read, watch, progress by default)
without one. Requests from another site's pages (a foreign Origin) are refused, bodies
have to be JSON; for a reverse proxy see serve.allowed_hosts, serve.allowed_origins,
serve.trusted_proxies. One server per data db: a second instance needs another config
(--config) with its own data.path and serve.port.`,
	Example: `  mangal serve
  mangal --config ~/.config/mangal-b/mangal.toml serve --port 6978`,
	GroupID: groupMode,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return serve.Run(cmd.Context(), serve.Args{
			Host:            config.Serve.Host.Get(),
			Port:            config.Serve.Port.Get(),
			Web:             config.Serve.Web.Get(),
			Token:           config.Serve.Token.Get(),
			TokenOnLoopback: config.Serve.TokenOnLoopback.Get(),
			Auth:            config.Serve.Auth.Get(),
			LoopbackScopes:  config.Serve.LoopbackScopes.Get(),
			AllowedHosts:    config.Serve.AllowedHosts.Get(),
			AllowedOrigins:  config.Serve.AllowedOrigins.Get(),
			TrustedProxies:  config.Serve.TrustedProxies.Get(),
			TLSCert:         config.Serve.TLSCert.Get(),
			TLSKey:          config.Serve.TLSKey.Get(),
		}, cmd.ErrOrStderr())
	},
}

var serveTokenArgs = struct {
	Generate, Clear, List bool
	Name, Revoke          string
	Scopes                []string
}{}

func init() {
	serveCmd.AddCommand(serveTokenCmd)
	f := serveTokenCmd.Flags()
	f.BoolVar(&serveTokenArgs.Generate, "generate", false, "Generate a random token, save it and print it")
	f.BoolVar(&serveTokenArgs.Clear, "clear", false, "Remove the saved token")
	f.StringVar(&serveTokenArgs.Name, "name", "", "With --generate, a named token for someone else (only its hash is kept, it's printed once)")
	f.StringSliceVar(&serveTokenArgs.Scopes, "scope", nil, "Scopes of the named token: read, watch, progress, write (comma separated or repeated; read by default)")
	f.BoolVar(&serveTokenArgs.List, "list", false, "List the tokens the server takes, with their scopes")
	f.StringVar(&serveTokenArgs.Revoke, "revoke", "", "Remove the named token")
	serveTokenCmd.MarkFlagsMutuallyExclusive("generate", "clear", "list", "revoke")
}

var serveTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage the tokens of the serve API",
	Long: `Save the token of the serve API with every scope (Authorization: Bearer <token>); the
TUI, "mangal watch" and "mangal api" on the same config send it. It's kept with the logins
(the OS keyring with auth.keyring); serve.token is only used when none is saved. The token
is read from the terminal without echo, or from stdin.

--generate --name NAME --scope read,watch makes a named token for another client (e.g.
read-only for a script or an assistant): only its hash is kept, it's printed once, --list
shows the tokens and --revoke NAME removes one (a running server stops taking it within
seconds). Scopes: read (GET, events), watch (the watches: add, change, check), progress
(the web reader's reads: history, tracker progress), write (the files, the library, the
config, downloads).`,
	Example: `  mangal serve token --generate
  mangal serve token --generate --name claude --scope read
  mangal serve token --list
  mangal serve token --revoke claude`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		switch {
		case serveTokenArgs.List:
			return listServeTokens(cmd)
		case serveTokenArgs.Revoke != "":
			return revokeServeToken(cmd, serveTokenArgs.Revoke)
		case serveTokenArgs.Name != "":
			if !serveTokenArgs.Generate {
				return errors.New("--name needs --generate")
			}
			return generateNamedToken(cmd, serveTokenArgs.Name, serveTokenArgs.Scopes)
		case len(serveTokenArgs.Scopes) != 0:
			return errors.New("--scope is for a named token (--generate --name NAME); the saved token has every scope")
		}
		var token string
		switch {
		case serveTokenArgs.Clear:
			if err := cache.SetServeToken(""); err != nil {
				return err
			}
			successf(cmd, "removed the saved serve token")
			return nil
		case serveTokenArgs.Generate:
			var err error
			if token, err = serve.NewToken(); err != nil {
				return err
			}
		default:
			var err error
			if token, err = readSecret(cmd, bufio.NewReader(cmd.InOrStdin()), "Serve token: "); err != nil {
				return err
			}
			if token == "" {
				return errors.New("no token given (--generate makes one, --clear removes the saved one)")
			}
			if len(token) < serve.MinTokenLen {
				cmd.PrintErrf("warning: the token is short (%d characters), use %d or more (--generate makes one)\n", len(token), serve.MinTokenLen)
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

// reservedTokenNames are the names of the tokens that aren't named ones.
var reservedTokenNames = []string{"default", "config", "loopback", "none"}

func generateNamedToken(cmd *cobra.Command, name string, scopes []string) error {
	if slices.Contains(reservedTokenNames, name) || strings.TrimSpace(name) != name || name == "" {
		return fmt.Errorf("%q can't be a token's name", name)
	}
	if len(scopes) == 0 {
		scopes = []string{serve.ScopeRead}
	}
	scopes, err := serve.NormalizeScopes(scopes)
	if err != nil {
		return err
	}
	tokens, err := cache.ServeTokens()
	if err != nil {
		return err
	}
	if slices.ContainsFunc(tokens, func(t cache.ServeTokenEntry) bool { return t.Name == name }) {
		return fmt.Errorf("there's a token %q already (--revoke %s first)", name, name)
	}
	token, err := serve.NewToken()
	if err != nil {
		return err
	}
	tokens = append(tokens, cache.ServeTokenEntry{Name: name, Hash: serve.HashToken(token), Scopes: scopes, Created: time.Now()})
	if err := cache.SetServeTokens(tokens); err != nil {
		return err
	}
	cmd.Println(token)
	successf(cmd, "saved the token %q (%s); it isn't shown again", name, strings.Join(scopes, ", "))
	return nil
}

func revokeServeToken(cmd *cobra.Command, name string) error {
	tokens, err := cache.ServeTokens()
	if err != nil {
		return err
	}
	i := slices.IndexFunc(tokens, func(t cache.ServeTokenEntry) bool { return t.Name == name })
	if i < 0 {
		return fmt.Errorf("no token %q (--list shows them)", name)
	}
	if err := cache.SetServeTokens(slices.Delete(tokens, i, i+1)); err != nil {
		return err
	}
	successf(cmd, "revoked the token %q", name)
	return nil
}

func listServeTokens(cmd *cobra.Command) error {
	saved, err := cache.ServeToken()
	if err != nil {
		return err
	}
	tokens, err := cache.ServeTokens()
	if err != nil {
		return err
	}
	all := strings.Join(serve.Scopes, ", ")
	if saved != "" {
		cmd.Printf("default  %s  (saved, mangal's own)\n", all)
	}
	if config.Serve.Token.Get() != "" {
		note := "(serve.token)"
		if saved != "" {
			note = "(serve.token; mangal's own commands send the saved one)"
		}
		cmd.Printf("config  %s  %s\n", all, note)
	}
	for _, t := range tokens {
		cmd.Printf("%s  %s  (made %s)\n", t.Name, strings.Join(t.Scopes, ", "), t.Created.Format(time.DateOnly))
	}
	if saved == "" && config.Serve.Token.Get() == "" && len(tokens) == 0 {
		cmd.Println("no tokens")
	}
	if config.Serve.Auth.Get() == serve.AuthNone {
		cmd.Println("serve.auth is none: the server takes no tokens")
	} else if scopes := config.Serve.LoopbackScopes.Get(); len(scopes) != 0 && !config.Serve.TokenOnLoopback.Get() {
		cmd.Printf("without a token, from the same machine: %s (serve.loopback_scopes)\n", strings.Join(scopes, ", "))
	}
	return nil
}
