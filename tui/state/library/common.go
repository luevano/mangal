package library

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
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
	// do may tell its status, the label follows it
	do func(status func(string)) (reloadMsg, error)
}

// action is a confirmed change, do returns the notification.
func action(label string, do func() (string, error)) tea.Cmd {
	return func() tea.Msg {
		return actionMsg{label: label, do: func(func(string)) (reloadMsg, error) {
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
	return base.LoadWithStatus(ctx, msg.label, func(_ context.Context, status func(string)) (reloadMsg, error) {
		return msg.do(status)
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
	watches.LibraryScreen = NewManga
}

// NewManga is the screen of a library manga: its chapters.
func NewManga(m storage.Manga) (base.State, error) {
	files, err := client.LibraryFiles()
	if err != nil {
		return nil, err
	}
	return newMangaState(files, m), nil
}

// NewLinks is the links screen of a library manga, planning the links right away (then
// asking) when there are some.
func NewLinks(m storage.Manga, links library.Links) base.State {
	s := newLinksState(m)
	s.autoLinks = links
	return s
}

// NewLinksPick is the links screen of a library manga asking right away which id of its
// link pick of the source to link.
func NewLinksPick(m storage.Manga, source lmmeta.IDCode) base.State {
	s := newLinksState(m)
	s.autoPick = source
	return s
}

// NewMerge is the library's duplicates with keep marked, planning the merge of other into
// it right away (then asking).
func NewMerge(keep, other int64) base.State {
	s := New().(*mangasState)
	s.keep, s.duplicates, s.pendingMerge = keep, true, other
	return s
}
