package base

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func ctrlW() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlW} }

// chain is home, library, manga, chapters, with chapters shown.
func chain(t *testing.T) (*model, []*keyedState) {
	t.Helper()
	windows := []*keyedState{keyed("Home", "home"), keyed("Library", "library"), keyed("Manga", "library/1"), keyed("Chapters", "chapters/p/1")}
	m := newTestModel(t, windows[0])
	for _, w := range windows[1:] {
		m.Update(State(w))
	}
	return m, windows
}

func press(m *model, keys ...tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = m.Update(k)
	}
	return cmd
}

func TestSwitcherJumpKeepsWindowsAhead(t *testing.T) {
	m, w := chain(t)
	press(m, ctrlW())
	if m.switcher == nil {
		t.Fatal("ctrl+w didn't open the switcher")
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Windows", "Home", "  Library", "    Manga", "Chapters (shown)"} {
		if !strings.Contains(view, want) {
			t.Errorf("switcher view lacks %q:\n%s", want, view)
		}
	}
	// up twice: Library
	press(m, keyMsg("k"), tea.KeyMsg{Type: tea.KeyUp}, tea.KeyMsg{Type: tea.KeyEnter})
	if m.switcher != nil {
		t.Error("the switcher stayed open")
	}
	checkTitles(t, m, "Home", "Library")
	if len(m.forward) != 2 || m.forward[0] != State(w[2]) || m.forward[1] != State(w[3]) {
		t.Fatalf("ahead %v, want Manga, Chapters", m.forward)
	}
	for _, s := range w {
		if s.destroyed {
			t.Errorf("%s destroyed", s.title)
		}
	}
	if w[3].ctx.Err() == nil {
		t.Error("a window ahead has a live ctx")
	}
	if !strings.Contains(ansi.Strip(m.viewBreadcrumb()), "2 ahead") {
		t.Errorf("breadcrumbs %q don't tell the windows ahead", ansi.Strip(m.viewBreadcrumb()))
	}

	// and forward again: the window is restored, with a new ctx
	press(m, ctrlW(), keyMsg("j"), keyMsg("l"), tea.KeyMsg{Type: tea.KeyEnter})
	checkTitles(t, m, "Home", "Library", "Manga", "Chapters")
	if len(m.forward) != 0 {
		t.Errorf("%d ahead, want none", len(m.forward))
	}
	if req, ok := restoredWith(&w[3].fakeState); !ok || req != nil {
		t.Errorf("chapters not restored: %v %v", req, ok)
	}
	if w[3].ctx.Err() != nil {
		t.Error("the restored window's ctx is done")
	}
}

func TestPushOfWindowAheadJumpsForward(t *testing.T) {
	m, w := chain(t)
	m.Update(State(keyed("Library again", "library")))
	checkTitles(t, m, "Home", "Library")
	again := keyed("Manga again", "library/1")
	m.Update(State(again))
	checkTitles(t, m, "Home", "Library", "Manga")
	if len(m.forward) != 1 || m.forward[0] != State(w[3]) {
		t.Errorf("ahead %v, want Chapters", m.forward)
	}
	if !again.destroyed || again.ctx != nil {
		t.Error("the request was shown or kept")
	}
	if req, ok := restoredWith(&w[2].fakeState); !ok || req != State(again) {
		t.Errorf("restored with %v, want the request", req)
	}
}

func TestNewWindowDropsWindowsAhead(t *testing.T) {
	m, w := chain(t)
	m.Update(JumpTo("library")())
	checkTitles(t, m, "Home", "Library")
	_, cmd := m.Update(State(keyed("Trash", "trash")))
	checkTitles(t, m, "Home", "Library", "Trash")
	if !w[2].destroyed || !w[3].destroyed || len(m.forward) != 0 {
		t.Error("the windows ahead weren't closed")
	}
	if !containsNotification(cmd, "Closed 2 windows ahead") {
		t.Error("no notice of the closed windows")
	}
}

func TestBackClosesWindowsAhead(t *testing.T) {
	m, w := chain(t)
	m.Update(JumpTo("library/1")())
	m.Update(BackMsg{Steps: 1})
	checkTitles(t, m, "Home", "Library")
	if !w[2].destroyed || !w[3].destroyed || len(m.forward) != 0 {
		t.Error("esc didn't close the window and the ones ahead of it")
	}
}

