package watches

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ base.State = (*detailsState)(nil)

// detailsState is a watch: its settings and the history of its checks.
type detailsState struct {
	list.Screen
	// manager is nil until connected, when opened from another screen
	manager watch.Manager
	connect connector
	w       watch.Watch
	status  watch.Status
	backoff int
	size    base.Size
	now     func() time.Time

	open, edit, pause, refresh, library, history, live, relink key.Binding
}

type detailsMsg struct {
	manager watch.Manager
	w       watch.Watch
	runs    []watch.CheckRun
	status  watch.Status
}

func newDetailsState(m watch.Manager, w watch.Watch, backoff int) *detailsState {
	s := &detailsState{
		manager: m,
		connect: Connect,
		w:       w,
		backoff: backoff,
		now:     time.Now,
		open:    util.Bind("details", "enter"),
		edit:    util.Bind("edit", "e"),
		pause:   util.Bind("pause/resume", "p"),
		refresh: util.Bind("refresh", "r"),
		library: util.Bind("library manga", "L"),
		history: util.Bind("provider history", "v"),
		live:    util.Bind("live log", "l"),
		relink:  util.Bind("re-link manga", "m"),
	}
	s.Screen = list.NewScreen(list.New(2, 1, "check", "checks", []*runItem{}, func(i *runItem) _list.DefaultItem { return i }), s.keyMap)
	return s
}

func (s *detailsState) keyMap() help.KeyMap {
	short := []key.Binding{s.open, s.live, s.edit, s.pause, s.library, s.history}
	return util.KeyMap{Short: short, Full: [][]key.Binding{append(short, s.relink, s.refresh)}}
}

func (s *detailsState) Title() base.Title { return base.Title{Text: s.w.Title} }

func (s *detailsState) header() string {
	header := settings(s.w, s.backoff, s.now())
	if ps, ok := s.status.Provider(s.w.Provider); ok {
		line := s.w.Provider + " is being checked (since " + ago(ps.Since, s.now()) + ")"
		if ps.Watch == s.w.ID {
			line = "checking now"
			if ps.State != "" && ps.State != "checking" {
				line += ": " + ps.State
			}
			line += " (l: live log)"
		}
		header += "\n" + style.Normal.Accent.Render(line)
	}
	return header + "\n"
}

func (s *detailsState) Resize(size base.Size) tea.Cmd {
	s.size = size
	size.Height = max(1, size.Height-lipgloss.Height(s.header())-1)
	return s.Screen.Resize(size)
}

func (s *detailsState) Init(ctx context.Context) tea.Cmd { return s.reload(ctx, "Loading the checks") }

func (s *detailsState) reload(ctx context.Context, label string) tea.Cmd {
	m, connect, id := s.manager, s.connect, s.w.ID
	return base.Load(ctx, label, func(ctx context.Context) (detailsMsg, error) {
		var err error
		if m == nil {
			if m, err = connect(ctx); err != nil {
				return detailsMsg{}, err
			}
		}
		w, err := m.Get(ctx, id)
		if err != nil {
			return detailsMsg{}, err
		}
		runs, err := m.Runs(ctx, watch.RunFilter{Watch: id, Limit: maxRuns})
		if err != nil {
			return detailsMsg{}, err
		}
		st, err := m.Status(ctx)
		return detailsMsg{manager: m, w: w, runs: runs, status: st}, err
	}, func(msg detailsMsg) tea.Msg { return msg })
}

// historyItems are the watch's checks in the runs, latest first.
func historyItems(id int64, runs []watch.CheckRun) []_list.Item {
	var items []_list.Item
	for _, run := range runs {
		for _, r := range run.Results {
			if r.Watch == id {
				items = append(items, &runItem{r: r, kind: run.Kind, started: true})
			}
		}
	}
	return items
}

func (s *detailsState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case detailsMsg:
		s.manager, s.w, s.status = msg.manager, msg.w, msg.status
		return tea.Batch(s.List.SetItems(historyItems(s.w.ID, msg.runs)), s.Resize(s.size))
	case base.RestoredMsg:
		return s.reload(ctx, "")
	case actionMsg:
		return base.Load(ctx, msg.label, msg.do, func(notify string) tea.Msg { return changedMsg{notify: notify} })
	case changedMsg:
		return tea.Batch(s.reload(ctx, ""), base.Notify(msg.notify))
	case tea.KeyMsg:
		if s.List.Filtering() {
			break
		}
		m, w := s.manager, s.w
		switch {
		case key.Matches(msg, s.refresh):
			return s.reload(ctx, "Refreshing")
		case key.Matches(msg, s.library):
			return libraryCmd(ctx, w)
		case m == nil:
			// still connecting, the keys below need the manager
		case key.Matches(msg, s.history):
			return func() tea.Msg { return newHistoryState(m, w.Provider) }
		case key.Matches(msg, s.live):
			return func() tea.Msg { return newLiveState(m, &w) }
		case key.Matches(msg, s.edit):
			return func() tea.Msg { return newEditState(m, w) }
		case key.Matches(msg, s.relink):
			return func() tea.Msg { return newRelinkState(m, w) }
		case key.Matches(msg, s.pause):
			return pauseCmd(m, w)
		case key.Matches(msg, s.open):
			if i, ok := s.List.SelectedItem().(*runItem); ok {
				return showRun(i.r)
			}
			return nil
		}
	}
	return s.List.Update(msg)
}

func (s *detailsState) View() string {
	return strings.Join([]string{s.header(), s.List.View()}, "\n")
}
