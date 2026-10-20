package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/path"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/spf13/cobra"
)

var libraryFillArgs = struct {
	From     []string
	ID       string
	Provider string
	MangaID  string
	Chapters string
	Apply    bool
	Selector string
}{}

func init() {
	libraryCmd.AddCommand(libraryFillCmd)
	f := libraryFillCmd.Flags()
	f.StringSliceVar(&libraryFillArgs.From, "from", nil, "Provider to get the chapters from (several: search the title on each and list the candidates)")
	f.StringVar(&libraryFillArgs.ID, "id", "", "The manga's id, instead of a title query")
	f.StringVar(&libraryFillArgs.Provider, "provider", "", "Fill this provider's manga (--manga-id) instead of a library manga's source, in the library or not")
	f.StringVar(&libraryFillArgs.MangaID, "manga-id", "", "The --provider's manga to fill")
	f.StringVarP(&libraryFillArgs.Chapters, "chapters", "c", "", "Only these chapter numbers (12, 10-20, 30-)")
	f.BoolVar(&libraryFillArgs.Apply, "apply", false, "Download them, instead of only listing what would happen")
	f.StringVarP(&libraryFillArgs.Selector, "manga-selector", "m", "exact", "Manga selector of the search (first|last|exact|closest|<index>)")
	_ = libraryFillCmd.MarkFlagRequired("from")
	_ = libraryFillCmd.RegisterFlagCompletionFunc("from", completionProviderIDs)
	_ = libraryFillCmd.RegisterFlagCompletionFunc("provider", completionProviderIDs)
	libraryFillCmd.MarkFlagsRequiredTogether("provider", "manga-id")
}

var libraryFillCmd = &cobra.Command{
	Use:   "fill (<id> | --provider <provider> --manga-id <id>) --from <provider> [title]",
	Short: "Get a library manga's unavailable chapters from another provider",
	Long: `Get the chapters the library manga's current source (id from "mangal library ls") lists
but can't download (unavailable: retired, external, ...) and its folder doesn't have, from
another provider's manga: the --id, else the one the manga selector picks from a search of
the title (the source's title without one). With --provider and --manga-id, it's that
provider's manga instead, in the library or not (its folder is where its downloads go).
Chapters match by number; of several with one,
the first available, as the provider lists them.

They go into the library manga's folder with the source's titles (file name, ComicInfo.xml
Title) and series metadata; their downloads stay recorded as the other provider's (its
chapter URL is the ComicInfo.xml Web, its Notes have a "mangal:" line saying where they're from), and the folder's
source doesn't change.

Only lists what would happen unless --apply is set. With several --from providers, it
searches the title on each and lists the candidates, to run again with one --from and --id.`,
	Example: `  mangal library fill 12 --from mango-mangapill
  mangal library fill 12 --from mango-mangapill --chapters 1-2 --apply
  mangal library fill 12 --from mango-mangapill --id 2085/dark-gathering
  mangal library fill --provider mango-mangadex --manga-id d64b0768-bebe-433c-b923-99870503c0a3 --from mango-mangapill
  mangal library fill 12 --from mango-mangapill,mango-weebcentral`,
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
		req := core.FillReq{Provider: libraryFillArgs.Provider, MangaID: libraryFillArgs.MangaID}
		if req.Provider == "" {
			if len(args) == 0 {
				return errors.New("give a library manga id, or --provider and --manga-id")
			}
			id, err := parseMangaID(args[0])
			if err != nil {
				return err
			}
			args = args[1:]
			lm, err := c.Library.Manga(ctx, id)
			if err != nil {
				return err
			}
			if len(lm.Sources) == 0 {
				return fmt.Errorf("library manga %d has no source", lm.ID)
			}
			src := lm.Sources[0]
			req.Provider, req.MangaID, req.Query = src.Provider, src.MangaID, src.Title
		}
		var mangaID, query string
		st.Print(func() {
			mangaID, query, err = mangaQuery(cmd, "id", libraryFillArgs.ID, strings.Join(args, " "))
		})
		if err != nil {
			return err
		}
		if len(libraryFillArgs.From) > 1 {
			if mangaID != "" {
				return errors.New("--id needs a single --from")
			}
			if query == "" {
				query = req.Query
			}
			if query == "" {
				_, info, found, err := c.Env().ProviderManga(ctx, req.Provider, req.MangaID, "")
				if err != nil {
					return err
				}
				if !found {
					return fmt.Errorf("%s has no manga %q", req.Provider, req.MangaID)
				}
				query = info.Title
			}
			return printSearch(ctx, cmd, st, c, libraryFillArgs.From, query)
		}
		ctx = core.WithProgress(ctx, st.Progress)
		req.Chapters = libraryFillArgs.Chapters
		req.From = core.RetitleFrom{Provider: libraryFillArgs.From[0], MangaID: mangaID, Query: query, Selector: libraryFillArgs.Selector}
		plan, err := c.PlanFill(ctx, req)
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
		id := manager.Add(plan.Job("cli")).ID
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
