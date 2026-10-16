package cmd

import (
	"fmt"
	"time"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/sanitize"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(historyCmd)
	historyCmd.AddCommand(historyClearCmd)
	historyClearCmd.Flags().BoolVarP(&historyClearArgs.Yes, "yes", "y", false, "Clear without asking")
}

// providerManga are the optional [provider [manga-id]] args, empty for any.
func providerManga(args []string) (provider, mangaID string) {
	if len(args) > 0 {
		provider = args[0]
	}
	if len(args) > 1 {
		mangaID = args[1]
	}
	return provider, mangaID
}

var historyCmd = &cobra.Command{
	Use:   "history [provider [manga-id]]",
	Short: "List the chapters read",
	Long: `List the mangas read, last read first, or the chapters read of one provider or manga
(provider and manga ids as shown in the list). Reads are recorded when read.history.local
is set (the default).`,
	Args:              cobra.MaximumNArgs(2),
	ValidArgsFunction: completionFirstProvider,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newCore()
		if err != nil {
			return err
		}
		provider, mangaID := providerManga(args)
		reads, err := c.Library.History(ctxOf(cmd), provider, mangaID)
		if err != nil {
			return err
		}
		if len(reads) == 0 {
			cmd.Println("no history")
			return nil
		}

		const when = "2006-01-02 15:04"
		if mangaID != "" {
			for _, r := range reads {
				cmd.Printf("vol %g ch %g  %s  read %s, last %s\n", r.Volume, r.Chapter, sanitize.Text(r.ChapterTitle), times(r.Count), r.Last.Format(when))
			}
			return nil
		}

		type manga struct {
			provider, id, title string
			chapters            int
			// last is the last chapter read (reads are last read first)
			last     float32
			lastRead time.Time
		}
		var mangas []*manga
		byKey := map[[2]string]*manga{}
		for _, r := range reads {
			key := [2]string{r.Provider, r.MangaID}
			m := byKey[key]
			if m == nil {
				m = &manga{provider: r.Provider, id: r.MangaID, title: r.MangaTitle, last: r.Chapter, lastRead: r.Last}
				byKey[key] = m
				mangas = append(mangas, m)
			}
			m.chapters++
		}
		for _, m := range mangas {
			cmd.Printf("%s  %s %s  %s, last ch %g on %s\n", sanitize.Text(m.title), m.provider, sanitize.Text(m.id), stringutil.Quantify(m.chapters, "chapter", "chapters"), m.last, m.lastRead.Format(when))
		}
		return nil
	},
}

func times(n int) string {
	if n == 1 {
		return "once"
	}
	return fmt.Sprintf("%d times", n)
}

var historyClearArgs = struct {
	Yes bool
}{}

var historyClearCmd = &cobra.Command{
	Use:               "clear [provider [manga-id]]",
	Short:             "Forget the chapters read",
	Long:              `Forget every chapter read, or those of one provider or manga. "mangal library undo" brings them back.`,
	Args:              cobra.MaximumNArgs(2),
	ValidArgsFunction: completionFirstProvider,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newCore()
		if err != nil {
			return err
		}
		provider, mangaID := providerManga(args)
		plan, err := c.Changes.Plan(ctxOf(cmd), core.HistoryClearReq{Provider: provider, MangaID: mangaID})
		if err != nil {
			return err
		}
		if plan.Nothing {
			cmd.Println("no history")
			return nil
		}
		reads := plan.Detail.([]storage.Read)
		if !historyClearArgs.Yes && !confirm(cmd, fmt.Sprintf("Forget %s?", stringutil.Quantify(len(reads), "read chapter", "read chapters"))) {
			cmd.Println("nothing cleared (--yes clears without asking)")
			return nil
		}
		st := newStatus(cmd)
		defer st.Done()
		res, err := c.Changes.ApplyPlan(ctxOf(cmd), plan, applyOpts(st))
		st.Done()
		undoHint(cmd, res.Snapshot, err)
		if err != nil {
			return err
		}
		printResultLines(cmd, res.Lines)
		return nil
	},
}
