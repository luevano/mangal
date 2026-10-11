package watches

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

const (
	// pollChecking is how often the list reloads while a check runs, poll otherwise
	pollChecking = 3 * time.Second
	poll         = 30 * time.Second
	// startGrace is how long a check just started counts as running before the status
	// says so
	startGrace = 2 * time.Second
	// lastRuns is how many check runs the last result of each watch is looked for in
	lastRuns = 50
)

var _ base.State = (*listState)(nil)

type item struct {
	w watch.Watch
	// last is its latest check, nil if none in the recent runs
	last    *storage.WatchRun
	backoff int
	now     time.Time
}

func (i *item) FilterValue() string { return i.w.Title + " " + i.w.Provider }

func (i *item) Title() string {
	if !i.w.Enabled {
		return i.w.Title + " (paused)"
	}
	return i.w.Title
}

func (i *item) Description() string {
	w := i.w
	parts := []string{
		w.Provider,
		"last " + formatNumber(w.LastDownloaded),
		"checked " + ago(w.LastChecked, i.now),
		"next " + nextCheck(w, i.backoff, i.now),
	}
	switch {
	case i.last != nil:
		parts = append(parts, result(*i.last))
	case w.LastError != "":
		parts = append(parts, "error")
	}
	if w.Failures > 0 {
		parts = append(parts, stringutil.Quantify(w.Failures, "failure", "failures"))
	}
	return strings.Join(parts, " · ")
}

// listState is the watches.
type listState struct {
	list.Screen
	connect connector
	manager watch.Manager
	status  watch.Status
	confirm *confirm.Dialog
	backoff int
	// started is when this screen started a check, it counts as running for a moment
	started time.Time
	// gen is the poll loop, a new one starts each time the screen is shown
	gen    int
	closed bool
	now    func() time.Time

	keys listKeyMap
}

type listKeyMap struct {
	details, check, checkDue, pause, pauseProvider, delete, edit, gaps, refresh, runs key.Binding
}

type loadedMsg struct {
	manager watch.Manager
	status  watch.Status
	watches []watch.Watch
	last    map[int64]storage.WatchRun
}

type tickMsg struct{ gen int }

// actionMsg is a change to a watch (pause, delete), run with Load by the State.
type actionMsg struct {
	label string
	do    func(ctx context.Context) (string, error)
}

// changedMsg reloads after an action, with its notification.
type changedMsg struct{ notify string }

// New is the watches screen.
func New() base.State {
	return newListState(Connect)
}

func newListState(connect connector) *listState {
	s := &listState{
		connect: connect,
		confirm: confirm.NewDialog(40, color.Warning),
		backoff: config.Watch.Backoff.Get(),
		now:     time.Now,
		keys: listKeyMap{
			details:       util.Bind("details", "enter"),
			check:         util.Bind("check now", "c"),
			checkDue:      util.Bind("check due", "C"),
			pause:         util.Bind("pause/resume", "p"),
			pauseProvider: util.Bind("pause/resume its provider", "P"),
			delete:        util.Bind("delete", "d"),
			edit:          util.Bind("edit", "e"),
			gaps:          util.Bind("check gaps", "g"),
			refresh:       util.Bind("refresh", "r"),
			runs:          util.Bind("runs", "h"),
		},
	}
	s.Screen = list.NewScreen(list.New(2, 1, "watch", "watches", []*item{}, func(i *item) _list.DefaultItem { return i }), s.keyMap)
	s.updateKeys()
	return s
}

func (s *listState) KeyMap() help.KeyMap {
	if s.confirm.Active() {
		return s.confirm.KeyMap()
	}
	return s.Screen.KeyMap()
}

func (s *listState) keyMap() help.KeyMap {
	k := s.keys
	short := []key.Binding{k.details, k.check, k.pause, k.edit, k.runs}
	return util.KeyMap{Short: short, Full: [][]key.Binding{short, {k.checkDue, k.gaps, k.pauseProvider, k.delete, k.refresh}}}
}

func (s *listState) Backable() bool  { return s.Screen.Backable() && !s.confirm.Active() }
func (s *listState) Inputting() bool { return s.Screen.Inputting() || s.confirm.Active() }
func (s *listState) Title() base.Title {
	return base.Title{Text: "Watches"}
}

func (s *listState) Subtitle() string {
	parts := []string{s.List.Subtitle()}
	if s.manager != nil {
		parts = append(parts, managerName(s.status))
	}
	now := s.now()
	switch {
	case s.status.Checking:
		parts = append(parts, "checking since "+ago(s.status.Since, now))
	case s.checking():
		parts = append(parts, "checking")
	case !s.status.NextCheck.IsZero() && !s.status.NextCheck.After(now):
		parts = append(parts, "next check: due")
	case !s.status.NextCheck.IsZero():
		parts = append(parts, "next check in "+short(s.status.NextCheck.Sub(now)))
	}
	return strings.Join(parts, " · ")
}

