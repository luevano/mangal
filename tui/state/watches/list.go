package watches

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
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

// listState is the watches.
type listState struct {
	list.Screen
	connect connector
	manager watch.Manager
	status  watch.Status
	backoff int
	// started is when this screen started a check, it counts as running for a moment
	started time.Time
	// gen is the poll loop, a new one starts each time the screen is shown
	gen    int
	closed bool
	now    func() time.Time

	// watches and last are the loaded ones, the items those of provider (all when empty),
	// sorted by provider when byProvider
	watches    []watch.Watch
	last       map[int64]storage.WatchRun
	provider   string
	byProvider bool
	// missing shows only the watches whose manga needs fixing (missingManga), unlinked
	// those with a library issue
	missing, unlinked bool

	// feed are the live events; live the watches being checked and liveProviders the
	// providers, as the events tell (the status, polled, has the rest)
	feed          *feed
	live          map[int64]liveWatch
	liveProviders map[string]bool
	// frame is the spinner's, spinning while its tick loop runs
	frame    int
	spinning bool

	keys listKeyMap
}

type listKeyMap struct {
	details, check, checkDue, checkProvider, pause, pauseProvider, delete, edit, gaps, refresh, runs,
	library, history, nextProvider, group, live, running, relink, missing, unlinked key.Binding
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
		backoff: config.Watch.Backoff.Get(),
		now:     time.Now,
		keys: listKeyMap{
			details:       util.Bind("details", "enter"),
			check:         base.KeyCheck,
			checkDue:      base.KeyCheckDue,
			checkProvider: base.KeyCheckProvider,
			pause:         base.KeyPause,
			pauseProvider: base.KeyPauseProvider,
			delete:        base.KeyStopWatching,
			edit:          base.KeyEdit,
			gaps:          base.KeyFillGaps,
			refresh:       base.KeyRefresh,
			runs:          base.KeyRuns,
			library:       base.KeyLibraryManga,
			history:       base.KeyProviderHistory,
			nextProvider:  base.KeyNextProvider,
			group:         base.KeyGroupByProvider,
			live:          base.KeyLiveLog,
			running:       base.KeyLiveChecks,
			relink:        base.KeyRelink,
			missing:       base.KeyNeedsRelink,
			unlinked:      base.KeyNotInLibrary,
		},
		live:          map[int64]liveWatch{},
		liveProviders: map[string]bool{},
	}
	s.Screen = list.NewScreen(list.New(2, 1, "watch", "watches", []*item{}, func(i *item) _list.DefaultItem { return i }), s.keyMap)
	s.Confirm = confirm.NewDialog(40, color.Warning)
	s.updateKeys()
	return s
}

func (s *listState) keyMap() help.KeyMap {
	k := s.keys
	short := []key.Binding{k.details, k.check, k.live, k.pause, k.edit, k.runs}
	return util.KeyMap{Short: short, Full: [][]key.Binding{
		short,
		{k.checkDue, k.checkProvider, k.gaps, k.pauseProvider, k.delete, k.refresh},
		{k.library, k.history, k.nextProvider, k.group, k.running},
		{k.relink, k.missing, k.unlinked},
	}}
}

func (s *listState) Key() string { return "watches" }

func (s *listState) Title() base.Title {
	return base.Title{Text: "Watches"}
}

func (s *listState) Subtitle() string {
	parts := []string{s.List.Subtitle()}
	if s.provider != "" {
		parts = append(parts, s.provider)
	}
	if s.byProvider {
		parts = append(parts, "by provider")
	}
	if s.missing {
		parts = append(parts, "needing re-link")
	} else if n := s.countMissing(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d need a re-link (%s)", n, s.keys.missing.Help().Key))
	}
	if s.unlinked {
		parts = append(parts, "not in library")
	} else if n := s.countUnlinked(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d not in library (%s)", n, s.keys.unlinked.Help().Key))
	}
	if s.manager != nil {
		parts = append(parts, managerName(s.status))
	}
	now := s.now()
	switch n := len(s.runningProviders()); {
	case n > 0:
		parts = append(parts, stringutil.Quantify(n, "provider", "providers")+" checking")
	case s.checking():
		parts = append(parts, "checking")
	case !s.status.NextCheck.IsZero() && !s.status.NextCheck.After(now):
		parts = append(parts, "next check: due")
	case !s.status.NextCheck.IsZero():
		parts = append(parts, "next check "+watch.FormatNext(s.status.NextCheck, now))
	}
	return strings.Join(parts, " · ")
}

