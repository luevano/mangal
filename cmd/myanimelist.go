package cmd

import (
	"bufio"
	"fmt"

	"github.com/luevano/mangal/v5/auth/oauth"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/tui/base"
	myAnimeListViewer "github.com/luevano/mangal/v5/tui/model/myanimelist"
	"github.com/spf13/cobra"
)

var myAnimeListLoginArgs = struct {
	Browser bool
}{}

func init() {
	rootCmd.AddCommand(myAnimeListCmd)
	myAnimeListCmd.AddCommand(myAnimeListLoginCmd)
	myAnimeListLoginCmd.Flags().BoolVar(&myAnimeListLoginArgs.Browser, "browser", false, "Also open the address in the browser")
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
		cmd.Printf("Logged in to MyAnimeList as %q\n", user.Name())
		return nil
	},
}
