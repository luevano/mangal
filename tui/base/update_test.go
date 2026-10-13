package base

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/tui/util"
)

type fakeState struct {
	title     string
	inputting bool
	got       []tea.Msg
	ctx       context.Context
	destroyed bool
}

func (s *fakeState) Backable() bool      { return !s.inputting }
func (s *fakeState) Inputting() bool     { return s.inputting }
func (s *fakeState) KeyMap() help.KeyMap { return util.KeyMap{} }
func (s *fakeState) Title() Title        { return Title{Text: s.title} }
func (s *fakeState) Subtitle() string    { return "" }
func (s *fakeState) Status() string      { return "" }
func (s *fakeState) Resize(Size) tea.Cmd { return nil }
func (s *fakeState) View() string        { return s.title }
func (s *fakeState) Destroy()            { s.destroyed = true }

func (s *fakeState) Init(ctx context.Context) tea.Cmd {
	s.ctx = ctx
	return nil
}

func (s *fakeState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	s.ctx = ctx
	s.got = append(s.got, msg)
	return nil
}

func (s *fakeState) received(msg tea.Msg) bool {
	for _, m := range s.got {
		if reflect.DeepEqual(m, msg) {
			return true
		}
	}
	return false
}

func newTestModel(t *testing.T, state State) *model {
	t.Helper()
	m := newModel(context.Background(), state, false, nil)
	m.Init()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	t.Cleanup(func() { m.shutdown(time.Second) })
	return m
}

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// splitLoad returns the start and work cmds of a Load (a tea.Sequence).
func splitLoad(t *testing.T, cmd tea.Cmd) (tea.Msg, tea.Cmd) {
	t.Helper()
	v := reflect.ValueOf(cmd())
	if v.Kind() != reflect.Slice || v.Len() != 2 {
		t.Fatalf("Load isn't a sequence of 2 cmds: %T", v.Interface())
	}
	start := v.Index(0).Interface().(tea.Cmd)
	return start(), v.Index(1).Interface().(tea.Cmd)
}

func TestGlobalKeysWhileInputting(t *testing.T) {
	home := &fakeState{title: "home"}
	m := newTestModel(t, home)
	screen := &fakeState{title: "screen", inputting: true}
	m.Update(State(screen))

	for _, k := range []string{"H", "?"} {
		msg := keyMsg(k)
		_, cmd := m.Update(msg)
		if cmd != nil {
			t.Errorf("%s while inputting returned a cmd", k)
		}
		if !screen.received(msg) {
			t.Errorf("%s while inputting didn't reach the state", k)
		}
	}
	if m.help.ShowAll {
		t.Error("? toggled the help while inputting")
	}

	screen.inputting = false
	if _, cmd := m.Update(keyMsg("H")); cmd == nil {
		t.Fatal("H didn't go home")
	} else if _, ok := cmd().(BackToHomeMsg); !ok {
		t.Error("H didn't go home")
	}
}

type resultMsg struct{}

func TestViewportForwardsOtherMsgs(t *testing.T) {
	s := &fakeState{title: "home"}
	m := newTestModel(t, s)
	m.Update(ShowViewportMsg{Title: "Logs", Content: "lines"})
	if !m.inViewport {
		t.Fatal("viewport not shown")
	}

	m.Update(resultMsg{})
	if !s.received(resultMsg{}) {
		t.Error("a non-key msg didn't reach the state while the viewport was shown")
	}

	key := keyMsg("j")
	m.Update(key)
	if s.received(key) {
		t.Error("a key reached the state while the viewport was shown")
	}
}

func TestNotificationTimeoutKeepsNewer(t *testing.T) {
	m := newTestModel(t, &fakeState{})
	m.Update(NotificationMsg{Message: "old"})
	oldID := m.notificationID
	m.Update(NotificationMsg{Message: "new"})

	m.Update(notificationTimeoutMsg{id: oldID})
	if m.notification != "new" {
		t.Errorf("an old timeout hid the newer notification, got %q", m.notification)
	}
	m.Update(notificationTimeoutMsg{id: m.notificationID})
	if m.notification != "" {
		t.Errorf("notification not hidden, got %q", m.notification)
	}
}

func TestLoadDropsStaleResults(t *testing.T) {
	home := &fakeState{title: "home"}
	m := newTestModel(t, home)
	screen := &fakeState{title: "screen"}
	m.Update(State(screen))
	ctx := screen.ctx

	release := make(chan struct{})
	cmd := Load(ctx, "loading", func(ctx context.Context) (int, error) {
		<-release
		return 1, nil
	}, func(int) tea.Msg { return resultMsg{} })
	if !Busy(ctx) {
		t.Error("not busy with a load running")
	}

	start, work := splitLoad(t, cmd)
	m.Update(start)
	if m.loadingLabel() != "loading" {
		t.Errorf("loading label %q", m.loadingLabel())
	}

	// the user goes back before the result arrives
	m.Update(BackMsg{Steps: 1})
	if !screen.destroyed {
		t.Error("popped state not destroyed")
	}
	if ctx.Err() == nil {
		t.Error("the popped state's ctx isn't cancelled")
	}
	close(release)
	m.Update(work())

	if home.received(resultMsg{}) || screen.received(resultMsg{}) {
		t.Error("a stale result was delivered")
	}
	if m.loadingLabel() != "" {
		t.Error("the loading label stayed")
	}
}

