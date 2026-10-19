package cmd

import (
	"strings"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/path"
	"github.com/spf13/cobra"
)

var libraryRetitleArgs = struct {
	From     string
	ID       string
	Apply    bool
	Selector string
}{}

func init() {
	libraryCmd.AddCommand(libraryRetitleCmd)
	f := libraryRetitleCmd.Flags()
	f.StringVar(&libraryRetitleArgs.From, "from", "", "Provider to take the titles from (default: the manga's current source's)")
	f.StringVar(&libraryRetitleArgs.ID, "id", "", "The manga's id, instead of a title query")
	f.BoolVar(&libraryRetitleArgs.Apply, "apply", false, "Retitle, instead of only listing what would happen")
	f.StringVarP(&libraryRetitleArgs.Selector, "manga-selector", "m", "exact", "Manga selector of the search (first|last|exact|closest|<index>)")
	_ = libraryRetitleCmd.RegisterFlagCompletionFunc("from", completionProviderIDs)
}

var libraryRetitleCmd = &cobra.Command{
	Use:   "retitle <id> [title]",
	Short: "Give a library manga's chapters another provider's titles",
	Long: `Give the downloaded chapters of the library manga (id from "mangal library ls") the titles
a provider's manga has for the same chapter numbers: the manga of the library manga's current
source, or with --from the provider's manga with the --id, else the one the manga selector
picks from a search of the title (the library title without one). Every chapter is listed for
this, unavailable and retired ones too; of several with a number (scanlation groups), the
first whose title says more than the number ("Chapter 1").

Each chapter file gets the name the chapter name template gives it with the new title, and a
CBZ (or ZIP) with a ComicInfo.xml gets a new file with its Title changed, every other entry
copied as is; the original goes to the trash. Its download and library chapter get the title.

Only lists what would happen unless --apply is set; "mangal library undo" undoes it.`,
	Example: `  mangal library retitle 12
  mangal library retitle 12 --from mango-mangadex --apply
  mangal library retitle 12 --from mango-mangadex "Dark Gathering"
  mangal library retitle 12 --from mango-mangadex --id 7c145eb7-2ee0-4a82-8d5c-bbb7ba0a4d42`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		st := newStatus(cmd)
		defer st.Done()
		ctx := st.Context(cmd.Context())
		if _, err := path.ExistingDownloads(); err != nil {
			return err
		}
		c, err := newCore()
		if err != nil {
			return err
		}
		id, err := parseMangaID(args[0])
		if err != nil {
			return err
		}
		var mangaID, query string
		st.Print(func() {
			mangaID, query, err = mangaQuery(cmd, "id", libraryRetitleArgs.ID, strings.Join(args[1:], " "))
		})
		if err != nil {
			return err
		}
		ctx = core.WithProgress(ctx, st.Progress)
		req, err := c.RetitleSource(ctx, id, core.RetitleFrom{Provider: libraryRetitleArgs.From, MangaID: mangaID, Query: query, Selector: libraryRetitleArgs.Selector})
		if err != nil {
			return err
		}
		st.Set("planning the retitle")
		plan, err := c.Changes.Plan(ctx, req)
		st.Done()
		if err != nil {
			return err
		}
		d := plan.Detail.(core.RetitleDetail)
		cmd.Printf("titles from %s %q\n", d.Provider.ID, d.From.Title)
		printLines(cmd, plan.Lines)
		if plan.Nothing {
			cmd.Println("nothing to retitle")
			return nil
		}
		if !libraryRetitleArgs.Apply {
			cmd.Println("run with --apply to retitle")
			return nil
		}
		res, err := c.Changes.ApplyPlan(ctx, plan, applyOpts(st))
		st.Done()
		undoHint(cmd, res.Snapshot, err)
		if err != nil {
			return err
		}
		printResultLines(cmd, res.Lines)
		cmd.Printf("retitled %d chapters\n", res.Detail)
		printTrash(cmd)
		return nil
	},
}