func (s *listState) Resize(size base.Size) tea.Cmd {
	s.confirm.Resize(size)
	return s.Screen.Resize(size)
}

// Destroy stops the poll loop.
func (s *listState) Destroy() { s.closed = true }

func (s *listState) Init(ctx context.Context) tea.Cmd {
	return tea.Batch(s.reload(ctx, "Loading the watches"), s.tick(pollChecking))
}

// checking is true while a check runs, or one was just started here.
func (s *listState) checking() bool {
	return s.status.Checking || (!s.started.IsZero() && s.now().Sub(s.started) < startGrace)
}

func (s *listState) tick(d time.Duration) tea.Cmd {
	gen := s.gen
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{gen: gen} })
}

func (s *listState) reload(ctx context.Context, label string) tea.Cmd {
	m, connect := s.manager, s.connect
	return base.Load(ctx, label, func(ctx context.Context) (loadedMsg, error) {
		var msg loadedMsg
		var err error
		if m == nil {
			if m, err = connect(ctx); err != nil {
				return msg, err
			}
		}
		msg.manager = m
		if msg.status, err = m.Status(ctx); err != nil {
			return msg, err
		}
		if msg.watches, err = m.List(ctx); err != nil {
			return msg, err
		}
		runs, err := m.Runs(ctx, watch.RunFilter{Limit: lastRuns})
		if err != nil {
			return msg, err
		}
		msg.last = lastResults(runs)
		return msg, nil
	}, func(msg loadedMsg) tea.Msg { return msg })
}

// lastResults is the latest result of each watch in the runs (latest first), skipped
// checks aside.
func lastResults(runs []watch.CheckRun) map[int64]storage.WatchRun {
	last := map[int64]storage.WatchRun{}
	for _, run := range runs {
		for _, r := range run.Results {
			if _, ok := last[r.Watch]; ok || r.Watch == 0 || r.Status == storage.StatusSkipped {
				continue
			}
			last[r.Watch] = r
		}
	}
	return last
}

func (s *listState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok && s.confirm.Active() {
		return s.confirm.Update(msg)
	}
	switch msg := msg.(type) {
	case loadedMsg:
		return s.onLoaded(msg)
	case tickMsg:
		if msg.gen != s.gen || s.closed {
			return nil
		}
		next := poll
		if s.checking() {
			next = pollChecking
		}
		// the items would change under the filter being typed
		if s.List.Filtering() {
			return s.tick(next)
		}
		return tea.Batch(s.reload(ctx, ""), s.tick(next))
	case base.RestoredMsg:
		s.gen++
		return tea.Batch(s.reload(ctx, ""), s.tick(pollChecking))
	case actionMsg:
		return base.Load(ctx, msg.label, msg.do, func(notify string) tea.Msg { return changedMsg{notify: notify} })
	case changedMsg:
		return tea.Batch(s.reload(ctx, ""), base.Notify(msg.notify))
	case tea.KeyMsg:
		if s.List.Filtering() || s.manager == nil {
			break
		}
		if cmd, ok := s.updateKey(ctx, msg); ok {
			return cmd
		}
	}
	return s.List.Update(msg)
}

func (s *listState) onLoaded(msg loadedMsg) tea.Cmd {
	s.manager, s.status = msg.manager, msg.status
	now := s.now()
	selected := int64(0)
	if i, ok := s.List.SelectedItem().(*item); ok {
		selected = i.w.ID
	}
	items := make([]_list.Item, len(msg.watches))
	index := 0
	for i, w := range msg.watches {
		it := &item{w: w, backoff: s.backoff, now: now}
		if r, ok := msg.last[w.ID]; ok {
			it.last = &r
		}
		if w.ID == selected {
			index = i
		}
		items[i] = it
	}
	cmd := s.List.SetItems(items)
	s.List.Select(index)
	s.updateKeys()
	return cmd
}

func (s *listState) updateKeys() {
	enable := len(s.List.Items()) != 0
	for _, k := range []*key.Binding{&s.keys.details, &s.keys.check, &s.keys.checkDue, &s.keys.pause, &s.keys.pauseProvider, &s.keys.delete, &s.keys.edit, &s.keys.gaps} {
		k.SetEnabled(enable)
	}
}

