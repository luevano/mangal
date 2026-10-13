package library

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
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
	list.Screen
	files library.Files
	empty key.Binding
}

type trashLoadedMsg []_list.Item

func newTrashState(files library.Files) *trashState {
	s := &trashState{
		files: files,
		empty: base.KeyEmptyTrash,
	}
	s.Screen = list.NewScreen(list.New(2, 1, "trashed batch", "trashed batches", []*batchItem{}, func(i *batchItem) _list.DefaultItem { return i }), s.keyMap)
	s.Confirm = newDialog()
	return s
}

func (s *trashState) keyMap() help.KeyMap { return util.KeyMap{Short: []key.Binding{s.empty}} }

func (s *trashState) reload(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "", func(context.Context) (trashLoadedMsg, error) {
		batches, err := library.Trash(s.files.FS, s.files.Root)
		if err != nil {
			return nil, err
		}
		items := make([]_list.Item, len(batches))
		for i, b := range batches {
			items[i] = &batchItem{batch: b}
		}
		return items, nil
	}, func(items trashLoadedMsg) tea.Msg { return items })
}

func (s *trashState) Key() string       { return "trash" }
func (s *trashState) Title() base.Title { return base.Title{Text: "Trash"} }
func (s *trashState) Subtitle() string {
	return "deleted chapters and mangas, and the copies merges replaced; emptying deletes them for good"
}
func (s *trashState) Init(ctx context.Context) tea.Cmd {
	return s.reload(ctx)
}

func (s *trashState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	switch msg := msg.(type) {
	case trashLoadedMsg:
		return s.List.SetItemsKeep(msg)
	case actionMsg:
		return msg.run(ctx)
	case reloadMsg:
		return withNotify(s.reload(ctx), msg.notify)
	case tea.KeyMsg:
		if s.List.Filtering() || !key.Matches(msg, s.empty) {
			break
		}
		if len(s.List.Items()) == 0 {
			return base.Notify("the trash is empty")
		}
		if base.Busy(ctx) {
			return base.Notify(busyNotice)
		}
		files := s.files
		s.Confirm.AskDelete("Empty the trash", "Delete everything in the trash? It can't be undone.", s.View(), action("Emptying the trash", func() (string, error) {
			return EmptyTrash(files)
		}))
		return nil
	}
	return s.List.Update(msg)
}

// EmptyTrash deletes what's in the trash for good, after a snapshot of the database.
func EmptyTrash(files library.Files) (string, error) {
	if _, err := client.Snapshot("empty-trash"); err != nil {
		return "", err
	}
	return "trash emptied", library.EmptyTrash(files.FS, files.Root)
}
