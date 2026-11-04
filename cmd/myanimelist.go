package cmd

import (
	"bufio"
	"errors"
	"fmt"

	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/tui/base"
	myAnimeListViewer "github.com/luevano/mangal/v5/tui/model/myanimelist"
	"github.com/spf13/cobra"
)

var myAnimeListLoginArgs = struct {
	Browser bool
}{}

var myAnimeListClientArgs = struct {
	Clear bool
}{}

func init() {
	rootCmd.AddCommand(myAnimeListCmd)
	myAnimeListCmd.AddCommand(myAnimeListLoginCmd)
	myAnimeListLoginCmd.Flags().BoolVar(&myAnimeListLoginArgs.Browser, "browser", false, "Also open the address in the browser")
	myAnimeListCmd.AddCommand(myAnimeListClientCmd)
	myAnimeListClientCmd.Flags().BoolVar(&myAnimeListClientArgs.Clear, "clear", false, "Remove the saved API client")
}

const myAnimeListSetup = `MyAnimeList needs the client ID of a MyAnimeList API client: register one at
https://myanimelist.net/apiconfig (App Type "other", App Redirect URL
` + oauth.MyAnimeListServerCallbackURL + `) and save its ID with
"mangal myanimelist client" (the secret only for other App Types).`

var myAnimeListCmd = &cobra.Command{
	Use:   "myanimelist",
	Short: "Log in to MyAnimeList",
	Long: `Log in to MyAnimeList, to sync the reading progress (read.history.myanimelist).

` + myAnimeListSetup,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return base.RunStandalone(cmd.Context(), myAnimeListViewer.New())
	},
}

var myAnimeListLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to MyAnimeList without the TUI, also with the browser on another machine",
	Long: `Log in to MyAnimeList for headless use.

The authorize address is printed: open it in any browser and allow mangal. A browser on
this machine comes back to mangal by itself; on another machine the page it ends up on
won't load, paste its address (it has the code) here.

` + myAnimeListSetup,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		in := bufio.NewReader(cmd.InOrStdin())
		user, err := myanimelist.LoginManual(cmd.Context(), manualLogin(cmd, in, myAnimeListLoginArgs.Browser))
		if err != nil {
			return fmt.Errorf("myanimelist login: %w", err)
		}
		cmd.Printf("logged in to MyAnimeList as %q\n", user.Name())
		return nil
	},
}

var myAnimeListClientCmd = &cobra.Command{
	Use:   "client",
	Short: "Save the MyAnimeList API client mangal logs in with",
	Long: `Save the client ID (and secret, only for App Types other than "other") of your
MyAnimeList API client, kept with the logins (auth.keyring keeps them in the OS keyring).
Both are asked, the secret without echo, or read from stdin a line each. On a server it's
the server's client, every user's login goes through (admin).

` + myAnimeListSetup,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return setAPIClient(cmd, "myanimelist", "MyAnimeList", myAnimeListClientArgs.Clear, "Client secret (empty for none): ")
	},
}

// setAPIClient asks for the tracker's API client and saves it (on the connection's server),
// clear removes it.
func setAPIClient(cmd *cobra.Command, source, name string, clear bool, secretLabel string) error {
	rc, err := connectRemote(cmd)
	if err != nil {
		return err
	}
	accounts := core.NewAccounts()
	if rc != nil {
		sayOn(cmd, rc.URL)
		accounts = rc.Services().Accounts
	}
	ctx := cmd.Context()
	if clear {
		if err := accounts.SetAPIClient(ctx, source, "", nil); err != nil {
			return err
		}
		successf(cmd, "removed the %s API client", name)
		return nil
	}
	in := bufio.NewReader(cmd.InOrStdin())
	id, err := prompt(cmd, in, name+" client ID: ")
	if err != nil {
		return err
	}
	if id == "" {
		return errors.New("empty client id, --clear removes the saved one")
	}
	secret, err := readSecret(cmd, in, secretLabel)
	if err != nil {
		return err
	}
	if err := accounts.SetAPIClient(ctx, source, id, &secret); err != nil {
		return err
	}
	successf(cmd, "saved the %s API client", name)
	if clients, err := accounts.APIClients(ctx); err == nil {
		for _, c := range clients {
			if c.Source == source {
				cmd.PrintErrf("its redirect URL must be %s (%s)\n", c.RedirectURL, c.RegisterURL)
			}
		}
	}
	return nil
}
