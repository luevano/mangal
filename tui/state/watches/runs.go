package watches

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// maxRuns is how many check runs the screens show.
const maxRuns = 200

// runItem is the check of a watch in a run.
type runItem struct {
	r storage.WatchRun
	// kind is the check run's, shown in a watch's history
	kind string
	// watchTitle shows the manga, in a run's results and a provider's history
	watchTitle bool
	// started shows when, in the histories
	started bool
}

func (i *runItem) FilterValue() string {
	return strings.Join([]string{i.r.Title, i.r.Provider, result(i.r), i.kind, formatTime(i.r.Started), chapterNumbers(i.r)}, " ")
}

func (i *runItem) Title() string {
	res := result(i.r)
	if len(i.r.Chapters) != 0 {
		res += " (" + chapterNumbers(i.r) + ")"
	}
	var parts []string
	if i.started {
		parts = append(parts, formatTime(i.r.Started))
	}
	if i.watchTitle {
		parts = append(parts, i.r.Title)
	}
	return strings.Join(append(parts, res), " · ")
}

func (i *runItem) Description() string {
	var parts []string
	if i.watchTitle {
		parts = append(parts, i.r.Provider)
	}
	if i.kind != "" {
		parts = append(parts, i.kind)
	}
	parts = append(parts, "from "+formatNumber(i.r.From))
	if i.r.Error != "" {
		parts = append(parts, i.r.Error)
	}
	return strings.Join(parts, " · ")
}

// showRun shows the check of a watch in full.
func showRun(r storage.WatchRun) tea.Cmd {
	c := color.Viewport
	if r.Status == storage.StatusError {
		c = color.Error
	}
	return base.Viewport(r.Title, runReport(r), c)
}

// checkRunItem is a check run.
type checkRunItem struct{ run watch.CheckRun }

func (i *checkRunItem) FilterValue() string { return i.run.Kind + " " + formatTime(i.run.Started) }

func (i *checkRunItem) Title() string {
	return formatTime(i.run.Started) + " · " + i.run.Kind
}

func (i *checkRunItem) Description() string {
	r := i.run
	parts := []string{
		stringutil.Quantify(r.Watches, "watch", "watches"),
		stringutil.Quantify(r.NewChapters, "new chapter", "new chapters"),
	}
	if r.Errors > 0 {
		parts = append(parts, stringutil.Quantify(r.Errors, "error", "errors"))
	}
	if r.Finished.IsZero() {
		parts = append(parts, "running")
	} else {
		parts = append(parts, "took "+r.Finished.Sub(r.Started).Round(time.Second).String())
	}
	return strings.Join(parts, " · ")
}

var _ base.State = (*runsState)(nil)

// runsState is every check run, latest first.
type runsState struct {
	list.Screen
	manager watch.Manager
	open    key.Binding
	refresh key.Binding
	history key.Binding
}

type runsMsg []watch.CheckRun

func newRunsState(m watch.Manager) *runsState {
	s := &runsState{manager: m, open: util.Bind("results", "enter"), refresh: base.KeyRefresh, history: util.Bind("history", "v")}
	s.Screen = list.NewScreen(list.New(2, 1, "check run", "check runs", []*checkRunItem{}, func(i *checkRunItem) _list.DefaultItem { return i }), s.keyMap)
	return s
}

func (s *runsState) keyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.open, s.history, s.refresh}}
}

func (s *runsState) Title() base.Title { return base.Title{Text: "Check runs"} }

func (s *runsState) Init(ctx context.Context) tea.Cmd { return s.reload(ctx, "Loading the check runs") }

func (s *runsState) reload(ctx context.Context, label string) tea.Cmd {
	m := s.manager
	return base.Load(ctx, label, func(ctx context.Context) (runsMsg, error) {
		return m.Runs(ctx, watch.RunFilter{Limit: maxRuns})
	}, func(runs runsMsg) tea.Msg { return runs })
}

func (s *runsState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case runsMsg:
		items := make([]_list.Item, len(msg))
		for i, r := range msg {
			items[i] = &checkRunItem{run: r}
		}
		return s.List.SetItems(items)
	case tea.KeyMsg:
		if s.List.Filtering() {
			break
		}
		switch {
		case key.Matches(msg, s.refresh):
			return s.reload(ctx, "Refreshing")
		case key.Matches(msg, s.history):
			m := s.manager
			return func() tea.Msg { return newHistoryState(m, "") }
		case key.Matches(msg, s.open):
			if i, ok := s.List.SelectedItem().(*checkRunItem); ok {
				m, id := s.manager, i.run.ID
				return func() tea.Msg { return newRunState(m, id) }
			}
			return nil
		}
	}
	return s.List.Update(msg)
}

var _ base.State = (*runState)(nil)

// runState is a check run's results, one per watch.
type runState struct {
	list.Screen
	manager watch.Manager
	id      int64
	run     watch.CheckRun

	open, history, library key.Binding
}

type runMsg watch.CheckRun

func newRunState(m watch.Manager, id int64) *runState {
	s := &runState{
		manager: m,
		id:      id,
		open:    util.Bind("details", "enter"),
		history: base.KeyProviderHistory,
		library: base.KeyLibraryManga,
	}
	s.Screen = list.NewScreen(list.New(2, 1, "watch", "watches", []*runItem{}, func(i *runItem) _list.DefaultItem { return i }), s.keyMap)
	return s
}

func (s *runState) keyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.open, s.library, s.history}}
}

func (s *runState) Title() base.Title {
	return base.Title{Text: fmt.Sprintf("Check run %d", s.id)}
}

func (s *runState) Subtitle() string {
	if s.run.ID == 0 {
		return s.List.Subtitle()
	}
	return (&checkRunItem{run: s.run}).Title() + " · " + (&checkRunItem{run: s.run}).Description()
}

func (s *runState) Init(ctx context.Context) tea.Cmd {
	m, id := s.manager, s.id
	return base.Load(ctx, "Loading the check run", func(ctx context.Context) (runMsg, error) {
		run, err := m.RunDetails(ctx, id)
		return runMsg(run), err
	}, func(run runMsg) tea.Msg { return run })
}

func (s *runState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case runMsg:
		s.run = watch.CheckRun(msg)
		items := make([]_list.Item, len(s.run.Results))
		for i, r := range s.run.Results {
			items[i] = &runItem{r: r, watchTitle: true}
		}
		return s.List.SetItems(items)
	case tea.KeyMsg:
		if s.List.Filtering() {
			break
		}
		if cmd, ok := runItemKey(ctx, msg, s.List, s.manager, s.open, s.library, s.history); ok {
			return cmd
		}
	}
	return s.List.Update(msg)
}

// runItemKey handles the keys on the selected check of a list of checks: open shows it,
// library opens its library manga, history its provider's history.
func runItemKey(ctx context.Context, msg tea.KeyMsg, l *list.Model, m watch.Manager, open, library, history key.Binding) (tea.Cmd, bool) {
	i, ok := l.SelectedItem().(*runItem)
	if !ok {
		return nil, key.Matches(msg, open, library, history)
	}
	r := i.r
	switch {
	case key.Matches(msg, open):
		return showRun(r), true
	case key.Matches(msg, library):
		return libraryCmd(ctx, watch.Watch{Watch: storage.Watch{ID: r.Watch, Provider: r.Provider, MangaID: r.MangaID, Title: r.Title}}), true
	case key.Matches(msg, history):
		return func() tea.Msg { return newHistoryState(m, r.Provider) }, true
	}
	return nil, false
}
