package watches

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
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
	"github.com/luevano/mangal/v5/theme/style"
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
	s *listState
	w watch.Watch
	// last is its latest check, nil if none in the recent runs
	last    *storage.WatchRun
	backoff int
	now     time.Time
}

func (i *item) FilterValue() string { return i.w.Title + " " + i.w.Provider }

func (i *item) Title() string {
	title := i.w.Title
	if !i.w.Enabled {
		title += " " + pausedStyle.Render("(paused)")
	}
	if i.w.NeedsRelink {
		title += " " + relinkStyle.Render("(needs re-link)")
	}
	if i.w.LibraryProblem() != "" {
		title += " " + libraryStyle.Render("(not in library)")
	}
	if _, ok := i.checking(); ok {
		title = style.Normal.Accent.Render(spinnerFrames[i.s.frame%len(spinnerFrames)]) + " " + title
	}
	return title
}

// checking is what the watch's check does now, false when it isn't being checked.
func (i *item) checking() (liveWatch, bool) {
	if i.s == nil {
		return liveWatch{}, false
	}
	return i.s.liveOf(i.w)
}

func (i *item) Description() string {
	w := i.w
	if lw, ok := i.checking(); ok {
		state := "checking"
		if lw.state != "" && lw.state != "checking" {
			state += ": " + lw.state
		}
		if !lw.since.IsZero() {
			state += " (" + short(max(0, i.now.Sub(lw.since))) + ")"
		}
		return w.Provider + " · last " + formatNumber(w.LastDownloaded) + " · " + state
	}
	if w.NeedsRelink {
		return w.Provider + " · " + w.MangaID + " · last " + formatNumber(w.LastDownloaded) + " · needs re-link: " + w.RelinkReason
	}
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
		confirm: confirm.NewDialog(40, color.Warning),
		backoff: config.Watch.Backoff.Get(),
		now:     time.Now,
		keys: listKeyMap{
			details:       util.Bind("details", "enter"),
			check:         util.Bind("check now", "c"),
			checkDue:      util.Bind("check due", "C"),
			checkProvider: util.Bind("check its provider", "alt+c"),
			pause:         util.Bind("pause/resume", "p"),
			pauseProvider: util.Bind("pause/resume its provider", "P"),
			delete:        util.Bind("stop watching", "x"),
			edit:          util.Bind("edit", "e"),
			gaps:          util.Bind("fill gaps", "F"),
			refresh:       util.Bind("refresh", "r"),
			runs:          util.Bind("every check run", "V"),
			library:       util.Bind("library manga", "L"),
			history:       util.Bind("provider history", "v"),
			nextProvider:  util.Bind("next provider", "tab"),
			group:         util.Bind("group by provider", "o"),
			live:          util.Bind("live log", "t"),
			running:       util.Bind("every live check", "T"),
			relink:        util.Bind("re-link manga", "m"),
			missing:       util.Bind("needs re-link only", "n"),
			unlinked:      util.Bind("not in library only", "i"),
		},
		live:          map[int64]liveWatch{},
		liveProviders: map[string]bool{},
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
	short := []key.Binding{k.details, k.check, k.live, k.pause, k.edit, k.runs}
	return util.KeyMap{Short: short, Full: [][]key.Binding{
		short,
		{k.checkDue, k.checkProvider, k.gaps, k.pauseProvider, k.delete, k.refresh},
		{k.library, k.history, k.nextProvider, k.group, k.running},
		{k.relink, k.missing, k.unlinked},
	}}
}

func (s *listState) Backable() bool  { return s.Screen.Backable() && !s.confirm.Active() }
func (s *listState) Inputting() bool { return s.Screen.Inputting() || s.confirm.Active() }
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