func TestLoadDeliversAndClearsOnlyItsLabel(t *testing.T) {
	s := &fakeState{title: "home"}
	m := newTestModel(t, s)
	ctx := s.ctx

	first, firstWork := splitLoad(t, Load(ctx, "first", func(context.Context) (int, error) { return 0, nil },
		func(int) tea.Msg { return resultMsg{} }))
	second, secondWork := splitLoad(t, Load(ctx, "second", func(context.Context) (int, error) {
		return 0, errors.New("boom")
	}, nil))
	m.Update(first)
	m.Update(second)

	m.Update(firstWork())
	if !s.received(resultMsg{}) {
		t.Error("result not delivered")
	}
	if m.loadingLabel() != "second" {
		t.Errorf("the first load cleared the second's label: %q", m.loadingLabel())
	}
	if !Busy(ctx) {
		t.Error("not busy with the second load running")
	}

	_, cmd := m.Update(secondWork())
	if m.loadingLabel() != "" {
		t.Error("label not cleared")
	}
	if Busy(ctx) {
		t.Error("still busy")
	}
	if cmd == nil {
		t.Fatal("error not shown")
	}
	if msg, ok := cmd().(ShowViewportMsg); !ok || msg.Title != "Error" {
		t.Errorf("error not shown, got %#v", msg)
	}
}

func TestCanceledErrorIgnored(t *testing.T) {
	m := newTestModel(t, &fakeState{})
	if _, cmd := m.Update(errors.Join(errors.New("search"), context.Canceled)); cmd != nil {
		t.Error("a canceled error was shown")
	}
}

func TestShutdownCancels(t *testing.T) {
	s := &fakeState{}
	m := newModel(context.Background(), s, false, nil)
	m.Init()
	m.shutdown(time.Second)
	if s.ctx.Err() == nil {
		t.Error("ctx not cancelled on quit")
	}
	if !s.destroyed {
		t.Error("state not destroyed on quit")
	}
	if _, ok := Track(s.ctx); ok {
		t.Error("work can start after quitting")
	}
}

func TestLoadWithStatus(t *testing.T) {
	s := &fakeState{title: "home"}
	m := newTestModel(t, s)

	gate := make(chan struct{})
	cmd := LoadWithStatus(s.ctx, "Scanning", func(ctx context.Context, status func(string)) (int, error) {
		status("scanning library: 1/2 (x)")
		<-gate
		progress.Report(ctx, progress.Status{Message: "AniList rate limited, retrying in 58s"})
		<-gate
		return 0, nil
	}, func(int) tea.Msg { return resultMsg{} })
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("not a batch of the load and its labels: %T", cmd())
	}
	start, work := splitLoad(t, batch[0])
	m.Update(start)
	if m.loadingLabel() != "Scanning" {
		t.Errorf("label %q", m.loadingLabel())
	}
	done := make(chan tea.Msg)
	go func() { done <- work() }()

	listen := batch[1]
	for _, want := range []string{"Scanning library: 1/2 (x)", "AniList rate limited, retrying in 58s"} {
		_, next := m.Update(listen())
		if m.loadingLabel() != want {
			t.Errorf("label %q, want %q", m.loadingLabel(), want)
		}
		listen = next
		gate <- struct{}{}
	}
	m.Update(<-done)
	if !s.received(resultMsg{}) || m.loadingLabel() != "" {
		t.Errorf("result not delivered or label %q left", m.loadingLabel())
	}
	if msg := listen(); msg != nil {
		t.Errorf("labels still listened to after the load: %#v", msg)
	}
}

// provider text in a title, a notification or the view reaches the terminal without its
// escape sequences (MT-S1)
func TestViewSanitized(t *testing.T) {
	m := newTestModel(t, &fakeState{title: "home"})
	m.Update(State(&fakeState{title: "One Piece\x1b]52;c;ZWNobyBoaQ==\x07"}))
	m.Update(NotificationMsg{Message: "Downloaded \x1b[2J\"ch 1\""})
	v := m.View()
	if strings.Contains(v, "\x1b]52") || strings.Contains(v, "\x1b[2J") {
		t.Errorf("escape sequences in the view: %q", v)
	}
	if !strings.Contains(v, "One Piece") {
		t.Error("the title is gone")
	}
}
