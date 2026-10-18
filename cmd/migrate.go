package cmd

import (
	"fmt"
	"strings"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/util/sanitize"
	"github.com/spf13/cobra"
)

var migrateArgs = struct {
	Apply bool
}{}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.Flags().BoolVar(&migrateArgs.Apply, "apply", false, "Migrate, instead of only listing what would change")
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Move data under old provider ids to the current ones",
	Long: `Providers that were renamed keep their old ids in their history, data saved under an
old id still works but can be moved to the current id: the library index entries, the
downloads and the .libmangal.json files of the downloaded mangas get the new id, and the old cache is
dropped (it rebuilds). Folders aren't renamed or moved, "mangal library rename" does that.

Watches move too, and need a re-link: the new site may use other manga ids. The scheduler
skips them until a check finds the manga ("mangal watch check <id>") or they're re-linked
("mangal watch move <id>"); the ones left are listed at the end.

Only lists what would change unless --apply is set.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := newCore()
		if err != nil {
			return err
		}
		status := newStatus(cmd)
		defer status.Done()
		ctx := changeCtx(cmd, status)
		plan, err := c.Changes.Plan(ctx, core.MigrateReq{})
		status.Clear()
		if err != nil {
			return err
		}
		if plan.Nothing {
			cmd.Println("nothing to migrate")
			return nil
		}
		for _, line := range plan.Lines {
			cmd.Println(sanitize.Text(line))
		}
		if !migrateArgs.Apply {
			cmd.Println("run with --apply to migrate")
			return nil
		}
		res, err := c.Changes.ApplyPlan(ctx, plan, applyOpts(status))
		status.Done()
		undoHint(cmd, res.Snapshot, err)
		if err != nil {
			return err
		}
		cmd.Println("migrated")
		return relinkWarning(cmd, c)
	},
}

// relinkWarning lists the watches that need a re-link (e.g. moved from an old provider
// id whose site changed) and how to fix them.
func relinkWarning(cmd *cobra.Command, c *core.Core) error {
	watches, err := c.Watches(ctxOf(cmd))
	if err != nil {
		return err
	}
	all, err := watches.List(ctxOf(cmd))
	if err != nil {
		return err
	}
	var lines []string
	for _, w := range all {
		if w.NeedsRelink {
			lines = append(lines, fmt.Sprintf("  %d  %s (%s %s): %s", w.ID, w.Title, w.Provider, w.MangaID, w.RelinkReason))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	cmd.PrintErrf("warning: %d watches need a re-link, the scheduler skips them until then:\n%s\n", len(lines), sanitize.Text(strings.Join(lines, "\n")))
	cmd.PrintErrf(`check one ("mangal watch check <id>": finding its manga clears it) or re-link it: "mangal watch move <id> [provider] [title] [--id <manga id>]", or %s on the watch in the TUI's watches (%s on the home screen)`+"\n",
		base.KeyRelink.Help().Key, base.KeyWatches.Help().Key)
	return nil
}
