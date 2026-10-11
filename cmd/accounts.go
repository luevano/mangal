package cmd

import (
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	accountsViewer "github.com/luevano/mangal/v5/tui/model/accounts"
	anilistViewer "github.com/luevano/mangal/v5/tui/model/anilist"
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
	Short: "Show the accounts mangal uses",
	Long: `Show the accounts mangal uses: the Anilist login and the providers' accounts (e.g. the
MangaPlus subscription), cached for 12 hours. In a terminal they can be refreshed, and
Anilist logged into or the MangaPlus secret set.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if accountsArgs.Plain || !term.IsTerminal(int(os.Stdout.Fd())) {
			entries, err := client.Accounts(cmd.Context(), accountsArgs.Refresh)
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
				if e.Err != nil {
					fields = append(fields, "error: "+e.Err.Error())
				}
				cmd.Println(strings.Join(fields, "  "))
			}
			return nil
		}

		model := accountsViewer.New(cmd.Context(), true)
		if _, err := tea.NewProgram(model).Run(); err != nil {
			return err
		}
		if model.LoginAnilist {
			_, err := tea.NewProgram(anilistViewer.New(cmd.Context(), anilist.Anilist(), true)).Run()
			return err
		}
		return nil
	},
}