func (s *listState) updateKey(ctx context.Context, msg tea.KeyMsg) (tea.Cmd, bool) {
	m := s.manager
	switch {
	case key.Matches(msg, s.keys.refresh):
		return s.reload(ctx, "Refreshing"), true
	case key.Matches(msg, s.keys.runs):
		return func() tea.Msg { return newRunsState(m) }, true
	case key.Matches(msg, s.keys.checkDue):
		var due []int64
		now := s.now()
		for _, i := range s.List.Items() {
			if w := i.(*item).w; w.Enabled && !w.NextCheck.After(now) {
				due = append(due, w.ID)
			}
		}
		if len(due) == 0 {
			return base.Notify("No watch is due"), true
		}
		return s.check(ctx, due, watch.CheckOptions{}, stringutil.Quantify(len(due), "due watch", "due watches")), true
	}
	i, ok := s.List.SelectedItem().(*item)
	if !ok {
		return nil, false
	}
	w := i.w
	switch {
	case key.Matches(msg, s.keys.details):
		backoff := s.backoff
		return func() tea.Msg { return newDetailsState(m, w, backoff) }, true
	case key.Matches(msg, s.keys.edit):
		return func() tea.Msg { return newEditState(m, w) }, true
	case key.Matches(msg, s.keys.check):
		return s.check(ctx, []int64{w.ID}, watch.CheckOptions{}, w.Title), true
	case key.Matches(msg, s.keys.gaps):
		return s.check(ctx, []int64{w.ID}, watch.CheckOptions{Gaps: true, Kind: watch.KindGaps}, w.Title+" (gaps)"), true
	case key.Matches(msg, s.keys.pause):
		return pauseCmd(m, w), true
	case key.Matches(msg, s.keys.pauseProvider):
		return pauseProviderCmd(m, w), true
	case key.Matches(msg, s.keys.delete):
		message := fmt.Sprintf("Stop watching %q? The history of its checks stays, the downloads too.", w.Title)
		s.confirm.AskDelete("Delete", message, s.View(), func() tea.Msg {
			return actionMsg{label: "Deleting " + w.Title, do: func(ctx context.Context) (string, error) {
				return "stopped watching " + w.Title, m.Remove(ctx, w.ID)
			}}
		})
		return nil, true
	}
	return nil, false
}

// pauseCmd pauses or resumes the watch.
func pauseCmd(m watch.Manager, w watch.Watch) tea.Cmd {
	label, done := "Pausing "+w.Title, "paused "+w.Title
	if !w.Enabled {
		label, done = "Resuming "+w.Title, "resumed "+w.Title
	}
	return func() tea.Msg {
		return actionMsg{label: label, do: func(ctx context.Context) (string, error) {
			_, err := m.SetEnabled(ctx, w.ID, !w.Enabled)
			return done, err
		}}
	}
}

// pauseProviderCmd pauses every watch of w's provider, or resumes them when w is paused
// (e.g. while the site is broken).
func pauseProviderCmd(m watch.Manager, w watch.Watch) tea.Cmd {
	resume := !w.Enabled
	label, done := "Pausing the watches of "+w.Provider, "paused the watches of "+w.Provider
	if resume {
		label, done = "Resuming the watches of "+w.Provider, "resumed the watches of "+w.Provider
	}
	return func() tea.Msg {
		return actionMsg{label: label, do: func(ctx context.Context) (string, error) {
			all, err := m.List(ctx)
			if err != nil {
				return "", err
			}
			n := 0
			for _, other := range all {
				if other.Provider != w.Provider || other.Enabled == resume {
					continue
				}
				if _, err := m.SetEnabled(ctx, other.ID, resume); err != nil {
					return "", err
				}
				n++
			}
			return fmt.Sprintf("%s (%d)", done, n), nil
		}}
	}
}

// check starts a check of the watches. It keeps running on other screens (until the TUI
// quits), its summary is a notification wherever the user is.
func (s *listState) check(ctx context.Context, ids []int64, opts watch.CheckOptions, what string) tea.Cmd {
	if s.checking() {
		return base.Notify(busyMessage(s.status, s.now()))
	}
	s.started = s.now()
	// polls faster from now, the loop in place could be waiting its slow tick
	s.gen++
	return tea.Batch(base.Notify("Checking "+what), checkCmd(base.Background(ctx), s.manager, ids, opts), s.tick(pollChecking))
}

func busyMessage(st watch.Status, now time.Time) string {
	if st.Checking && !st.Since.IsZero() {
		return "A check is running (since " + ago(st.Since, now) + "), try again when it's done"
	}
	return "A check is running, try again when it's done"
}

func checkCmd(ctx context.Context, m watch.Manager, ids []int64, opts watch.CheckOptions) tea.Cmd {
	return func() tea.Msg {
		release, ok := base.Track(ctx)
		if !ok {
			return nil
		}
		defer release()
		report, err := m.Check(ctx, ids, opts)
		switch {
		case errors.Is(err, watch.ErrBusy):
			return base.NotificationMsg{Message: "Another watch check is running, try again when it's done"}
		case err != nil:
			return fmt.Errorf("check watches: %w", err)
		}
		return base.NotificationWithDurationMsg{NotificationMsg: base.NotificationMsg{Message: summary(report)}, Duration: 5 * time.Second}
	}
}

// summary is a check's report in one line.
func summary(r watch.Report) string {
	s := fmt.Sprintf("Checked %s: %s", stringutil.Quantify(r.Watches, "watch", "watches"), stringutil.Quantify(r.NewChapters, "new chapter", "new chapters"))
	if r.Errors > 0 {
		s += ", " + stringutil.Quantify(r.Errors, "error", "errors")
	}
	return s
}

func (s *listState) View() string {
	if s.confirm.Active() {
		return s.confirm.View()
	}
	return s.List.View()
}
