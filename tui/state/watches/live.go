package watches

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

// feed is a subscription to the live events of the checks (Manager.Subscribe), read one
// event at a time with next. It ends with the ctx of the screen that subscribed.
type feed struct {
	ch <-chan watch.LiveEvent
}

// feedMsg is a subscription made.
type feedMsg struct{ feed *feed }

// liveMsg is an event of a feed, closed when it ended.
type liveMsg struct {
	feed   *feed
	e      watch.LiveEvent
	closed bool
}

// subscribe subscribes to the manager's live events. Without them (an older server) the
// screens still poll the status, it's only logged.
func subscribe(ctx context.Context, m watch.Manager) tea.Cmd {
	return base.Load(ctx, "", func(ctx context.Context) (*feed, error) {
		ch, err := m.Subscribe(ctx)
		if err != nil {
			log.Warn("watch: live events: %s", err)
			return nil, nil
		}
		return &feed{ch: ch}, nil
	}, func(f *feed) tea.Msg {
		if f == nil {
			return nil
		}
		return feedMsg{feed: f}
	})
}

func (f *feed) next() tea.Cmd {
	return func() tea.Msg {
		e, ok := <-f.ch
		return liveMsg{feed: f, e: e, closed: !ok}
	}
}

// liveWatch is what a watch being checked is doing.
type liveWatch struct {
	provider string
	state    string
	chapter  *float32
	since    time.Time
}

// spinnerFrames animate the watches being checked.
var spinnerFrames = base.DotSpinner.Frames

type spinMsg struct{ gen int }

const spinEvery = 150 * time.Millisecond

// eventLine is the event in the live log: its time and message, with the manga and the
// provider when every watch's events are shown.
func eventLine(e watch.LiveEvent, all bool) string {
	line := e.Time.Local().Format(time.TimeOnly) + "  "
	if all {
		switch {
		case e.Title != "" && e.Kind != watch.LiveStart:
			line += style.Normal.Secondary.Render(e.Title) + ": "
		case e.Provider != "" && e.Watch == 0:
			line += style.Normal.Secondary.Render(e.Provider) + ": "
		}
	}
	msg := e.Message
	switch {
	case e.Kind == watch.LiveDone && e.Status == storage.StatusError, e.Kind == watch.LiveChapterDone && strings.Contains(msg, " failed: "):
		msg = style.Normal.Error.Render(msg)
	case e.Kind == watch.LiveWait:
		msg = style.Normal.Warning.Render(msg)
	case e.Kind == watch.LiveDone && e.Status == storage.StatusNew:
		msg = style.Normal.Success.Render(msg)
	case e.Kind == watch.LiveRunStart, e.Kind == watch.LiveRunDone:
		msg = style.Bold.Accent.Render(msg)
	}
	return line + msg
}

// maxLiveLines is how many lines a live log keeps.
const maxLiveLines = 2000

var _ base.State = (*liveState)(nil)

// liveState is the live log of the checks: of one watch (what its current check does),
// or of every running check.
type liveState struct {
	manager watch.Manager
	// watch is the one shown, nil for every check
	watch *watch.Watch
	feed  *feed
	lines []string
	// progress is the line of each watch's page progress, rewritten until another of its
	// events comes
	progress map[int64]int
	// offset is how many lines from the end the view is scrolled up, 0 follows
	offset int
	size   base.Size

	up, down, pageUp, pageDown, follow key.Binding
}

func newLiveState(m watch.Manager, w *watch.Watch) *liveState {
	return &liveState{
		manager:  m,
		watch:    w,
		up:       util.BindNamedKey("↑/k", "up", "up", "k"),
		down:     util.BindNamedKey("↓/j", "down", "down", "j"),
		pageUp:   util.BindNamedKey("pgup/b", "page up", "pgup", "b"),
		pageDown: util.BindNamedKey("pgdn/f/space", "page down", "pgdown", "f", " "),
		follow:   util.BindNamedKey("G/end", "follow", "G", "end"),
	}
}

