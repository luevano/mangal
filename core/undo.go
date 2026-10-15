package core

import (
	"context"
	"fmt"

	"github.com/luevano/mangal/v5/library"
)

// UndoReq undoes the last library operation not undone, or the one with the ID once the
// newer ones are undone: its file changes reversed, the database restored from its
// snapshot. Planning it with nothing to undo is library.ErrNothingToUndo.
type UndoReq struct {
	ID int64 `json:"id,omitempty"`
}

func (UndoReq) Kind() Kind { return KindUndo }

// UndoDone is what an undo did.
type UndoDone struct {
	Plan   library.UndoPlan
	Result library.UndoResult
}

// Line is "undone 4 merge: 3 steps, the database restored from <snapshot>".
func (d UndoDone) Line() string {
	restored := "the database restored from " + d.Plan.Op.Snapshot
	if !d.Result.Restored {
		restored = "the database not restored (no snapshot), \"mangal library scan\" fixes the library"
	}
	return fmt.Sprintf("undone %d %s: %d steps, %s", d.Plan.Op.ID, d.Plan.Op.Kind, d.Result.Undone, restored)
}

func (r UndoReq) plan(_ context.Context, c *Core) (*prepared, error) {
	undoer := c.undoer()
	up, err := undoer.Plan(r.ID)
	if err != nil {
		return nil, err
	}
	p := Plan{Summary: "undo " + up.Op.Summary, Lines: up.Lines(c.rel), Detail: up}
	if err := up.Refusal(false); err != nil {
		p.Blocked = []string{err.Error()}
		// forced, the later changes are lost
		p.Forceable, p.Irreversible = up.Refusal(true) == nil, true
	}
	return &prepared{
		plan:   p,
		tables: append(append([]string{}, journalTables...), libraryTables...),
		refuse: up.Refusal,
		apply: func(ctx context.Context, opts ApplyOpts, res *OpResult) error {
			undoer.OnProgress = progressOf(ctx)
			ur, snapshot, err := c.ops.Undo(ctx, undoer, up, opts.Force)
			res.Snapshot, res.Detail = snapshot, UndoDone{Plan: up, Result: ur}
			for _, s := range ur.Skipped {
				opts.line(res, "skipped "+s.Line(c.rel), true)
			}
			for _, dir := range ur.Kept {
				opts.line(res, fmt.Sprintf("kept %s: not empty", c.rel(dir)), false)
			}
			return err
		},
	}, nil
}