func TestSwitcherClosesAbove(t *testing.T) {
	m, w := chain(t)
	// from the chapters, close everything over the library: back to it
	press(m, ctrlW(), keyMsg("k"), keyMsg("k"), keyMsg("d"))
	checkTitles(t, m, "Home", "Library")
	if !w[2].destroyed || !w[3].destroyed || w[1].destroyed {
		t.Error("not exactly the windows over the library were closed")
	}
	if req, ok := restoredWith(&w[1].fakeState); !ok || req != nil {
		t.Error("the library wasn't restored")
	}
	press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.switcher != nil {
		t.Error("esc didn't close the switcher")
	}

	// only windows ahead: the shown one stays
	m, w = chain(t)
	m.Update(JumpTo("library")())
	press(m, ctrlW(), keyMsg("j"), keyMsg("d"), ctrlW())
	checkTitles(t, m, "Home", "Library")
	if len(m.forward) != 1 || w[2].destroyed || !w[3].destroyed {
		t.Errorf("ahead %v: want Manga kept, Chapters closed", m.forward)
	}
}

func TestJumpToClosedWindow(t *testing.T) {
	m, _ := chain(t)
	_, cmd := m.Update(JumpTo("watches")())
	checkTitles(t, m, "Home", "Library", "Manga", "Chapters")
	if !containsNotification(cmd, "That screen isn't open anymore") {
		t.Error("no notice")
	}
}

type fakeActivity struct {
	running []string
	msgs    chan tea.Msg
	stopped bool
}

type noticeMsg string

func (n noticeMsg) Notice() string { return string(n) }

func (a *fakeActivity) Wait(ctx context.Context) tea.Msg {
	select {
	case msg := <-a.msgs:
		return msg
	case <-ctx.Done():
		return nil
	}
}
func (a *fakeActivity) Status() string         { return "1 running" }
func (a *fakeActivity) Running() []string      { return a.running }
func (a *fakeActivity) Shutdown(time.Duration) { a.stopped = true }

func withActivity(t *testing.T, a Activity) {
	t.Helper()
	old := CurrentActivity
	CurrentActivity = a
	t.Cleanup(func() { CurrentActivity = old })
}

func TestQuitAsksWhileJobsRun(t *testing.T) {
	a := &fakeActivity{running: []string{"download One Piece (3 chapters)"}, msgs: make(chan tea.Msg, 1)}
	withActivity(t, a)
	home := &fakeState{title: "home"}
	m := newTestModel(t, home)
	if !strings.Contains(ansi.Strip(m.View()), "1 running") {
		t.Error("the header lacks the activity")
	}

	cmd := press(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil || m.quitAsk == nil {
		t.Fatal("ctrl+c quit without asking")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Quit?") || !strings.Contains(view, "One Piece") {
		t.Errorf("the question doesn't list the jobs:\n%s", view)
	}
	// other keys don't reach the state
	press(m, keyMsg("x"))
	if home.received(keyMsg("x")) {
		t.Error("a key went to the state while asking")
	}
	press(m, keyMsg("n"))
	if m.quitAsk != nil {
		t.Fatal("n didn't stay")
	}
	press(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	cmd = press(m, keyMsg("y"))
	if cmd == nil {
		t.Fatal("y didn't quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("y didn't quit")
	}
	m.shutdown(time.Second)
	if !a.stopped {
		t.Error("the activity wasn't shut down")
	}

	// nothing running: no question
	a.running = nil
	m = newTestModel(t, &fakeState{title: "home"})
	if cmd := press(m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Error("ctrl+c asked without jobs")
	}
}

func TestActivityReachesState(t *testing.T) {
	a := &fakeActivity{msgs: make(chan tea.Msg, 1)}
	withActivity(t, a)
	home := &fakeState{title: "home"}
	m := newTestModel(t, home)
	a.msgs <- noticeMsg("Downloaded One Piece")
	msg := m.listenActivity()()
	m.Update(msg)
	if !home.received(noticeMsg("Downloaded One Piece")) {
		t.Error("the state didn't get the activity msg")
	}
	if m.notification != "Downloaded One Piece" {
		t.Errorf("notification %q", m.notification)
	}
}

func TestDownloadsKey(t *testing.T) {
	old := NewDownloads
	NewDownloads = func() State { return keyed("Downloads", "downloads") }
	t.Cleanup(func() { NewDownloads = old })
	m, _ := chain(t)
	press(m, keyMsg("Q"))
	checkTitles(t, m, "Home", "Library", "Manga", "Chapters", "Downloads")
	press(m, ctrlW(), keyMsg("k"), tea.KeyMsg{Type: tea.KeyEnter}, keyMsg("Q"))
	checkTitles(t, m, "Home", "Library", "Manga", "Chapters", "Downloads")
}
