package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/core"
	"github.com/spf13/cobra"
)

var searchArgs = struct {
	Providers []string
}{}

func init() {
	rootCmd.AddCommand(searchCmd)
	searchCmd.Flags().StringSliceVarP(&searchArgs.Providers, "provider", "p", nil, "Provider ids to search (repeat or comma-separate them; every installed one when not given)")
	_ = searchCmd.RegisterFlagCompletionFunc("provider", completionProviderIDs)
}

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search several providers at once",
	Long: `Search the query on the providers at once, printing each provider's results (id, title,
URL) as soon as it answers. "mangal inline json" has the full results of one provider.`,
	Example: `  mangal search dark gathering -p mango-mangadex,mango-mangapill`,
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		st := newStatus(cmd)
		defer st.Done()
		ctx := st.Context(ctxOf(cmd))
		c, err := newCore()
		if err != nil {
			return err
		}
		providers := searchArgs.Providers
		if len(providers) == 0 {
			if providers, err = installedProviderIDs(ctx, c); err != nil {
				return err
			}
		}
		return printSearch(ctx, cmd, st, c, providers, strings.Join(args, " "))
	},
}

// installedProviderIDs are the installed providers' ids.
func installedProviderIDs(ctx context.Context, c *core.Core) ([]string, error) {
	infos, err := c.Providers.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(infos))
	for i, info := range infos {
		ids[i] = info.ID
	}
	return ids, nil
}

// printSearch searches the query on the providers, printing each one's results as they
// come: an error when every provider failed.
func printSearch(ctx context.Context, cmd *cobra.Command, st *status, c *core.Core, providers []string, query string) error {
	pending := len(providers)
	st.Set(fmt.Sprintf("searching %q on %d providers", query, pending))
	failed := 0
	for r := range c.Search(ctx, core.SearchReq{Providers: providers, Query: query}) {
		pending--
		st.Print(func() {
			if r.Err != nil {
				failed++
				cmd.PrintErrf("%s: error: %s\n", r.Provider, r.Err)
				return
			}
			cmd.Printf("%s: %d results\n", providerLabel(r.Provider, r.Info), len(r.Mangas))
			for _, m := range r.Mangas {
				info := m.Info()
				cmd.Printf("  %s\t%s\t%s\n", info.ID, info.Title, info.URL)
			}
		})
		if pending > 0 {
			st.Set(fmt.Sprintf("searching %q: %d providers left", query, pending))
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if failed != 0 && failed == len(providers) {
		return fmt.Errorf("search %q: every provider failed", query)
	}
	return nil
}

// providerLabel is the provider's name and id, the id alone when it isn't known.
func providerLabel(id string, info libmangal.ProviderInfo) string {
	if info.Name == "" {
		return id
	}
	return fmt.Sprintf("%s (%s)", info.Name, info.ID)
}
