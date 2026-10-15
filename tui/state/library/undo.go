package library

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
)

// undoNotice ends the notification of an undoable change.
func undoNotice(notify string) string {
	return notify + " (" + base.KeyUndo.Help().Key + " undoes it)"
}

type undoPlannedMsg struct {
	plan core.Plan
	err  error
}

// undo plans undoing the last library operation, askUndo asks to go ahead.
func (s *mangasState) undo(ctx context.Context) tea.Cmd {
	if base.Busy(ctx) {
		return nil
	}
	return base.LoadWithStatus(ctx, "Planning the undo", func(ctx context.Context, status func(string)) (undoPlannedMsg, error) {
		changes, err := base.Changes()
		if err != nil {
			return undoPlannedMsg{}, err
		}
		plan, err := changes.Plan(base.WithStatus(ctx, status), core.UndoReq{})
		return undoPlannedMsg{plan: plan, err: err}, nil
	}, func(msg undoPlannedMsg) tea.Msg { return msg })
}

func (s *mangasState) askUndo(msg undoPlannedMsg) tea.Cmd {
	if msg.err != nil {
		return undoErr(msg.err)
	}
	plan := msg.plan
	lines := plan.Lines
	if len(plan.Blocked) != 0 {
		report := strings.Join(lines, "\n") + "\n\nCan't undo it: " + strings.Join(plan.Blocked, "; ") + ` ("mangal library undo --force")`
		return base.Viewport("Undo", report, color.Warning)
	}
	op := plan.Detail.(library.UndoPlan).Op
	s.Confirm.Ask("Undo", strings.Join(lines, "\n"), s.View(), func() tea.Msg {
		return actionMsg{label: "Undoing " + op.Kind, do: func(ctx context.Context, status func(string)) (reloadMsg, error) {
			res, err := base.ApplyPlan(ctx, plan, status)
			if err != nil {
				return reloadMsg{}, err
			}
			notify := fmt.Sprintf("undone: %s", op.Summary)
			if kept := res.Detail.(core.UndoDone).Result.Kept; len(kept) != 0 {
				notify += fmt.Sprintf(" (%d folders kept: not empty)", len(kept))
			}
			return reloadMsg{notify: notify}, nil
		}}
	})
	return nil
}

// undoErr is a nothing-to-undo plan as a notification.
func undoErr(err error) tea.Cmd {
	if errors.Is(err, library.ErrNothingToUndo) {
		return base.Notify("nothing to undo")
	}
	return func() tea.Msg { return err }
}
