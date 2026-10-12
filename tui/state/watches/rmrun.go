package watches

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/list"
)

// runRemover deletes the selected check of a list of checks from the history: d then d
// (or y), after a snapshot of the data db (U in the library undoes it).
type runRemover struct {
	confirm *confirm.Dialog
	key     key.Binding
}

func newRunRemover() *runRemover {
	return &runRemover{confirm: confirm.NewDialog(50, color.Warning), key: base.KeyDeleteRun}
}

type (
	removeRunMsg  struct{ r storage.WatchRun }
	runRemovedMsg struct {
		notify string
		// checkRunGone: its check run had nothing else and went too
		checkRunGone bool
	}
)

// keyMap is the dialog's while it's up, else keys.
func (rr *runRemover) keyMap(keys help.KeyMap) help.KeyMap {
	if rr.confirm.Active() {
		return rr.confirm.KeyMap()
	}
	return keys
}

// update handles the dialog's keys, d on the selected check and the removal; reload is
// the screen's after it. Not handled: false.
func (rr *runRemover) update(ctx context.Context, msg tea.Msg, l *list.Model, m watch.Manager, behind func() string, reload func(checkRunGone bool) tea.Cmd) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
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
		message := fmt.Sprintf("Delete the check of %q from %s (%s, %s) from the history? Its downloads stay. A database snapshot is taken first: U in the library undoes it.",
			r.Title, r.Provider, formatTime(r.Started), result(r))
		rr.confirm.AskDelete("Delete check", message, behind(), func() tea.Msg { return removeRunMsg{r: r} })
		return nil, true
	case removeRunMsg:
		r := msg.r
		return base.Load(ctx, "Deleting the check of "+r.Title, func(ctx context.Context) (runRemovedMsg, error) {
			removed, err := m.RemoveRun(ctx, r.ID)
			notify := fmt.Sprintf("deleted the check of %s from the history (U in the library undoes it)", r.Title)
			if removed.CheckRunRemoved {
				notify = fmt.Sprintf("deleted the check of %s and its check run %d from the history (U in the library undoes it)", r.Title, r.CheckRun)
			}
			return runRemovedMsg{notify: notify, checkRunGone: removed.CheckRunRemoved}, err
		}, func(msg runRemovedMsg) tea.Msg { return msg }), true
	case runRemovedMsg:
		return tea.Batch(reload(msg.checkRunGone), base.Notify(msg.notify)), true
	}
	return nil, false
}
