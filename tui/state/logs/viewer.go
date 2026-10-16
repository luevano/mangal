package logs

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/util"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// maxEntries is about how many entries a viewer keeps, the latest.
const maxEntries = 50000

// followEvery is how often a followed log is read again.
var followEvery = 500 * time.Millisecond

// now is swapped in tests.
var now = time.Now

// levels are the minimum levels tab goes through.
var levels = []log.Level{log.LevelDebug, log.LevelInfo, log.LevelWarn, log.LevelError}

type kind int

const (
	kindSession kind = iota
	kindFile
	kindSearch
	// every file of a period, in order
	kindPeriod
)

type viewerKeys struct {
	up, down, pageUp, pageDown, top, bottom, find, next, prev, narrow, level, copy, clear key.Binding
	files, prevPeriod, nextPeriod                                                         key.Binding
}

var _ base.State = (*viewer)(nil)

// viewer is a pager of log entries: this session's, a file's (and the next ones, when it's
// the newest), a period's (and the next ones, when it's the current one) or a search's
// through every file.
type viewer struct {
	kind  kind
	dir   string
	file  log.File
	query string
	// main is the window ctrl+l opens ("Logs"), note is said in its subtitle
	main bool
	note string
	// period is the period shown ("" the current one), stamps every period of the files
	// (oldest first) and currentPeriod the current one's, as of the last load
	period, currentPeriod string
	stamps                []string
	// followable: new entries come (this session, the newest file), follow shows them
	followable, follow bool
	tail               *log.Tail
	// followed is the file the tail reads, as of its last read
	followed string
	// tailMu: a follow of before the window was left may still be reading
	tailMu *sync.Mutex
	gen    int

	loaded  bool
	entries []log.Entry
	dropped int
	// dated: the entries span more than a day, their date is shown
	dated bool

	level   log.Level
	find    *search.Model
	matcher *matcher
	// hits are whether each entry matches (matched once, when it comes); narrow: only
	// those are shown
	hits   []bool
	narrow bool
	// shown are the indexes of the entries at the level (and matching, narrowed), matches
	// the ones of shown that match, current the shown match (-1 for none)
	shown, matches []int
	current        int

	// heights are the rows of each shown entry at width (with its file's header in a
	// search), starts the first row of each
	width     int
	heights   []int
	starts    []int
	rows, top int
	size      base.Size
	keys      viewerKeys
}

func newViewer(k kind) *viewer {
	v := &viewer{
		kind:    k,
		current: -1,
		find:    search.New("Find (fuzzy, re: for a regexp)...", "", 64, 0),
		keys: viewerKeys{
			up:         util.BindNamedKey("↑/k", "up", "up", "k"),
			down:       util.BindNamedKey("↓/j", "down", "down", "j"),
			pageUp:     util.BindNamedKey("pgup/b", "page up", "pgup", "b"),
			pageDown:   util.BindNamedKey("pgdn/f/space", "page down", "pgdown", "f", "space"),
			top:        util.BindNamedKey("g/home", "top", "g", "home"),
			bottom:     util.BindNamedKey("G/end", "end", "G", "end"),
			find:       util.Bind("find", "/"),
			next:       base.KeyNextMatch,
			prev:       base.KeyPrevMatch,
			narrow:     base.KeyNarrow,
			level:      base.KeyLevel,
			copy:       base.KeyCopy,
			clear:      util.Bind("clear find", "esc"),
			files:      base.KeyLogFiles,
			prevPeriod: base.KeyPrevPeriod,
			nextPeriod: base.KeyNextPeriod,
		},
	}
	return v
}

func newSession() *viewer {
	v := newViewer(kindSession)
	v.followable, v.follow = true, true
	return v
}

func newFile(dir string, f log.File, newest bool) *viewer {
	v := newViewer(kindFile)
	v.dir, v.file = dir, f
	v.followable, v.follow = newest, newest
	return v
}

// newPeriod is the main viewer of a period of dir, "" for the current one (followed).
func newPeriod(dir, period string) *viewer {
	v := newViewer(kindPeriod)
	v.main, v.dir, v.period = true, dir, period
	v.followable, v.follow = period == "", period == ""
	return v
}

