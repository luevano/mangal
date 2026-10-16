package library

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/core"
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
	empty key.Binding
}

type trashLoadedMsg []_list.Item

func newTrashState() *trashState {
	s := &trashState{
		empty: base.KeyEmptyTrash,
	}
	s.Screen = list.NewScreen(list.New(2, 1, "trashed batch", "trashed batches", []*batchItem{}, func(i *batchItem) _list.DefaultItem { return i }), s.keyMap)
	s.Confirm = newDialog()
	return s
}

func (s *trashState) keyMap() help.KeyMap { return util.KeyMap{Short: []key.Binding{s.empty}} }

func (s *trashState) reload(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "", func(ctx context.Context) (trashLoadedMsg, error) {
		lib, err := base.Library()
		if err != nil {
			return nil, err
		}
		batches, err := lib.Trash(ctx)
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
	case tea.KeyPressMsg:
		if s.List.Filtering() || !key.Matches(msg, s.empty) {
			break
		}
		if len(s.List.Items()) == 0 {
			return base.Notify("the trash is empty")
		}
		if base.Busy(ctx) {
			return base.Notify(busyNotice)
		}
		return PlanEmptyTrash(ctx)
	case TrashCheckedMsg:
		if msg.Plan.Nothing {
			return tea.Batch(s.reload(ctx), base.Notify("the trash is empty"))
		}
		plan := msg.Plan
		s.Confirm.AskDelete("Empty the trash", EmptyTrashQuestion("Delete everything in the trash? It can't be undone.", msg), s.View(), func() tea.Msg {
			return actionMsg{label: "Emptying the trash", do: func(ctx context.Context, status func(string)) (reloadMsg, error) {
				notify, err := EmptyTrash(ctx, plan, status)
				return reloadMsg{notify: notify}, err
			}}
		})
		return nil
	}
	return s.List.Update(msg)
}

// TrashCheckedMsg is emptying the trash planned: the operations whose undo it blocks.
type TrashCheckedMsg struct {
	Plan core.Plan
}

// PlanEmptyTrash plans emptying the trash (the operations whose undo needs it), to
// confirm it.
func PlanEmptyTrash(ctx context.Context) tea.Cmd {
	return base.PlanChange(ctx, "Checking the undos", core.EmptyTrashReq{}, func(p core.Plan) tea.Msg { return TrashCheckedMsg{Plan: p} })
}

// EmptyTrashQuestion is question with the undos emptying the trash blocks listed before it.
func EmptyTrashQuestion(question string, msg TrashCheckedMsg) string {
	trash, _ := msg.Plan.Detail.(core.EmptyTrashPlan)
	if len(trash.Blocked) == 0 {
		return question
	}
	var b strings.Builder
	b.WriteString("These operations can't be undone after it:\n")
	for _, op := range trash.Blocked {
		fmt.Fprintf(&b, "  %d  %s  %s  %s\n", op.ID, op.Started.Format(time.DateTime), op.Kind, op.Summary)
	}
	return b.String() + "\n" + question
}

// EmptyTrash deletes what's in the trash for good (the confirmed plan), after a snapshot
// of the database.
func EmptyTrash(ctx context.Context, plan core.Plan, status func(string)) (string, error) {
	if _, err := base.ApplyPlan(ctx, plan, status); err != nil {
		return "", err
	}
	return "trash emptied", nil
}
