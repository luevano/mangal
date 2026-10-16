package cmd

import (
	"os"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/tui/base"
	accountsViewer "github.com/luevano/mangal/v5/tui/model/accounts"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var accountsArgs = struct {
	Refresh bool
	Plain   bool
}{}

func init() {
	rootCmd.AddCommand(accountsCmd)
	accountsCmd.Flags().BoolVarP(&accountsArgs.Refresh, "refresh", "r", false, "Fetch the accounts instead of using the cached ones")
	accountsCmd.Flags().BoolVar(&accountsArgs.Plain, "plain", false, "Print plain lines (the default when not in a terminal)")
}

var accountsCmd = &cobra.Command{
	Use:   "accounts",
	Short: "Show the accounts mangal can use",
	Long: `Show every account mangal can use, logged in or not: the Anilist, Kitsu and MyAnimeList
logins and the providers' accounts (e.g. the MangaPlus subscription), cached for 12 hours.
In a terminal they can be refreshed, Anilist, Kitsu or MyAnimeList logged into, or the
MangaPlus secret set.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if accountsArgs.Plain || !term.IsTerminal(int(os.Stdout.Fd())) {
			entries, err := core.NewAccounts().List(cmd.Context(), core.AccountsOptions{Refresh: accountsArgs.Refresh})
			if err != nil {
				return err
			}
			for _, e := range entries {
				fields := []string{e.Service, e.Status}
				if e.User != "" {
					fields = append(fields, "user "+e.User)
				}
				if !e.Expires.IsZero() {
					fields = append(fields, "renews/expires "+e.Expires.Format(time.DateOnly))
				}
				fields = append(fields, e.Details...)
				if e.Error != "" {
					fields = append(fields, "error: "+e.Error)
				}
				cmd.Println(strings.Join(fields, "  "))
			}
			return nil
		}

		return base.RunStandalone(cmd.Context(), accountsViewer.New(accountsArgs.Refresh))
	},
}
