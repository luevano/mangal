package library

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/state/watches"
)

// actionMsg is a confirmed change to the library (a delete, a merge), run with Load by
// the State. It's a msg and not the dialog's cmd: a Load counts as busy from when it's
// made, the dialog's cmd may never run.
type actionMsg struct {
	label string
	do    func() (reloadMsg, error)
}

// action is a confirmed change, do returns the notification.
func action(label string, do func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		return actionMsg{label: label, do: func() (reloadMsg, error) {
			notify, err := do()
			return reloadMsg{notify: notify}, err
		}}
	}
}

// reloadMsg asks the State to reload, after an action.
type reloadMsg struct {
	notify string
	// merged clears the merge's keep mark
	merged bool
}

func (msg actionMsg) run(ctx context.Context) tea.Cmd {
	return base.Load(ctx, msg.label, func(context.Context) (reloadMsg, error) {
		return msg.do()
	}, func(msg reloadMsg) tea.Msg { return msg })
}

func newDialog() *confirm.Dialog {
	return confirm.NewDialog(40, color.Warning)
}

// withNotify adds the notification of a reload, if any.
func withNotify(cmd tea.Cmd, notify string) tea.Cmd {
	if notify == "" {
		return cmd
	}
	return tea.Batch(cmd, base.Notify(notify))
}

func init() {
	// the watches' screens open a watch's library manga, that package can't import this one
	watches.LibraryScreen = func(m storage.Manga) (base.State, error) {
		files, err := client.LibraryFiles()
		if err != nil {
			return nil, err
		}
		return newMangaState(files, m), nil
	}
}