// busy says the provider is being checked, so it can't be checked now.
func (s *listState) busy(provider string) (string, bool) {
	if ps, ok := s.status.Provider(provider); ok {
		return fmt.Sprintf("%s is being checked (since %s), try again when it's done", provider, ago(ps.Since, s.now())), true
	}
	if s.liveProviders[provider] {
		return provider + " is being checked, try again when it's done", true
	}
	return "", false
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
	case watch.LiveFound, watch.LiveChapter, watch.LiveWait:
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
	if msg, ok := msg.(tea.KeyMsg); ok && s.confirm.Active() {
		return s.confirm.Update(msg)
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
	selected := int64(0)
	if i, ok := s.List.SelectedItem().(*item); ok {
		selected = i.w.ID
	}
	shown := s.shown()
	items := make([]_list.Item, len(shown))
	index := 0
	for i, w := range shown {
		it := &item{s: s, w: w, backoff: s.backoff, now: now}
		if r, ok := s.last[w.ID]; ok {
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

func (s *listState) updateKey(ctx context.Context, msg tea.KeyMsg) (tea.Cmd, bool) {
	m := s.manager
	switch {
	case key.Matches(msg, s.keys.refresh):
		return s.reload(ctx, "Refreshing"), true
	case key.Matches(msg, s.keys.runs):
		return func() tea.Msg { return newRunsState(m) }, true
	case key.Matches(msg, s.keys.nextProvider):
		s.provider = nextProvider(s.providers(), s.provider)
		return s.setItems(), true
	case key.Matches(msg, s.keys.group):
		s.byProvider = !s.byProvider
		return s.setItems(), true
	case key.Matches(msg, s.keys.missing):
		s.missing = !s.missing
		cmd := s.setItems()
		if s.missing && len(s.List.Items()) == 0 {
			cmd = tea.Batch(cmd, base.Notify("No watch needs a re-link"))
		}
		return cmd, true
	case key.Matches(msg, s.keys.unlinked):
		s.unlinked = !s.unlinked
		cmd := s.setItems()
		if s.unlinked && len(s.List.Items()) == 0 {
			cmd = tea.Batch(cmd, base.Notify("Every watch is in the library (or has downloaded nothing yet)"))
		}
		return cmd, true
	case key.Matches(msg, s.keys.running):
		return func() tea.Msg { return newLiveState(m, nil) }, true
	case key.Matches(msg, s.keys.checkDue):
		var due []int64
		busy := 0
		now := s.now()
		for _, i := range s.List.Items() {
			w := i.(*item).w
			if !w.Scheduled() || w.NextCheck.After(now) {
				continue
			}
			if _, ok := s.busy(w.Provider); ok {
				busy++
				continue
			}
			due = append(due, w.ID)
		}
		switch {
		case len(due) == 0 && busy > 0:
			return base.Notify("The providers of the due watches are being checked, try again when they're done"), true
		case len(due) == 0:
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
	case key.Matches(msg, s.keys.relink):
		return func() tea.Msg { return newRelinkState(m, w) }, true
	case key.Matches(msg, s.keys.library):
		return libraryCmd(ctx, w), true
	case key.Matches(msg, s.keys.history):
		return func() tea.Msg { return newHistoryState(m, w.Provider) }, true
	case key.Matches(msg, s.keys.live):
		return func() tea.Msg { return newLiveState(m, &w) }, true
	case key.Matches(msg, s.keys.check), key.Matches(msg, s.keys.gaps), key.Matches(msg, s.keys.checkProvider):
		if busy, ok := s.busy(w.Provider); ok {
			return base.Notify(busy), true
		}
		switch {
		case key.Matches(msg, s.keys.check):
			return s.check(ctx, []int64{w.ID}, watch.CheckOptions{}, w.Title), true
		case key.Matches(msg, s.keys.gaps):
			return s.check(ctx, []int64{w.ID}, watch.CheckOptions{Gaps: true, Kind: watch.KindGaps}, w.Title+" (gaps)"), true
		}
		s.started = s.now()
		s.gen++
		return tea.Batch(checkProviderCmd(ctx, m, w.Provider), s.tick(pollChecking)), true
	case key.Matches(msg, s.keys.pause):
		return pauseCmd(m, w), true
	case key.Matches(msg, s.keys.pauseProvider):
		return pauseProviderCmd(m, w), true
	case key.Matches(msg, s.keys.delete):
		message := fmt.Sprintf("Stop watching %q? The history of its checks stays, the downloads too.", w.Title)
		// x then x, as the library's d then d
		s.confirm.AskWith([]string{"x", "y"}, "Stop watching", message, s.View(), func() tea.Msg {
			return actionMsg{label: "Removing " + w.Title, do: func(ctx context.Context) (string, error) {
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
	s.started = s.now()
	// polls faster from now, the loop in place could be waiting its slow tick
	s.gen++
	s.spinning = false
	return tea.Batch(base.Notify("Checking "+what), checkCmd(base.Background(ctx), s.manager, ids, opts), s.tick(pollChecking), s.spin())
}

// busyMessage is a check refused because another check or a download has its providers.
const busyMessage = "Another check or a download has those providers, try again when it's done"

func checkCmd(ctx context.Context, m watch.Manager, ids []int64, opts watch.CheckOptions) tea.Cmd {
	return func() tea.Msg {
		release, ok := base.Track(ctx)
		if !ok {
			return nil
		}
		defer release()
		return checkResult(m.Check(ctx, ids, opts))
	}
}

func checkResult(report watch.Report, err error) tea.Msg {
	switch {
	case errors.Is(err, watch.ErrBusy):
		return base.NotificationMsg{Message: busyMessage}
	case err != nil:
		return fmt.Errorf("check watches: %w", err)
	}
	return base.NotificationWithDurationMsg{NotificationMsg: base.NotificationMsg{Message: summary(report)}, Duration: 5 * time.Second}
}

// checkProviderCmd checks every enabled watch of the provider now, in the background (it
// outlives the screen).
func checkProviderCmd(ctx context.Context, m watch.Manager, provider string) tea.Cmd {
	ctx = base.Background(ctx)
	return tea.Batch(base.Notify("Checking every watch of "+provider), func() tea.Msg {
		release, ok := base.Track(ctx)
		if !ok {
			return nil
		}
		defer release()
		all, err := m.List(ctx)
		if err != nil {
			return fmt.Errorf("check %s: %w", provider, err)
		}
		var ids []int64
		for _, w := range all {
			if w.Provider == provider && w.Scheduled() {
				ids = append(ids, w.ID)
			}
		}
		if len(ids) == 0 {
			return base.NotificationMsg{Message: provider + " has no enabled watches"}
		}
		return checkResult(m.Check(ctx, ids, watch.CheckOptions{}))
	})
}

// summary is a check's report in one line.
func summary(r watch.Report) string {
	s := fmt.Sprintf("Checked %s: %s", stringutil.Quantify(r.Watches, "watch", "watches"), stringutil.Quantify(r.NewChapters, "new chapter", "new chapters"))
	if r.Errors > 0 {
		s += ", " + stringutil.Quantify(r.Errors, "error", "errors")
	}
	if len(r.Busy) > 0 {
		s += " (busy, skipped: " + strings.Join(r.Busy, ", ") + ")"
	}
	return s
}

func (s *listState) View() string {
	if s.confirm.Active() {
		return s.confirm.View()
	}
	return s.List.View()
}
