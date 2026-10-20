package cmd

import (
	"errors"
	"fmt"

	"github.com/luevano/mangal/v5/core"
	"github.com/spf13/cobra"
)

var libraryURLsArgs = struct {
	All bool
}{}

func init() {
	libraryCmd.AddCommand(libraryURLsCmd)
	libraryURLsCmd.Flags().BoolVar(&libraryURLsArgs.All, "all", false, "Look every source up again (refresh the stored urls, ask for the gone ones), not only those without one")
}

var libraryURLsCmd = &cobra.Command{
	Use:   "urls",
	Short: "Look the web pages of the library's sources up",
	Long: `Look the web page (url) of each library source (a provider's manga) without one up on its
provider, by its manga id, and record it; a source the provider doesn't have any more is
marked gone (its old url kept) and isn't asked again. --all looks every source up again:
stale urls are refreshed, gone sources asked again. Downloads, moves and scans record the url
when they know it, this is for the sources recorded before.

A provider is asked one manga at a time with a pause of jobs.delay_min to jobs.delay_max
between them, watch.parallel_providers providers at once; the sources of providers not
installed or off in jobs.urls.providers (MangaPlus by default) are skipped. Prints each
source looked up and what was found.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		st := newStatus(cmd)
		defer st.Done()
		c, err := newCore()
		if err != nil {
			return err
		}
		rep, err := c.Library.LookupSourceURLs(changeCtx(cmd, st), core.SourceURLsOptions{All: libraryURLsArgs.All})
		st.Clear()
		for _, s := range rep.Skipped {
			cmd.Println("skipped " + s)
		}
		var filled, gone, failed int
		for _, r := range rep.Results {
			line := fmt.Sprintf("%d  %s  %s %s  ", r.Manga, r.Title, r.Provider, r.MangaID)
			switch {
			case r.Err == nil:
				filled++
				line += r.URL
			case errors.Is(r.Err, core.ErrNotFound):
				gone++
				line += "gone: " + r.Err.Error()
			default:
				failed++
				line += "error: " + r.Err.Error()
			}
			cmd.Println(line)
		}
		if len(rep.Results) == 0 && err == nil {
			cmd.Println("no source to look up")
			return nil
		}
		cmd.Printf("%d found, %d gone, %d failed\n", filled, gone, failed)
		return err
	},
}
