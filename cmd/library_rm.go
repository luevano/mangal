package cmd

import (
	"github.com/luevano/mangal/v5/core"
	"github.com/spf13/cobra"
)

var libraryRmArgs = struct {
	Apply bool
}{}

func init() {
	libraryCmd.AddCommand(libraryRmCmd)
	libraryRmCmd.Flags().BoolVar(&libraryRmArgs.Apply, "apply", false, "Delete, instead of only listing what would happen")
}

var libraryRmCmd = &cobra.Command{
	Use:   "rm <id> [chapter]",
	Short: "Delete a library manga, or one of its chapters",
	Long: `Delete the library manga (id from "mangal library ls"): its folder goes to the trash (only
its chapters when it shares the folder) and it leaves the library. With a chapter (its
number, file name or path, as "mangal library downloads" lists them) only that chapter
file goes. A manga whose folder is gone is only forgotten. "mangal library undo" undoes it,
until the trash is emptied.

Only lists what would happen unless --apply is set.`,
	Example: `  mangal library rm 12
  mangal library rm 12 --apply
  mangal library rm 12 3 --apply
  mangal library rm 12 "[0003.0] Chapter 3.cbz" --apply`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := parseMangaID(args[0])
		if err != nil {
			return err
		}
		req := core.DeleteReq{Manga: id}
		if len(args) == 2 {
			req.Chapter = args[1]
		}
		c, err := newCore()
		if err != nil {
			return err
		}
		st := newStatus(cmd)
		defer st.Done()
		ctx := changeCtx(cmd, st)
		plan, err := c.Changes.Plan(ctx, req)
		if err != nil {
			return err
		}
		printLines(cmd, plan.Lines)
		if !libraryRmArgs.Apply {
			cmd.Println("run with --apply to delete")
			return nil
		}
		res, err := c.Changes.ApplyPlan(ctx, plan, applyOpts(st))
		st.Done()
		undoHint(cmd, res.Snapshot, err)
		if err != nil {
			return err
		}
		if plan.Detail.(core.DeletePlan).Missing {
			cmd.Println("forgotten")
		} else {
			cmd.Println("deleted (in the trash until emptied)")
		}
		return nil
	},
}
