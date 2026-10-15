// Package history is the TUI's read history: the mangas read, last read first, and
// clearing it ("mangal history", "mangal history clear").
package history

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/sanitize"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

const when = "2006-01-02 15:04"

// item is a manga read: its reads, last read first.
type item struct {
	provider, mangaID, title string
	reads                    []storage.Read
}

func (i *item) FilterValue() string { return i.title }
func (i *item) Title() string       { return sanitize.Text(i.title) }
func (i *item) Description() string {
	last := i.reads[0]
	return fmt.Sprintf("%s %s · %s, last ch %g on %s", i.provider, sanitize.Text(i.mangaID),
		stringutil.Quantify(len(i.reads), "chapter", "chapters"), last.Chapter, last.Last.Format(when))
}

type state struct {
	list.Screen
	keys keyMap
}

type keyMap struct {
	open, clear, clearAll key.Binding
}

type loadedMsg []_list.Item

// plannedMsg is a clear planned, to confirm.
type plannedMsg struct{ plan core.Plan }

// clearedMsg is a clear done.
type clearedMsg struct{ notify string }

// New is the read history.
func New() base.State {
	s := &state{keys: keyMap{
		open:     util.Bind("chapters", "enter"),
		clear:    base.KeyDelete,
		clearAll: base.KeyClearAll,
	}}
	s.Screen = list.NewScreen(list.New(2, 1, "manga", "mangas", []*item{}, func(i *item) _list.DefaultItem { return i }), s.keyMap)
	s.Confirm = confirm.NewDialog(40, color.Warning)
	return s
}

func (s *state) keyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.keys.open, s.keys.clear, s.keys.clearAll}}
}

func (s *state) Key() string       { return "history" }
func (s *state) Title() base.Title { return base.Title{Text: "Read history"} }
func (s *state) Subtitle() string {
	return "the chapters read (read.history.local); forgetting them can be undone (U in the library)"
}

func (s *state) Init(ctx context.Context) tea.Cmd { return s.reload(ctx) }

func (s *state) reload(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "", func(context.Context) (loadedMsg, error) {
		c, err := base.Core()
		if err != nil {
			return nil, err
		}
		reads, err := c.Library.History("", "")
		if err != nil {
			return nil, err
		}
		return group(reads), nil
	}, func(items loadedMsg) tea.Msg { return items })
}

// group are the reads by manga, in the order of their last read.
func group(reads []storage.Read) []_list.Item {
	var items []_list.Item
	byKey := map[[2]string]*item{}
	for _, r := range reads {
		k := [2]string{r.Provider, r.MangaID}
		i := byKey[k]
		if i == nil {
			i = &item{provider: r.Provider, mangaID: r.MangaID, title: r.MangaTitle}
			byKey[k] = i
			items = append(items, i)
		}
		i.reads = append(i.reads, r)
	}
	return items
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	switch msg := msg.(type) {
	case loadedMsg:
		return s.List.SetItemsKeep(msg)
	case base.RestoredMsg:
		return s.reload(ctx)
	case plannedMsg:
		return s.ask(msg.plan)
	case clearMsg:
		plan := msg.plan
		return base.LoadWithStatus(ctx, "Forgetting", func(ctx context.Context, status func(string)) (clearedMsg, error) {
			res, err := base.ApplyPlan(ctx, plan, status)
			if err != nil {
				return clearedMsg{}, err
			}
			notify := plan.Summary
			if len(res.Lines) != 0 {
				notify = res.Lines[0].Text
			}
			return clearedMsg{notify: notify + " (" + base.KeyUndo.Help().Key + " in the library undoes it)"}, nil
		}, func(msg clearedMsg) tea.Msg { return msg })
	case clearedMsg:
		return tea.Batch(s.reload(ctx), base.Notify(msg.notify))
	case tea.KeyPressMsg:
		if s.List.Filtering() {
			break
		}
		switch {
		case key.Matches(msg, s.keys.clearAll):
			if len(s.List.Items()) == 0 {
				return base.Notify("No history")
			}
			return s.plan(ctx, core.HistoryClearReq{})
		}
		i, ok := s.List.SelectedItem().(*item)
		if !ok {
			break
		}
		switch {
		case key.Matches(msg, s.keys.open):
			return base.Viewport(i.title, chapters(i.reads), color.Accent)
		case key.Matches(msg, s.keys.clear):
			return s.plan(ctx, core.HistoryClearReq{Provider: i.provider, MangaID: i.mangaID})
		}
	}
	return s.List.Update(msg)
}

// chapters are the manga's chapters read, as "mangal history <provider> <manga-id>" lists them.
func chapters(reads []storage.Read) string {
	lines := make([]string, len(reads))
	for i, r := range reads {
		times := "once"
		if r.Count != 1 {
			times = fmt.Sprintf("%d times", r.Count)
		}
		lines[i] = fmt.Sprintf("vol %g ch %g  %s  read %s, last %s", r.Volume, r.Chapter, sanitize.Text(r.ChapterTitle), times, r.Last.Format(when))
	}
	return strings.Join(lines, "\n")
}

func (s *state) plan(ctx context.Context, req core.HistoryClearReq) tea.Cmd {
	if base.Busy(ctx) {
		return nil
	}
	return base.PlanChange(ctx, "Planning", req, func(p core.Plan) tea.Msg { return plannedMsg{plan: p} })
}

// ask confirms the clear, d or y does it.
func (s *state) ask(plan core.Plan) tea.Cmd {
	if plan.Nothing {
		return base.Notify("No history")
	}
	message := fmt.Sprintf("%s? %s in the library undoes it.", capitalize(plan.Summary), base.KeyUndo.Help().Key)
	s.Confirm.AskDelete("Forget", strings.Join(append(plan.Lines, "", message), "\n"), s.View(), func() tea.Msg {
		return clearMsg{plan: plan}
	})
	return nil
}

// clearMsg is a confirmed clear, applied with Load.
type clearMsg struct{ plan core.Plan }

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