// Destroy stops the poll loop.
func (s *listState) Destroy() { s.closed = true }

func (s *listState) Init(ctx context.Context) tea.Cmd {
	return tea.Batch(s.reload(ctx, "Loading the watches"), s.tick(pollChecking))
}

// checking is true while a check runs, or one was just started here.
func (s *listState) checking() bool {
	return len(s.runningProviders()) > 0 || (!s.started.IsZero() && s.now().Sub(s.started) < startGrace)
}

// runningProviders are the providers being checked, sorted.
func (s *listState) runningProviders() []string {
	var running []string
	for _, ps := range s.status.Providers {
		running = append(running, ps.Provider)
	}
	for p := range s.liveProviders {
		running = append(running, p)
	}
	slices.Sort(running)
	return slices.Compact(running)
}

// liveOf is what the watch's check does now, false when it isn't being checked.
func (s *listState) liveOf(w watch.Watch) (liveWatch, bool) {
	if lw, ok := s.live[w.ID]; ok {
		return lw, true
	}
	if ps, ok := s.status.Provider(w.Provider); ok && ps.Watch == w.ID {
		return liveWatch{provider: ps.Provider, state: ps.State, chapter: ps.Chapter, since: ps.WatchSince}, true
	}
	return liveWatch{}, false
}

// onEvent keeps what the watches being checked do.
func (s *listState) onEvent(e watch.LiveEvent) {
	switch e.Kind {
	case watch.LiveProviderStart:
		s.liveProviders[e.Provider] = true
	case watch.LiveProviderDone:
		delete(s.liveProviders, e.Provider)
		s.status.Providers = slices.DeleteFunc(s.status.Providers, func(ps watch.ProviderStatus) bool { return ps.Provider == e.Provider })
	case watch.LiveStart:
		s.live[e.Watch] = liveWatch{provider: e.Provider, state: "checking", since: e.Time}
	case watch.LiveFound, watch.LiveChapter, watch.LiveWait, watch.LiveProgress, watch.LiveStatus:
		if lw, ok := s.live[e.Watch]; ok {
			lw.state = e.Message
			if e.Chapter != nil {
				lw.chapter = e.Chapter
			}
			s.live[e.Watch] = lw
		}
	case watch.LiveDone:
		delete(s.live, e.Watch)
		for i, ps := range s.status.Providers {
			if ps.Watch == e.Watch {
				s.status.Providers[i].Watch = 0
			}
		}
	}
}

// spin starts the spinner's tick loop if something is being checked.
func (s *listState) spin() tea.Cmd {
	if s.spinning || !s.anyChecking() {
		return nil
	}
	s.spinning = true
	gen := s.gen
	return tea.Tick(spinEvery, func(time.Time) tea.Msg { return spinMsg{gen: gen} })
}

