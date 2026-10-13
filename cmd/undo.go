package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
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
		undoer, err := client.LibraryUndoer()
		if err != nil {
			return err
		}
		if libraryUndoArgs.List {
			return listOperations(cmd, undoer)
		}
		var id int64
		if len(args) == 1 {
			if id, err = strconv.ParseInt(args[0], 10, 64); err != nil {
				return fmt.Errorf("invalid operation id %q", args[0])
			}
		}
		plan, err := undoer.Plan(id)
		if errors.Is(err, library.ErrNothingToUndo) {
			cmd.Println("nothing to undo")
			return nil
		}
		if err != nil {
			return err
		}
		root := path.Downloads()
		rel := func(p string) string {
			if r, err := filepath.Rel(root, p); err == nil && isInsideRel(r) {
				return r
			}
			return p
		}
		for _, line := range plan.Lines(rel) {
			cmd.Println(line)
		}
		refusal := plan.Refusal(libraryUndoArgs.Force)
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
		st := newStatus(cmd)
		defer st.Done()
		undoer.OnProgress = st.Progress
		res, snapshot, err := client.UndoContext(cmd.Context(), undoer, plan, libraryUndoArgs.Force)
		st.Done()
		if snapshot != "" {
			cmd.PrintErrf("database snapshot before the undo: %s\n", snapshot)
		}
		for _, s := range res.Skipped {
			cmd.PrintErrf("skipped %s\n", s.Line(rel))
		}
		for _, dir := range res.Kept {
			cmd.Printf("kept %s: not empty\n", rel(dir))
		}
		if err != nil {
			return err
		}
		restored := "the database restored from " + plan.Op.Snapshot
		if !res.Restored {
			restored = "the database not restored (no snapshot), \"mangal library scan\" fixes the library"
		}
		cmd.Printf("undone %d %s: %d steps, %s\n", plan.Op.ID, plan.Op.Kind, res.Undone, restored)
		return nil
	},
}

// isInsideRel is true for a relative path that doesn't leave its directory.
func isInsideRel(rel string) bool {
	return rel != ".." && !filepath.IsAbs(rel) && (len(rel) < 3 || rel[:3] != ".."+string(filepath.Separator))
}

func listOperations(cmd *cobra.Command, undoer library.Undoer) error {
	ops, err := undoer.Journal.Ops(0)
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