func (s *liveState) Backable() bool  { return true }
func (s *liveState) Inputting() bool { return false }
func (s *liveState) Status() string  { return "" }

func (s *liveState) KeyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.up, s.down, s.pageUp, s.pageDown, s.follow}}
}

func (s *liveState) Title() base.Title {
	if s.watch != nil {
		return base.Title{Text: "Live: " + s.watch.Title}
	}
	return base.Title{Text: "Running checks"}
}

func (s *liveState) Subtitle() string {
	what := "every check of this process"
	if _, remote := s.manager.(*watch.Remote); remote {
		what = "every check of the server"
	}
	if s.watch != nil {
		what = s.watch.Provider
	}
	parts := []string{what}
	if s.offset > 0 {
		parts = append(parts, fmt.Sprintf("scrolled up %d", s.offset))
	}
	return strings.Join(parts, " · ")
}

func (s *liveState) Resize(size base.Size) tea.Cmd {
	s.size = size
	return nil
}

func (s *liveState) Init(ctx context.Context) tea.Cmd {
	return subscribe(ctx, s.manager)
}

// shows is true for the events of the log.
func (s *liveState) shows(e watch.LiveEvent) bool {
	return s.watch == nil || e.Watch == s.watch.ID
}

func (s *liveState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case base.RestoredMsg:
		s.feed = nil
		return subscribe(ctx, s.manager)
	case feedMsg:
		// a new subscription replays the recent events
		s.feed, s.lines, s.offset, s.progress = msg.feed, nil, 0, nil
		return msg.feed.next()
	case liveMsg:
		if msg.feed != s.feed || msg.closed {
			return nil
		}
		if s.shows(msg.e) {
			s.onEvent(msg.e)
		}
		return s.feed.next()
	case tea.KeyMsg:
		page := max(1, s.height()-1)
		switch {
		case key.Matches(msg, s.up):
			s.scroll(1)
		case key.Matches(msg, s.down):
			s.scroll(-1)
		case key.Matches(msg, s.pageUp):
			s.scroll(page)
		case key.Matches(msg, s.pageDown):
			s.scroll(-page)
		case key.Matches(msg, s.follow):
			s.offset = 0
		}
	}
	return nil
}

// onEvent adds the event's line, a page progress replaces the watch's previous one.
func (s *liveState) onEvent(e watch.LiveEvent) {
	line := eventLine(e, s.watch == nil)
	if e.Kind != watch.LiveProgress {
		delete(s.progress, e.Watch)
		s.add(line)
		return
	}
	if i, ok := s.progress[e.Watch]; ok {
		s.lines[i] = line
		return
	}
	s.add(line)
	if s.progress == nil {
		s.progress = map[int64]int{}
	}
	s.progress[e.Watch] = len(s.lines) - 1
}

func (s *liveState) add(line string) {
	s.lines = append(s.lines, line)
	if n := len(s.lines) - maxLiveLines; n > 0 {
		s.lines = s.lines[n:]
		for w, i := range s.progress {
			if i < n {
				delete(s.progress, w)
			} else {
				s.progress[w] = i - n
			}
		}
	}
	if s.offset > 0 {
		// keep the lines being read in place
		s.offset++
	}
}

func (s *liveState) scroll(n int) {
	s.offset = min(max(0, s.offset+n), max(0, len(s.lines)-s.height()))
}

func (s *liveState) height() int { return max(1, s.size.Height) }

func (s *liveState) View() string {
	if len(s.lines) == 0 {
		what := "No check is running"
		if s.watch != nil {
			what = s.watch.Title + " isn't being checked; its next check shows here as it happens"
		}
		return style.Italic.Secondary.Render(what)
	}
	end := len(s.lines) - s.offset
	start := max(0, end-s.height())
	lines := s.lines[start:end]
	if s.size.Width > 0 {
		cut := make([]string, len(lines))
		for i, l := range lines {
			cut[i] = ansi.Truncate(l, s.size.Width, "…")
		}
		lines = cut
	}
	return strings.Join(lines, "\n")
}