func (s *listState) anyChecking() bool {
	if len(s.live) > 0 {
		return true
	}
	for _, ps := range s.status.Providers {
		if ps.Watch != 0 {
			return true
		}
	}
	return false
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
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	switch msg := msg.(type) {
	case loadedMsg:
		return s.onLoaded(ctx, msg)
	case feedMsg:
		s.feed = msg.feed
		clear(s.live)
		clear(s.liveProviders)
		return msg.feed.next()
	case liveMsg:
		if msg.feed != s.feed || msg.closed {
			return nil
		}
		s.onEvent(msg.e)
		cmds := []tea.Cmd{s.feed.next(), s.spin()}
		if msg.e.Kind == watch.LiveRunDone {
			// the last results changed
			cmds = append(cmds, s.reload(ctx, ""))
		}
		return tea.Batch(cmds...)
	case spinMsg:
		if msg.gen != s.gen || s.closed {
			return nil
		}
		s.frame++
		s.spinning = false
		return s.spin()
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
		s.spinning = false
		var sub tea.Cmd
		if s.manager != nil {
			s.feed = nil
			sub = subscribe(ctx, s.manager)
		}
		return tea.Batch(s.reload(ctx, ""), s.tick(pollChecking), sub)
	case actionMsg:
		return base.Load(ctx, msg.label, msg.do, func(notify string) tea.Msg { return changedMsg{notify: notify} })
	case changedMsg:
		return tea.Batch(s.reload(ctx, ""), base.Notify(msg.notify))
	case tea.KeyPressMsg:
		if s.List.Filtering() || s.manager == nil {
			break
		}
		if cmd, ok := s.updateKey(ctx, msg); ok {
			return cmd
		}
	}
	return s.List.Update(msg)
}

func (s *listState) onLoaded(ctx context.Context, msg loadedMsg) tea.Cmd {
	first := s.manager == nil
	s.manager, s.status = msg.manager, msg.status
	s.watches, s.last = msg.watches, msg.last
	cmds := []tea.Cmd{s.setItems(), s.spin()}
	if first {
		cmds = append(cmds, subscribe(ctx, s.manager))
	}
	return tea.Batch(cmds...)
}

// countMissing is how many watches need their manga fixed.
func (s *listState) countMissing() int {
	n := 0
	for _, w := range s.watches {
		if missingManga(w) {
			n++
		}
	}
	return n
}

// countUnlinked is how many watches have a library issue.
func (s *listState) countUnlinked() int {
	n := 0
	for _, w := range s.watches {
		if w.LibraryProblem() != "" {
			n++
		}
	}
	return n
}

// shown are the watches of the provider filter, in the order chosen.
func (s *listState) shown() []watch.Watch {
	var shown []watch.Watch
	for _, w := range s.watches {
		if (s.provider == "" || w.Provider == s.provider) && (!s.missing || missingManga(w)) && (!s.unlinked || w.LibraryProblem() != "") {
			shown = append(shown, w)
		}
	}
	if s.byProvider {
		slices.SortStableFunc(shown, func(a, b watch.Watch) int {
			return cmp.Or(cmp.Compare(a.Provider, b.Provider), cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)))
		})
	}
	return shown
}

// setItems shows the loaded watches, the selected one stays selected.
func (s *listState) setItems() tea.Cmd {
	now := s.now()
	shown := s.shown()
	items := make([]_list.Item, len(shown))
	for i, w := range shown {
		it := &item{s: s, w: w, backoff: s.backoff, now: now}
		if r, ok := s.last[w.ID]; ok {
			it.last = &r
		}
		items[i] = it
	}
	cmd := s.List.SetItemsKeep(items)
	s.updateKeys()
	return cmd
}

// providers are the providers of the loaded watches, sorted.
func (s *listState) providers() []string {
	var providers []string
	for _, w := range s.watches {
		providers = append(providers, w.Provider)
	}
	slices.Sort(providers)
	return slices.Compact(providers)
}

func (s *listState) updateKeys() {
	enable := len(s.List.Items()) != 0
	for _, k := range []*key.Binding{&s.keys.details, &s.keys.check, &s.keys.checkDue, &s.keys.checkProvider, &s.keys.pause, &s.keys.pauseProvider,
		&s.keys.delete, &s.keys.edit, &s.keys.gaps, &s.keys.library, &s.keys.history, &s.keys.live, &s.keys.relink} {
		k.SetEnabled(enable)
	}
}
