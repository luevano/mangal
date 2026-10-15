package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/spf13/cobra"
)

var libraryUndoArgs = struct {
	List  bool
	Apply bool
	Force bool
}{}

func init() {
	libraryCmd.AddCommand(libraryUndoCmd)
	libraryUndoCmd.Flags().BoolVarP(&libraryUndoArgs.List, "list", "l", false, "List the operations that can be undone")
	libraryUndoCmd.Flags().BoolVar(&libraryUndoArgs.Apply, "apply", false, "Undo, instead of only listing what would happen")
	libraryUndoCmd.Flags().BoolVar(&libraryUndoArgs.Force, "force", false, "Undo even if the library changed since, steps can't be undone or the snapshot is gone")
}

var libraryUndoCmd = &cobra.Command{
	Use:   "undo [id]",
	Short: "Undo the last library operation",
	Long: `Undo the last library operation (rename, merge, move, scan --mark, migrate, a delete in
the TUI) not undone yet, or the one with the id (from --list) once the newer ones are
undone: its file changes are reversed, newest first, and the data database is restored
from the snapshot taken before it (data.backups snapshots are kept). Logins, search
history and title bindings aren't restored.

Nothing is ever overwritten: a step whose file is gone or whose old place is taken
again is listed and refuses the undo. It's refused too when the library changed since
(a download, a read, a watch check, ...) or while it ran (another mangal, e.g. "mangal
serve", or a download): restoring the snapshot would lose that. Publication statuses and
the link picks of "library bind" don't count, they're looked up again.
--force undoes it anyway, skipping those steps; "mangal library scan" then fixes the
library. Emptying the trash can't be undone.

Only lists what would happen unless --apply is set.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newCore()
		if err != nil {
			return err
		}
		if libraryUndoArgs.List {
			return listOperations(cmd, c)
		}
		var id int64
		if len(args) == 1 {
			if id, err = strconv.ParseInt(args[0], 10, 64); err != nil {
				return fmt.Errorf("invalid operation id %q", args[0])
			}
		}
		st := newStatus(cmd)
		defer st.Done()
		ctx := changeCtx(cmd, st)
		plan, err := c.Changes.Plan(ctx, core.UndoReq{ID: id})
		if errors.Is(err, library.ErrNothingToUndo) {
			cmd.Println("nothing to undo")
			return nil
		}
		if err != nil {
			return err
		}
		printLines(cmd, plan.Lines)
		refusal := plan.Detail.(library.UndoPlan).Refusal(libraryUndoArgs.Force)
		if !libraryUndoArgs.Apply {
			if refusal != nil {
				cmd.PrintErrf("can't undo it: %s\n", refusal)
				return nil
			}
			cmd.Println("run with --apply to undo")
			return nil
		}
		if refusal != nil {
			return refusal
		}
		opts := applyOpts(st)
		opts.Force = libraryUndoArgs.Force
		res, err := c.Changes.ApplyPlan(ctx, plan, opts)
		st.Done()
		if res.Snapshot != "" {
			cmd.PrintErrf("database snapshot before the undo: %s\n", res.Snapshot)
		}
		printResultLines(cmd, res.Lines)
		if err != nil {
			return err
		}
		cmd.Println(res.Detail.(core.UndoDone).Line())
		return nil
	},
}

func listOperations(cmd *cobra.Command, c *core.Core) error {
	ops, err := c.Library.Ops(0)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		cmd.Println("no operations in the journal")
		return nil
	}
	for _, op := range ops {
		state := ""
		switch {
		case !op.Undone.IsZero():
			state = "  undone " + op.Undone.Format(time.DateTime)
		case op.Finished.IsZero():
			state = "  never finished"
		case op.Error != "":
			state = "  stopped midway: " + op.Error
		case op.Interfered != "":
			state = "  " + op.Interfered
		}
		cmd.Printf("%d  %s  %s  %s  %d steps  %s%s\n", op.ID, op.Started.Format(time.DateTime), op.Kind, op.Summary, op.Steps, op.Snapshot, state)
	}
	return nil
}