func newSearch(dir, query string) *viewer {
	v := newViewer(kindSearch)
	v.dir, v.query = dir, query
	v.matcher = newMatcher(query)
	return v
}

func (v *viewer) Key() string {
	if v.main {
		return "logs"
	}
	switch v.kind {
	case kindSession:
		return viewerKey(sessionKey)
	case kindSearch:
		return "logs-search/" + v.query
	}
	return viewerKey(v.file.Name)
}

func (v *viewer) Title() base.Title {
	switch {
	case v.main && v.period != "":
		return base.Title{Text: "Logs · " + v.period}
	case v.main:
		return base.Title{Text: "Logs"}
	case v.kind == kindSession:
		return base.Title{Text: "Logs · this session"}
	case v.kind == kindSearch:
		return base.Title{Text: "Logs · search: " + v.query}
	}
	return base.Title{Text: "Logs · " + v.file.Name}
}

// stampOf is the period of a log file's name (2026-10-13 of 2026-10-13.1.log).
func stampOf(name string) string {
	stamp, _, _ := strings.Cut(name, ".")
	return stamp
}

// following is the period followed: the current one's, or the next one's once there.
func (v *viewer) following() string {
	if v.followed != "" {
		return stampOf(v.followed)
	}
	return v.currentPeriod
}

func (v *viewer) Subtitle() string {
	var parts []string
	switch {
	case v.note != "":
		parts = append(parts, v.note)
	case v.kind == kindSearch:
		parts = append(parts, stringutil.Quantify(len(v.entries), "entry", "entries")+" in every file")
	case v.kind == kindPeriod && v.period == "" && v.following() != "":
		parts = append(parts, v.following())
	case v.kind == kindFile && v.followed != "" && v.followed != v.file.Name:
		parts = append(parts, "now in "+v.followed)
	}
	if v.level != log.LevelDebug {
		parts = append(parts, strings.ToLower(v.level.String())+" and up")
	}
	if v.matcher != nil {
		switch {
		case len(v.matches) == 0:
			parts = append(parts, "no match")
		case v.current < 0:
			parts = append(parts, stringutil.Quantify(len(v.matches), "match", "matches"))
		default:
			parts = append(parts, fmt.Sprintf("match %d/%d", v.current+1, len(v.matches)))
		}
		if v.narrow {
			parts = append(parts, "narrowed")
		} else {
			parts = append(parts, "all lines")
		}
	}
	switch {
	case v.follow:
		parts = append(parts, "following")
	case v.rows > v.height():
		parts = append(parts, fmt.Sprintf("%d%%", 100*min(v.rows, v.top+v.height())/v.rows))
	}
	if v.dropped > 0 {
		parts = append(parts, fmt.Sprintf("the first %d entries not shown", v.dropped))
	}
	return strings.Join(parts, " · ")
}

func (v *viewer) Status() string { return "" }

func (v *viewer) Inputting() bool { return v.find.Searching() }

// Backable: esc stops finding first.
func (v *viewer) Backable() bool { return !v.find.Searching() && v.matcher == nil }

func (v *viewer) KeyMap() help.KeyMap {
	if v.find.Searching() {
		return inputKeys("find")
	}
	k := v.keys
	k.clear.SetEnabled(v.matcher != nil)
	k.next.SetEnabled(len(v.matches) > 0)
	k.prev.SetEnabled(len(v.matches) > 0)
	k.narrow.SetEnabled(v.matcher != nil)
	k.copy.SetEnabled(len(v.shown) > 0)
	end := k.bottom
	if v.followable {
		end.SetHelp(end.Help().Key, "follow")
	}
	if !v.main {
		return util.KeyMap{
			Short: []key.Binding{k.find, k.next, k.narrow, k.level, end, k.clear},
			Full: [][]key.Binding{
				{k.find, k.next, k.prev, k.narrow, k.clear},
				{k.level, k.copy},
				{k.up, k.down, k.pageUp, k.pageDown},
				{k.top, end},
			},
		}
	}
	_, ok := v.stepPeriod(-1)
	k.prevPeriod.SetEnabled(ok)
	_, ok = v.stepPeriod(1)
	k.nextPeriod.SetEnabled(ok)
	return util.KeyMap{
		Short: []key.Binding{k.find, k.next, k.narrow, k.level, end, k.files, k.prevPeriod, k.nextPeriod, k.clear},
		Full: [][]key.Binding{
			{k.find, k.next, k.prev, k.narrow, k.clear},
			{k.level, k.copy},
			{k.files, k.prevPeriod, k.nextPeriod},
			{k.up, k.down, k.pageUp, k.pageDown},
			{k.top, end},
		},
	}
}

