package watches

import (
	"context"
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/list"
)

// runRemover deletes the selected check of a list of checks from the history: d then d
// (or y), after a snapshot of the data db (U in the library undoes it). It asks with the
// list screen's Confirm.
type runRemover struct {
	confirm *confirm.Dialog
	key     key.Binding
}

// newRunRemover sets the screen's Confirm, the remover asks with it.
func newRunRemover(s *list.Screen) *runRemover {
	s.Confirm = confirm.NewDialog(50, color.Warning)
	return &runRemover{confirm: s.Confirm, key: base.KeyDeleteRun}
}

type (
	removeRunMsg  struct{ r storage.WatchRun }
	runRemovedMsg struct {
		notify string
		// checkRunGone: its check run had nothing else and went too
		checkRunGone bool
	}
)

// update handles the dialog's keys, d on the selected check and the removal; reload is
// the screen's after it. Not handled: false.
func (rr *runRemover) update(ctx context.Context, msg tea.Msg, l *list.Model, m watch.Manager, behind func() string, reload func(checkRunGone bool) tea.Cmd) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if rr.confirm.Active() {
			return rr.confirm.Update(msg), true
		}
		if l.Filtering() || m == nil || !key.Matches(msg, rr.key) {
			return nil, false
		}
		i, ok := l.SelectedItem().(*runItem)
		if !ok {
			return nil, true
		}
		r := i.r
		message := fmt.Sprintf("Delete the check of %q from %s (%s, %s) from the history? Its downloads stay. A database snapshot is taken first: %s in the library undoes it.",
			r.Title, r.Provider, watch.FormatTime(r.Started), result(r), base.KeyUndo.Help().Key)
		rr.confirm.AskDelete("Delete check", message, behind(), func() tea.Msg { return removeRunMsg{r: r} })
		return nil, true
	case removeRunMsg:
		r := msg.r
		return base.Load(ctx, "Deleting the check of "+r.Title, func(ctx context.Context) (runRemovedMsg, error) {
			removed, err := m.RemoveRun(ctx, r.ID)
			notify := fmt.Sprintf("deleted the check of %s from the history (%s in the library undoes it)", r.Title, base.KeyUndo.Help().Key)
			if removed.CheckRunRemoved {
				notify = fmt.Sprintf("deleted the check of %s and its check run %d from the history (%s in the library undoes it)", r.Title, r.CheckRun, base.KeyUndo.Help().Key)
			}
			return runRemovedMsg{notify: notify, checkRunGone: removed.CheckRunRemoved}, err
		}, func(msg runRemovedMsg) tea.Msg { return msg }), true
	case runRemovedMsg:
		return tea.Batch(reload(msg.checkRunGone), base.Notify(msg.notify)), true
	}
	return nil, false
}
