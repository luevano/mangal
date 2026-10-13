package library

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
)

// journaled runs an undoable library operation (snapshot first, file changes journaled),
// the notification says how to undo it.
func journaled(kind, summary, notify string, do func(library.Journal) error) (string, error) {
	_, err := client.Operation(kind, summary, do)
	if err != nil {
		return "", err
	}
	return undoNotice(notify), nil
}

// undoNotice ends the notification of an undoable change.
func undoNotice(notify string) string {
	return notify + " (" + base.KeyUndo.Help().Key + " undoes it)"
}

type undoPlannedMsg struct {
	undoer library.Undoer
	plan   library.UndoPlan
	err    error
}

// undo plans undoing the last library operation, askUndo asks to go ahead.
func (s *mangasState) undo(ctx context.Context) tea.Cmd {
	if base.Busy(ctx) {
		return nil
	}
	return base.Load(ctx, "Planning the undo", func(context.Context) (undoPlannedMsg, error) {
		undoer, err := client.LibraryUndoer()
		if err != nil {
			return undoPlannedMsg{}, err
		}
		plan, err := undoer.Plan(0)
		return undoPlannedMsg{undoer: undoer, plan: plan, err: err}, nil
	}, func(msg undoPlannedMsg) tea.Msg { return msg })
}

func (s *mangasState) askUndo(msg undoPlannedMsg) tea.Cmd {
	if msg.err != nil {
		return undoErr(msg.err)
	}
	root := s.files.Root
	rel := func(p string) string {
		if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
		return p
	}
	lines := msg.plan.Lines(rel)
	if err := msg.plan.Refusal(false); err != nil {
		report := strings.Join(lines, "\n") + "\n\nCan't undo it: " + err.Error() + ` ("mangal library undo --force")`
		return base.Viewport("Undo", report, color.Warning)
	}
	undoer, plan := msg.undoer, msg.plan
	s.Confirm.Ask("Undo", strings.Join(lines, "\n"), s.View(), func() tea.Msg {
		return actionMsg{label: "Undoing " + plan.Op.Kind, do: func(ctx context.Context, status func(string)) (reloadMsg, error) {
			undoer.OnProgress = func(p library.Progress) { status(p.String()) }
			res, _, err := client.UndoContext(ctx, undoer, plan, false)
			if err != nil {
				return reloadMsg{}, err
			}
			notify := fmt.Sprintf("undone: %s", plan.Op.Summary)
			if len(res.Kept) != 0 {
				notify += fmt.Sprintf(" (%d folders kept: not empty)", len(res.Kept))
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
