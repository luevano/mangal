package cmd

import (
	"fmt"
	"strings"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/path"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/spf13/cobra"
)

var libraryFillArgs = struct {
	From     string
	ID       string
	Chapters string
	Apply    bool
	Selector string
}{}

func init() {
	libraryCmd.AddCommand(libraryFillCmd)
	f := libraryFillCmd.Flags()
	f.StringVar(&libraryFillArgs.From, "from", "", "Provider to get the chapters from")
	f.StringVar(&libraryFillArgs.ID, "id", "", "The manga's id, instead of a title query")
	f.StringVarP(&libraryFillArgs.Chapters, "chapters", "c", "", "Only these chapter numbers (12, 10-20, 30-)")
	f.BoolVar(&libraryFillArgs.Apply, "apply", false, "Download them, instead of only listing what would happen")
	f.StringVarP(&libraryFillArgs.Selector, "manga-selector", "m", "exact", "Manga selector of the search (first|last|exact|closest|<index>)")
	_ = libraryFillCmd.MarkFlagRequired("from")
	_ = libraryFillCmd.RegisterFlagCompletionFunc("from", completionProviderIDs)
}

var libraryFillCmd = &cobra.Command{
	Use:   "fill <id> --from <provider> [title]",
	Short: "Get a library manga's unavailable chapters from another provider",
	Long: `Get the chapters the library manga's current source (id from "mangal library ls") lists
but can't download (unavailable: retired, external, ...) and its folder doesn't have, from
another provider's manga: the --id, else the one the manga selector picks from a search of
the title (the source's title without one). Chapters match by number; of several with one,
the first available, as the provider lists them.

They go into the library manga's folder with the source's titles (file name, ComicInfo.xml
Title) and series metadata; their downloads stay recorded as the other provider's (its
chapter URL is the ComicInfo.xml Web, a note says where they're from), and the folder's
source doesn't change.

Only lists what would happen unless --apply is set.`,
	Example: `  mangal library fill 12 --from mango-mangapill
  mangal library fill 12 --from mango-mangapill --chapters 1-2 --apply
  mangal library fill 12 --from mango-mangapill --id 2085/dark-gathering`,
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
			mangaID, query, err = mangaQuery(cmd, "id", libraryFillArgs.ID, strings.Join(args[1:], " "))
		})
		if err != nil {
			return err
		}
		lm, err := c.Library.Manga(ctx, id)
		if err != nil {
			return err
		}
		if len(lm.Sources) == 0 {
			return fmt.Errorf("library manga %d has no source", lm.ID)
		}
		src := lm.Sources[0]
		ctx = core.WithProgress(ctx, st.Progress)
		plan, err := c.PlanFill(ctx, core.FillReq{
			Provider: src.Provider, MangaID: src.MangaID, Query: src.Title, Chapters: libraryFillArgs.Chapters,
			From: core.RetitleFrom{Provider: libraryFillArgs.From, MangaID: mangaID, Query: query, Selector: libraryFillArgs.Selector},
		})
		st.Done()
		if err != nil {
			return err
		}
		if plan.From.ID != "" {
			cmd.Printf("chapters of %s %q from %s %q\n", plan.Provider.ID, plan.Manga.Title, plan.From.ID, plan.FromManga.Title)
		}
		printLines(cmd, plan.Lines())
		if plan.Nothing() {
			cmd.Println("nothing to get")
			return nil
		}
		if !libraryFillArgs.Apply {
			cmd.Println("run with --apply to get them")
			return nil
		}
		// the job the TUI and serve queue, run here
		manager := jobs.New(ctx)
		id = manager.Add(plan.Job("cli")).ID
		var job jobs.Job
		for {
			changed := manager.Changed()
			job, _ = manager.Get(id)
			if !job.Status.Active() {
				break
			}
			if job.Message != "" {
				st.Set(job.Message)
			}
			<-changed
		}
		st.Done()
		for _, r := range job.Results {
			n := stringutil.FormatFloat32(r.Chapter.Info().Number)
			switch {
			case r.Err != nil:
				cmd.PrintErrf("error: chapter %s: %s\n", n, r.Err)
			case r.Down != nil:
				cmd.Println(r.Down.Path())
			}
		}
		if job.Status == jobs.Cancelled {
			return ctx.Err()
		}
		return job.Err
	},
}
