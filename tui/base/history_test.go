package base

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// keyedState is a fakeState with a window key.
type keyedState struct {
	fakeState
	key string
}

func (s *keyedState) Key() string { return s.key }

func keyed(title, key string) *keyedState {
	return &keyedState{fakeState: fakeState{title: title}, key: key}
}

func (m *model) titles() []string {
	var titles []string
	for _, s := range m.history.states {
		titles = append(titles, s.Title().Text)
	}
	return append(titles, m.state.Title().Text)
}

func checkTitles(t *testing.T, m *model, want ...string) {
	t.Helper()
	got := m.titles()
	if len(got) != len(want) {
		t.Fatalf("windows %q, want %q", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("windows %q, want %q", got, want)
		}
	}
}

func restoredWith(s *fakeState) (State, bool) {
	for _, msg := range s.got {
		if r, ok := msg.(RestoredMsg); ok {
			return r.Request, true
		}
	}
	return nil, false
}

func TestPushSameWindowGoesBack(t *testing.T) {
	home := keyed("home", "home")
	m := newTestModel(t, home)
	watches := keyed("watches", "watches")
	details := keyed("details 7", "watch/7")
	manga := keyed("manga 3", "library/3")
	m.Update(State(watches))
	m.Update(State(details))
	m.Update(State(manga))
	checkTitles(t, m, "home", "watches", "details 7", "manga 3")
	oldCtx := details.ctx

	// the manga's watch again, from the library manga
	again := keyed("details 7 (new)", "watch/7")
	m.Update(State(again))
	checkTitles(t, m, "home", "watches", "details 7")
	// the window over it stays, ahead
	if manga.destroyed || len(m.forward) != 1 || m.forward[0] != State(manga) {
		t.Error("the window over it didn't stay ahead")
	}
	if manga.ctx.Err() == nil {
		t.Error("the window ahead has a live ctx")
	}
	if !again.destroyed {
		t.Error("the request wasn't destroyed")
	}
	if again.ctx != nil {
		t.Error("the request was initialized")
	}
	req, ok := restoredWith(&details.fakeState)
	if !ok || req != State(again) {
		t.Errorf("restored with request %v, want the pushed state", req)
	}
	if oldCtx.Err() == nil || details.ctx.Err() != nil {
		t.Error("the restored window didn't get a new ctx")
	}
}

func TestPushShownWindowRestoresIt(t *testing.T) {
	m := newTestModel(t, keyed("home", "home"))
	lib := keyed("library", "library")
	m.Update(State(lib))
	again := keyed("library", "library")
	m.Update(State(again))
	checkTitles(t, m, "home", "library")
	if req, ok := restoredWith(&lib.fakeState); !ok || req != State(again) {
		t.Errorf("not restored with the request: %v", req)
	}
}

func TestPushHomeAgain(t *testing.T) {
	home := keyed("home", "home")
	m := newTestModel(t, home)
	m.Update(State(keyed("library", "library")))
	m.Update(State(keyed("home", "home")))
	checkTitles(t, m, "home")
}

func TestPushSameInstanceGoesBack(t *testing.T) {
	m := newTestModel(t, &fakeState{title: "home"})
	providers := &fakeState{title: "providers"}
	m.Update(State(providers))
	m.Update(State(&fakeState{title: "mangas"}))
	m.Update(State(providers))
	checkTitles(t, m, "home", "providers")
	if req, ok := restoredWith(providers); !ok || req != nil {
		t.Errorf("the same instance came as a request: %v", req)
	}
}

func TestUnkeyedWindowsStack(t *testing.T) {
	m := newTestModel(t, &fakeState{title: "home"})
	m.Update(State(&fakeState{title: "form"}))
	m.Update(State(&fakeState{title: "form"}))
	checkTitles(t, m, "home", "form", "form")
}

func TestDifferentItemsStack(t *testing.T) {
	m := newTestModel(t, keyed("home", "home"))
	m.Update(State(keyed("manga 1", "library/1")))
	m.Update(State(keyed("manga 2", "library/2")))
	checkTitles(t, m, "home", "manga 1", "manga 2")
}

func TestBreadcrumbsFollowTheHistory(t *testing.T) {
	m := newTestModel(t, keyed("Home", "home"))
	m.Update(State(keyed("Watches", "watches")))
	m.Update(State(keyed("One Piece", "watch/1")))
	m.Update(State(keyed("Watches", "watches")))
	if bc := m.viewBreadcrumb(); !strings.Contains(bc, "Home") || strings.Contains(bc, "One Piece") || strings.Contains(bc, "Watches") {
		t.Errorf("breadcrumbs %q, want only Home under Watches", bc)
	}
}

// containsNotification runs cmd (a sequence or batch too) looking for the notification.
func containsNotification(cmd tea.Cmd, message string) bool {
	if cmd == nil {
		return false
	}
	return msgHasNotification(cmd(), message)
}

func msgHasNotification(msg tea.Msg, message string) bool {
	switch msg := msg.(type) {
	case NotificationMsg:
		return msg.Message == message
	case tea.BatchMsg:
		for _, c := range msg {
			if containsNotification(c, message) {
				return true
			}
		}
	default:
		// tea.Sequence's msg is unexported: a slice of cmds
		v := reflectCmds(msg)
		for _, c := range v {
			if containsNotification(c, message) {
				return true
			}
		}
	}
	return false
}

func reflectCmds(msg tea.Msg) []tea.Cmd {
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice {
		return nil
	}
	var cmds []tea.Cmd
	for i := 0; i < v.Len(); i++ {
		if c, ok := v.Index(i).Interface().(tea.Cmd); ok {
			cmds = append(cmds, c)
		}
	}
	return cmds
}
