package logs

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/util"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// maxEntries is how many entries a viewer keeps, the latest.
const maxEntries = 50000

// followEvery is how often a followed log is read again.
var followEvery = 500 * time.Millisecond

// levels are the minimum levels tab goes through.
var levels = []log.Level{log.LevelDebug, log.LevelInfo, log.LevelWarn, log.LevelError}

type kind int

const (
	kindSession kind = iota
	kindFile
	kindSearch
)

type viewerKeys struct {
	up, down, pageUp, pageDown, top, bottom, find, next, prev, level, copy, clear key.Binding
}

var _ base.State = (*viewer)(nil)

// viewer is a pager of log entries: this session's, a file's (and the next ones, when it's
// the newest) or a search's through every file.
type viewer struct {
	kind  kind
	dir   string
	file  log.File
	query string
	// followable: new entries come (this session, the newest file), follow shows them
	followable, follow bool
	tail               *log.Tail
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
	pattern *regexp.Regexp
	// shown are the indexes of the entries at the level, matches the ones of shown that
	// match the pattern, current the shown match (-1 for none)
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
		find:    search.New("Find (regexp)...", "", 64, 0),
		keys: viewerKeys{
			up:       util.BindNamedKey("↑/k", "up", "up", "k"),
			down:     util.BindNamedKey("↓/j", "down", "down", "j"),
			pageUp:   util.BindNamedKey("pgup/b", "page up", "pgup", "b"),
			pageDown: util.BindNamedKey("pgdn/f/space", "page down", "pgdown", "f", " "),
			top:      util.BindNamedKey("g/home", "top", "g", "home"),
			bottom:   util.BindNamedKey("G/end", "end", "G", "end"),
			find:     util.Bind("find", "/"),
			next:     base.KeyNextMatch,
			prev:     base.KeyPrevMatch,
			level:    base.KeyLevel,
			copy:     base.KeyCopy,
			clear:    util.Bind("clear find", "esc"),
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

func newSearch(dir, query string) *viewer {
	v := newViewer(kindSearch)
	v.dir, v.query = dir, query
	v.pattern = log.Pattern(query, false)
	return v
}

func (v *viewer) Key() string {
	switch v.kind {
	case kindSession:
		return viewerKey(sessionKey)
	case kindSearch:
		return "logs-search/" + v.query
	}
	return viewerKey(v.file.Name)
}

func (v *viewer) Title() base.Title {
	switch v.kind {
	case kindSession:
		return base.Title{Text: "Log: this session"}
	case kindSearch:
		return base.Title{Text: "Log search: " + v.query}
	}
	return base.Title{Text: "Log: " + v.file.Name}
}

func (v *viewer) Subtitle() string {
	var parts []string
	switch {
	case v.kind == kindSearch:
		parts = append(parts, stringutil.Quantify(len(v.entries), "entry", "entries")+" in every file")
	case v.tail != nil && v.tail.Following() != v.file.Name:
		parts = append(parts, "now in "+v.tail.Following())
	}
	if v.level != log.LevelDebug {
		parts = append(parts, strings.ToLower(v.level.String())+" and up")
	}
	if v.pattern != nil {
		switch {
		case len(v.matches) == 0:
			parts = append(parts, "no match")
		case v.current < 0:
			parts = append(parts, stringutil.Quantify(len(v.matches), "match", "matches"))
		default:
			parts = append(parts, fmt.Sprintf("match %d/%d", v.current+1, len(v.matches)))
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
func (v *viewer) Backable() bool { return !v.find.Searching() && v.pattern == nil }

func (v *viewer) KeyMap() help.KeyMap {
	if v.find.Searching() {
		return inputKeys("find")
	}
	k := v.keys
	k.clear.SetEnabled(v.pattern != nil)
	k.next.SetEnabled(len(v.matches) > 0)
	k.prev.SetEnabled(len(v.matches) > 0)
	k.copy.SetEnabled(len(v.shown) > 0)
	end := k.bottom
	if v.followable {
		end.SetHelp(end.Help().Key, "follow")
	}
	return util.KeyMap{
		Short: []key.Binding{k.find, k.next, k.level, end, k.clear},
		Full: [][]key.Binding{
			{k.find, k.next, k.prev, k.clear},
			{k.level, k.copy},
			{k.up, k.down, k.pageUp, k.pageDown},
			{k.top, end},
		},
	}
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

// entriesMsg are entries loaded (replace) or followed.
type entriesMsg struct {
	gen     int
	entries []log.Entry
	replace bool
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
	label := "Reading " + v.file.Name
	if v.kind == kindSearch {
		label = "Searching the logs"
	}
	if v.followable {
		v.tail, v.tailMu = log.NewTailAt(v.dir, v.file), &sync.Mutex{}
	}
	tail, file, dir, pattern := v.tail, v.file, v.dir, v.pattern
	return base.Load(ctx, label, func(ctx context.Context) ([]log.Entry, error) {
		var entries []log.Entry
		add := func(e log.Entry) error {
			entries = append(entries, e)
			if len(entries) >= 2*maxEntries {
				entries = append(entries[:0], entries[len(entries)-maxEntries:]...)
			}
			return nil
		}
		switch {
		case tail != nil:
			return entries, tail.Next(log.Filter{}, add)
		case pattern != nil:
			files, err := log.Files(dir)
			if err != nil {
				return nil, err
			}
			return entries, log.Scan(ctx, files, log.Filter{Pattern: pattern}, add)
		default:
			return entries, log.Scan(ctx, []log.File{file}, log.Filter{}, add)
		}
	}, func(entries []log.Entry) tea.Msg {
		return entriesMsg{gen: gen, entries: entries, replace: true}
	})
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
		return entriesMsg{gen: gen, entries: entries}
	})
}

func (v *viewer) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case base.RestoredMsg:
		return v.start(ctx)
	case entriesMsg:
		// what a tail read is added even from an older follow: the tail moved past it
		if msg.replace && msg.gen != v.gen {
			return nil
		}
		first := !v.loaded
		v.loaded = true
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
		v.setPattern(log.Pattern(string(msg), false))
		return nil
	case search.SearchCancelMsg:
		v.layout()
		return nil
	case tea.KeyMsg:
		if v.find.Searching() {
			return v.find.Update(msg)
		}
		return v.key(msg)
	}
	if v.find.Searching() {
		return v.find.Update(msg)
	}
	return nil
}

func (v *viewer) key(msg tea.KeyMsg) tea.Cmd {
	k := v.keys
	page := max(1, v.height()-1)
	switch {
	case key.Matches(msg, k.find):
		cmd := v.find.Focus()
		v.layout()
		return cmd
	case key.Matches(msg, k.clear) && v.pattern != nil:
		v.setPattern(nil)
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

// setEntries replaces the entries, or adds them.
func (v *viewer) setEntries(entries []log.Entry, replace bool) {
	if replace {
		v.entries, v.dropped = nil, 0
	}
	v.entries = append(v.entries, entries...)
	if n := len(v.entries) - maxEntries; n > 0 {
		v.entries = append(v.entries[:0], v.entries[n:]...)
		v.dropped += n
	}
	if len(v.entries) > 0 {
		first, last := v.entries[0].Time, v.entries[len(v.entries)-1].Time
		v.dated = first.Format(time.DateOnly) != last.Format(time.DateOnly)
	}
	v.filter()
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
	v.shown = v.shown[:0]
	for i, e := range v.entries {
		if e.Level >= v.level {
			v.shown = append(v.shown, i)
		}
	}
	v.match()
	v.current = -1
	if v.pattern != nil && keep >= 0 {
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

func (v *viewer) match() {
	v.matches = v.matches[:0]
	if v.pattern == nil {
		return
	}
	for i, e := range v.shown {
		if v.pattern.MatchString(v.entries[e].Message) {
			v.matches = append(v.matches, i)
		}
	}
}

// setPattern finds the pattern (nil for none): the current match is the first one from
// the top shown.
func (v *viewer) setPattern(p *regexp.Regexp) {
	v.pattern = p
	v.match()
	v.current = -1
	v.layout()
	if p == nil || len(v.matches) == 0 {
		return
	}
	from := v.entryAt(v.top)
	m := sort.SearchInts(v.matches, max(0, from))
	if v.follow || m == len(v.matches) {
		m = len(v.matches) - 1
	}
	v.goTo(m)
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
	v.heights = v.heights[:0]
	v.starts = v.starts[:0]
	v.rows = 0
	for i := range v.shown {
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
	if v.follow {
		v.top = v.maxTop()
	}
	v.top = min(v.top, v.maxTop())
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
	if v.current >= 0 && v.matches[v.current] == i {
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
	if v.pattern != nil {
		msg = highlight(v.pattern, msg)
	}
	var rows []string
	if h := v.header(i); h != "" {
		rows = append(rows, style.Bold.Accent.Render(ansi.Truncate(h, v.width, "…")))
	}
	text := ansi.Hardwrap(stamp+" "+level+" "+msg, v.width, true)
	return append(rows, strings.Split(text, "\n")...)
}

var matchStyle = lipgloss.NewStyle().Reverse(true).Foreground(lipgloss.Color("#EBCA89"))

func highlight(p *regexp.Regexp, s string) string {
	locs := p.FindAllStringIndex(s, -1)
	if locs == nil {
		return s
	}
	var sb strings.Builder
	last := 0
	for _, l := range locs {
		if l[1] == l[0] {
			continue
		}
		sb.WriteString(s[last:l[0]])
		sb.WriteString(matchStyle.Render(s[l[0]:l[1]]))
		last = l[1]
	}
	sb.WriteString(s[last:])
	return sb.String()
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