// stepPeriod is the period d (1, -1) after the shown one, "" for the current one; ok is
// false past the ends.
func (v *viewer) stepPeriod(d int) (string, bool) {
	if v.kind != kindPeriod || !v.loaded {
		return "", false
	}
	var past []string
	for _, s := range v.stamps {
		if s != v.currentPeriod && s != v.following() {
			past = append(past, s)
		}
	}
	i := len(past)
	if v.period != "" {
		i = slices.Index(past, v.period)
	}
	switch j := i + d; {
	case j < 0 || (v.period == "" && j >= len(past)):
		return "", false
	case j >= len(past):
		return "", true
	default:
		return past[j], true
	}
}

// showPeriod shows another period ("" the current one), keeping the level and the find
// (narrowed or not).
func (v *viewer) showPeriod(ctx context.Context, period string) tea.Cmd {
	v.period = period
	v.followable, v.follow = period == "", period == ""
	v.loaded, v.tail, v.tailMu, v.followed = false, nil, nil, ""
	v.current, v.top = -1, 0
	v.setEntries(nil, true)
	return v.start(ctx)
}

func (v *viewer) Resize(size base.Size) tea.Cmd {
	v.size = size
	v.find.Resize(size)
	v.layout()
	return nil
}

// inputKeys are the help while typing a find or search.
func inputKeys(action string) help.KeyMap {
	return util.KeyMap{Short: []key.Binding{util.Bind(action, "enter"), util.Bind("cancel", "esc")}}
}

// height is the rows for the entries.
func (v *viewer) height() int {
	h := v.size.Height
	if v.find.Searching() {
		h -= 2
	}
	return max(1, h)
}

// entriesMsg are entries loaded (replace) or followed (by tail).
type entriesMsg struct {
	gen     int
	entries []log.Entry
	replace bool
	tail    *log.Tail
	// followed is the tail's file after the read
	followed string
}

// periodMsg is a period loaded: its entries, the tail that follows it (the current one),
// the periods of the files.
type periodMsg struct {
	gen                   int
	entries               []log.Entry
	tail                  *log.Tail
	stamps                []string
	period, currentPeriod string
	followed              string
	// unreadable: the current period's dir can't be read, this session's entries instead
	unreadable error
}

type tickMsg struct{ gen int }

func (v *viewer) Init(ctx context.Context) tea.Cmd {
	return v.start(ctx)
}

// start loads the entries, then follows them.
func (v *viewer) start(ctx context.Context) tea.Cmd {
	v.gen++
	gen := v.gen
	if v.kind == kindSession {
		v.setEntries(log.Recent(), true)
		return v.tick(ctx)
	}
	if v.loaded {
		// the file was read, the tail goes on from where it was
		return v.tick(ctx)
	}
	if v.kind == kindPeriod {
		return v.loadPeriod(ctx, gen)
	}
	label := "Reading " + v.file.Name
	if v.kind == kindSearch {
		label = "Searching the logs"
	}
	if v.followable {
		v.tail, v.tailMu = log.NewTailAt(v.dir, v.file), &sync.Mutex{}
	}
	tail, file, dir, m, search := v.tail, v.file, v.dir, v.matcher, v.kind == kindSearch
	return base.Load(ctx, label, func(ctx context.Context) ([]log.Entry, error) {
		var entries []log.Entry
		add := keepLast(&entries)
		switch {
		case tail != nil:
			return entries, tail.Next(log.Filter{}, add)
		case search:
			if m == nil {
				return nil, nil
			}
			return entries, logsOf(dir).Entries(ctx, core.LogQuery{}, func(e log.Entry) error {
				if !m.match(e.Message) {
					return nil
				}
				return add(e)
			})
		default:
			return entries, logsOf(dir).Entries(ctx, core.LogQuery{Files: []string{file.Name}}, add)
		}
	}, func(entries []log.Entry) tea.Msg {
		msg := entriesMsg{gen: gen, entries: entries, replace: true}
		if tail != nil {
			msg.followed = tail.Following()
		}
		return msg
	})
}

// logsOf are the logs of the directory. The current period's tail stays local: it reads
// on from where it was.
func logsOf(dir string) core.Logs { return core.NewLogs(func() string { return dir }) }

// scanFiles tells add the entries of the files, none for none.
func scanFiles(ctx context.Context, dir string, files []log.File, add func(log.Entry) error) error {
	if len(files) == 0 {
		return nil
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Name
	}
	return logsOf(dir).Entries(ctx, core.LogQuery{Files: names}, add)
}

// keepLast adds the entries to a slice that keeps about the last maxEntries.
func keepLast(entries *[]log.Entry) func(log.Entry) error {
	return func(e log.Entry) error {
		*entries = append(*entries, e)
		if len(*entries) >= 2*maxEntries {
			*entries = append((*entries)[:0], (*entries)[len(*entries)-maxEntries:]...)
		}
		return nil
	}
}

// loadPeriod reads the files of the period, the current one with a tail that follows it.
func (v *viewer) loadPeriod(ctx context.Context, gen int) tea.Cmd {
	dir, period := v.dir, v.period
	label := "Reading the current log"
	if period != "" {
		label = "Reading the log of " + period
	}
	return base.Load(ctx, label, func(ctx context.Context) (periodMsg, error) {
		msg := periodMsg{gen: gen, period: period}
		files, err := logsOf(dir).Files(ctx)
		if err != nil && period == "" {
			msg.unreadable = err
			return msg, nil
		}
		if err != nil {
			return msg, err
		}
		t := now()
		for _, f := range files {
			s := stampOf(f.Name)
			if len(msg.stamps) == 0 || msg.stamps[len(msg.stamps)-1] != s {
				msg.stamps = append(msg.stamps, s)
			}
			if !t.Before(f.Start) && t.Before(f.End) {
				msg.currentPeriod = s
			}
		}
		stamp := period
		if stamp == "" {
			stamp = msg.currentPeriod
		}
		var picked []log.File
		for _, f := range files {
			if stamp != "" && stampOf(f.Name) == stamp {
				picked = append(picked, f)
			}
		}
		add := keepLast(&msg.entries)
		if period != "" {
			return msg, scanFiles(ctx, dir, picked, add)
		}
		// the current period: the compressed files (the tail skips them), then a tail from
		// the first one written, or from the next file that comes
		first := slices.IndexFunc(picked, func(f log.File) bool { return !f.Compressed })
		if first < 0 {
			first = len(picked)
		}
		if err := scanFiles(ctx, dir, picked[:first], add); err != nil {
			return msg, err
		}
		if first < len(picked) {
			msg.tail = log.NewTailAt(dir, picked[first])
		} else if msg.tail, err = log.NewTail(dir); err != nil {
			return msg, err
		}
		err = msg.tail.Next(log.Filter{}, add)
		msg.followed = msg.tail.Following()
		return msg, err
	}, func(msg periodMsg) tea.Msg { return msg })
}

// sameEntries: this session's entries didn't change (the ring only adds at the end).
func sameEntries(a, b []log.Entry) bool {
	return len(a) == len(b) && (len(a) == 0 || a[len(a)-1] == b[len(b)-1])
}

// tick reads the followed log again in a while.
func (v *viewer) tick(ctx context.Context) tea.Cmd {
	if !v.followable {
		return nil
	}
	gen, tail, mu := v.gen, v.tail, v.tailMu
	return tea.Tick(followEvery, func(time.Time) tea.Msg {
		if ctx.Err() != nil {
			return nil
		}
		if tail == nil {
			return entriesMsg{gen: gen, entries: log.Recent(), replace: true}
		}
		mu.Lock()
		defer mu.Unlock()
		var entries []log.Entry
		if err := tail.Next(log.Filter{}, func(e log.Entry) error {
			entries = append(entries, e)
			return nil
		}); err != nil {
			log.Warn("logs: follow: %s", err)
		}
		return entriesMsg{gen: gen, entries: entries, tail: tail, followed: tail.Following()}
	})
}

func (v *viewer) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case base.RestoredMsg:
		return v.start(ctx)
	case periodMsg:
		if msg.gen != v.gen {
			return nil
		}
		if msg.unreadable != nil {
			v.kind, v.note = kindSession, "no log file: "+msg.unreadable.Error()
			return v.start(ctx)
		}
		v.stamps, v.currentPeriod, v.tail, v.followed = msg.stamps, msg.currentPeriod, msg.tail, msg.followed
		if msg.tail != nil {
			v.tailMu = &sync.Mutex{}
		}
		return v.Update(ctx, entriesMsg{gen: msg.gen, entries: msg.entries, replace: true})
	case entriesMsg:
		// what a tail read is added even from an older follow: the tail moved past it; not
		// another tail's (another period)
		if (msg.replace && msg.gen != v.gen) || (!msg.replace && msg.tail != v.tail) {
			return nil
		}
		first := !v.loaded
		v.loaded = true
		if msg.followed != "" {
			v.followed = msg.followed
		}
		switch {
		case v.kind == kindSession && !first && sameEntries(v.entries, msg.entries):
		case msg.replace || len(msg.entries) > 0:
			v.setEntries(msg.entries, msg.replace)
		}
		if first && v.kind == kindSearch && len(v.matches) > 0 {
			v.goTo(len(v.matches) - 1)
		}
		if msg.gen != v.gen {
			return nil
		}
		return v.tick(ctx)
	case search.SearchMsg:
		v.setMatcher(newMatcher(string(msg)))
		return nil
	case search.SearchCancelMsg:
		v.layout()
		return nil
	case tea.KeyPressMsg:
		if v.find.Searching() {
			return v.find.Update(msg)
		}
		return v.key(ctx, msg)
	}
	if v.find.Searching() {
		return v.find.Update(msg)
	}
	return nil
}

func (v *viewer) key(ctx context.Context, msg tea.KeyPressMsg) tea.Cmd {
	k := v.keys
	page := max(1, v.height()-1)
	switch {
	case v.main && key.Matches(msg, k.files):
		return push(newFiles(v.dir))
	case v.main && key.Matches(msg, k.prevPeriod):
		if p, ok := v.stepPeriod(-1); ok {
			return v.showPeriod(ctx, p)
		}
	case v.main && key.Matches(msg, k.nextPeriod):
		if p, ok := v.stepPeriod(1); ok {
			return v.showPeriod(ctx, p)
		}
	case key.Matches(msg, k.find):
		cmd := v.find.Focus()
		v.layout()
		return cmd
	case key.Matches(msg, k.clear) && v.matcher != nil:
		v.setMatcher(nil)
	case key.Matches(msg, k.narrow) && v.matcher != nil:
		v.narrow = !v.narrow
		v.filter()
	case key.Matches(msg, k.next):
		v.step(1)
	case key.Matches(msg, k.prev):
		v.step(-1)
	case key.Matches(msg, k.level):
		i := 0
		for j, l := range levels {
			if l == v.level {
				i = j
			}
		}
		v.level = levels[(i+1)%len(levels)]
		v.filter()
	case key.Matches(msg, k.copy):
		var sb strings.Builder
		for _, i := range v.shown {
			sb.WriteString(v.entries[i].String())
			sb.WriteByte('\n')
		}
		return base.Notify(util.CopyNotice(sb.String(), "Copied "+stringutil.Quantify(len(v.shown), "entry", "entries")))
	case key.Matches(msg, k.up):
		v.scroll(-1)
	case key.Matches(msg, k.down):
		v.scroll(1)
	case key.Matches(msg, k.pageUp):
		v.scroll(-page)
	case key.Matches(msg, k.pageDown):
		v.scroll(page)
	case key.Matches(msg, k.top):
		v.follow = false
		v.top = 0
	case key.Matches(msg, k.bottom):
		v.follow = v.followable
		v.top = v.maxTop()
	}
	return nil
}

// setEntries replaces the entries, or adds them: only the added ones are matched, and
// shown and measured unless the first ones were dropped.
func (v *viewer) setEntries(entries []log.Entry, replace bool) {
	from := len(v.entries)
	if replace {
		v.entries, v.hits, v.dropped, from = nil, nil, 0, 0
	}
	v.entries = append(v.entries, entries...)
	v.hits = append(v.hits, v.matchAll(entries)...)
	whole := replace
	// a tenth more first: all is shown again once some go
	if n := len(v.entries) - maxEntries; n > maxEntries/10 {
		v.entries = append(v.entries[:0], v.entries[n:]...)
		v.hits = append(v.hits[:0], v.hits[n:]...)
		v.dropped += n
		whole = true
	}
	if len(v.entries) > 0 {
		first, last := v.entries[0].Time, v.entries[len(v.entries)-1].Time
		dated := first.Format(time.DateOnly) != last.Format(time.DateOnly)
		// the stamps' width changes
		whole = whole || dated != v.dated
		v.dated = dated
	}
	if whole {
		v.filter()
		return
	}
	n := len(v.shown)
	v.pick(from)
	v.measure(n)
	if v.follow {
		v.top = v.maxTop()
	}
}

// matchAll is whether each entry matches, all false without a find.
func (v *viewer) matchAll(entries []log.Entry) []bool {
	hits := make([]bool, len(entries))
	if v.matcher != nil {
		for i, e := range entries {
			hits[i] = v.matcher.match(e.Message)
		}
	}
	return hits
}

// pick adds the entries from the index on to the shown ones (and their matches).
func (v *viewer) pick(from int) {
	narrowed := v.narrow && v.matcher != nil
	for i := from; i < len(v.entries); i++ {
		if v.entries[i].Level < v.level || (narrowed && !v.hits[i]) {
			continue
		}
		if v.matcher != nil && v.hits[i] {
			v.matches = append(v.matches, len(v.shown))
		}
		v.shown = append(v.shown, i)
	}
}

// filter picks the shown entries and their matches, keeping the current match (or the
// top entry) in view.
func (v *viewer) filter() {
	var keep = -1
	if v.current >= 0 {
		keep = v.shown[v.matches[v.current]]
	} else if i := v.entryAt(v.top); i >= 0 {
		keep = v.shown[i]
	}
	v.shown, v.matches = v.shown[:0], v.matches[:0]
	v.pick(0)
	v.current = -1
	if v.matcher != nil && keep >= 0 {
		for m, i := range v.matches {
			if v.shown[i] == keep {
				v.current = m
			}
		}
	}
	v.layout()
	if v.follow {
		return
	}
	if v.current >= 0 {
		v.goTo(v.current)
		return
	}
	if keep >= 0 {
		i := sort.SearchInts(v.shown, keep)
		if i < len(v.starts) {
			v.top = min(v.starts[i], v.maxTop())
		}
	}
}

// setMatcher finds the query's matcher (nil for none, which also stops narrowing): the
// current match is the first one from the top shown.
func (v *viewer) setMatcher(m *matcher) {
	from := -1
	if i := v.entryAt(v.top); i >= 0 {
		from = v.shown[i]
	}
	v.matcher = m
	if m == nil {
		v.narrow = false
	}
	v.hits = v.matchAll(v.entries)
	v.current = -1
	v.filter()
	if m == nil || len(v.matches) == 0 {
		return
	}
	i := sort.Search(len(v.matches), func(i int) bool { return v.shown[v.matches[i]] >= from })
	if v.follow || i == len(v.matches) {
		i = len(v.matches) - 1
	}
	v.goTo(i)
}

// step goes to the next (1) or previous (-1) match, around.
func (v *viewer) step(d int) {
	if len(v.matches) == 0 {
		return
	}
	m := v.current
	if m < 0 {
		m = sort.SearchInts(v.matches, max(0, v.entryAt(v.top)))
		if d < 0 {
			m--
		}
	} else {
		m += d
	}
	v.goTo((m%len(v.matches) + len(v.matches)) % len(v.matches))
}

// goTo makes the match current and shows it a third down the view.
func (v *viewer) goTo(m int) {
	v.current = m
	v.follow = false
	v.top = max(0, min(v.starts[v.matches[m]]-v.height()/3, v.maxTop()))
}

func (v *viewer) scroll(n int) {
	v.top = max(0, min(v.top+n, v.maxTop()))
	v.follow = v.followable && n > 0 && v.top == v.maxTop()
}

func (v *viewer) maxTop() int { return max(0, v.rows-v.height()) }

// entryAt is the shown entry at a row, -1 for none.
func (v *viewer) entryAt(row int) int {
	if len(v.starts) == 0 {
		return -1
	}
	return max(0, sort.Search(len(v.starts), func(i int) bool { return v.starts[i] > row })-1)
}

// layout measures the shown entries at the width.
func (v *viewer) layout() {
	v.width = max(1, v.size.Width)
	v.heights, v.starts, v.rows = v.heights[:0], v.starts[:0], 0
	v.measure(0)
	if v.follow {
		v.top = v.maxTop()
	}
	v.top = min(v.top, v.maxTop())
}

// measure adds the rows of the shown entries from the index on.
func (v *viewer) measure(from int) {
	for i := from; i < len(v.shown); i++ {
		h := 0
		if v.header(i) != "" {
			h++
		}
		for line := range strings.SplitSeq(v.plain(i), "\n") {
			h += max(1, (ansi.StringWidth(line)+v.width-1)/v.width)
		}
		v.heights = append(v.heights, h)
		v.starts = append(v.starts, v.rows)
		v.rows += h
	}
}

// header is the file of a search's shown entry when it's another than the previous one's.
func (v *viewer) header(i int) string {
	if v.kind != kindSearch {
		return ""
	}
	f := v.entries[v.shown[i]].File
	if i > 0 && v.entries[v.shown[i-1]].File == f {
		return ""
	}
	return "── " + f
}

func (v *viewer) stamp(e log.Entry) string {
	if v.dated {
		return e.Time.Format("01-02 15:04:05")
	}
	return e.Time.Format(time.TimeOnly)
}

func levelName(l log.Level) string {
	return fmt.Sprintf("%-5s", l.String())
}

// plain is the shown entry's text without styles.
func (v *viewer) plain(i int) string {
	e := v.entries[v.shown[i]]
	return v.stamp(e) + " " + levelName(e.Level) + " " + e.Message
}

// render is the shown entry's rows.
func (v *viewer) render(i int) []string {
	e := v.entries[v.shown[i]]
	stamp := style.Normal.Secondary.Render(v.stamp(e))
	current := v.current >= 0 && v.matches[v.current] == i
	if current {
		stamp = lipgloss.NewStyle().Reverse(true).Render(v.stamp(e))
	}
	level := levelName(e.Level)
	switch e.Level {
	case log.LevelDebug:
		level = style.Normal.Secondary.Render(level)
	case log.LevelWarn:
		level = style.Bold.Warning.Render(level)
	case log.LevelError:
		level = style.Bold.Error.Render(level)
	}
	msg := e.Message
	// every match is highlighted, only the visible ones are looked for
	if v.matcher != nil && v.hits[v.shown[i]] {
		st := matchStyle
		if current {
			st = currentMatchStyle
		}
		msg = highlight(msg, v.matcher.spans(msg), st)
	}
	var rows []string
	if h := v.header(i); h != "" {
		rows = append(rows, style.Bold.Accent.Render(ansi.Truncate(h, v.width, "…")))
	}
	text := ansi.Hardwrap(stamp+" "+level+" "+msg, v.width, true)
	return append(rows, strings.Split(text, "\n")...)
}

func (v *viewer) View() string {
	var out []string
	if v.find.Searching() {
		out = append(out, v.find.View(), "")
	}
	switch {
	case !v.loaded && v.kind != kindSession:
		return strings.Join(out, "\n")
	case len(v.shown) == 0:
		what := "No entries"
		switch {
		case len(v.entries) > 0:
			what = "No entries at " + strings.ToLower(v.level.String()) + " and up"
		case v.kind == kindSearch:
			what = "Nothing matches in the logs"
		}
		return strings.Join(append(out, style.Italic.Secondary.Render(what)), "\n")
	}
	height := v.height()
	i := v.entryAt(v.top)
	skip := v.top - v.starts[i]
	var rows []string
	for ; i < len(v.shown) && len(rows) < height+skip; i++ {
		rows = append(rows, v.render(i)...)
	}
	rows = rows[min(skip, len(rows)):]
	rows = rows[:min(height, len(rows))]
	return strings.Join(append(out, rows...), "\n")
}
