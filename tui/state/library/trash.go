package library

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/afs"
)

var _ base.State = (*trashState)(nil)

type batchItem struct {
	batch library.TrashBatch
}

func (i *batchItem) FilterValue() string { return i.batch.Name }
func (i *batchItem) Title() string       { return i.batch.Name }
func (i *batchItem) Description() string {
	return fmt.Sprintf("%d files · %.1f MB · %s", i.batch.Files, float64(i.batch.Size)/1e6, i.batch.Path)
}

type trashState struct {
	list    *list.Model
	confirm confirmer
	keyMap  trashKeyMap
}

func newTrashState() *trashState {
	return &trashState{
		list:    list.New(2, 1, "trashed batch", "trashed batches", []*batchItem{}, func(i *batchItem) _list.DefaultItem { return i }),
		confirm: newConfirmer(),
		keyMap:  trashKeyMap{empty: util.Bind("empty", "e")},
	}
}

func (s *trashState) reload() tea.Cmd {
	batches, err := library.Trash(afs.Afero, path.DownloadsDir())
	if err != nil {
		return func() tea.Msg { return err }
	}
	items := make([]_list.Item, len(batches))
	for i, b := range batches {
		items[i] = &batchItem{batch: b}
	}
	return s.list.SetItems(items)
}

func (s *trashState) Intermediate() bool { return false }
func (s *trashState) Backable() bool     { return s.list.Unfiltered() && !s.confirm.active }
func (s *trashState) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(s.keyMap, s.list.KeyMap)
}
func (s *trashState) Title() base.Title { return base.Title{Text: "Trash"} }
func (s *trashState) Subtitle() string  { return "replaced by merges, emptying deletes them for good" }
func (s *trashState) Status() string    { return s.list.Status() }
func (s *trashState) Resize(size base.Size) tea.Cmd {
	s.confirm.size = size
	return s.list.Resize(size)
}

func (s *trashState) Init(ctx context.Context) tea.Cmd {
	return tea.Sequence(s.list.Init(), s.reload())
}

func (s *trashState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, done := s.confirm.update(msg); done {
		return cmd
	}
	if msg, ok := msg.(reloadMsg); ok {
		return tea.Batch(s.reload(), base.Notify(msg.notify))
	}
	if msg, ok := msg.(tea.KeyMsg); ok && key.Matches(msg, s.keyMap.empty) && !s.list.Filtering() {
		if len(s.list.Items()) == 0 {
			return base.Notify("the trash is empty")
		}
		s.confirm.ask("Empty the trash", "Delete everything in the trash? It can't be undone.", s.View(), func() tea.Msg {
			if err := library.EmptyTrash(afs.Afero, path.DownloadsDir()); err != nil {
				return err
			}
			return reloadMsg{notify: "trash emptied"}
		})
		return nil
	}
	return s.list.Update(msg)
}

func (s *trashState) View() string {
	if s.confirm.active {
		return s.confirm.view()
	}
	return s.list.View()
}

type trashKeyMap struct {
	empty key.Binding
}

func (k trashKeyMap) ShortHelp() []key.Binding  { return []key.Binding{k.empty} }
func (k trashKeyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }
