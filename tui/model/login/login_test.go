package login

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

var errGone = errors.New("gone")

type expiredError struct{}

func (expiredError) Error() string { return "expired" }

// fake is a service whose calls are recorded.
type fake struct {
	status  Status
	logins  [][]string
	saved   []string
	clients [][]string
}

func (f *fake) service(withClient bool) Service {
	svc := Service{
		ID:     "fake",
		Name:   "Fake",
		Color:  color.Anilist,
		Check:  func(context.Context) error { return nil },
		Status: func(context.Context) (Status, error) { return f.status, nil },
		Fields: []Input{
			{Label: "User", Required: true},
			{Label: "Password", Secret: true, Required: true, Clear: true},
		},
		LoginNew: func(_ context.Context, values []string) (metadata.User, error) {
			f.logins = append(f.logins, values)
			return nil, metadata.ErrUnauthorized
		},
		Rejected: "wrong",
		LoginSaved: func(_ context.Context, user string) error {
			f.saved = append(f.saved, user)
			return nil
		},
		NotFound: errGone,
		Expired:  ExpiredAs[expiredError],
		Logout:   func() error { return nil },
		Forget:   func(string) error { return nil },
	}
	if withClient {
		svc.Fields = nil
		svc.FormView = func(key string) string { return key + " logs in" }
		svc.Client = &Client{
			Fields: []Input{{Label: "Client ID"}, {Label: "Client secret", Secret: true}},
			Load:   func() ([]string, error) { return []string{"id", "secret"}, nil },
			Save: func(_ context.Context, values []string) (string, error) {
				f.clients = append(f.clients, values)
				return "saved", nil
			},
		}
	}
	return svc
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return util.KeyPress("enter")
	case "esc":
		return util.KeyPress("esc")
	case "tab":
		return util.KeyPress("tab")
	case "shift+tab":
		return util.KeyPress("shift+tab")
	case "ctrl+d":
		return util.KeyPress("ctrl+d")
	}
	return util.KeyPress(k)
}

// run runs cmd, giving the screen its msgs.
func run(s *Screen, cmd tea.Cmd) {
	for _, msg := range base.Drain(cmd) {
		switch msg.(type) {
		case statusMsg, clientMsg:
			run(s, s.Update(context.Background(), msg))
		}
	}
}

func press(s *Screen, keys ...string) {
	for _, k := range keys {
		run(s, s.Update(context.Background(), keyMsg(k)))
	}
}

// TestKeys: every phase's keys are base ones, none collides with a global or the list's
// (but esc: it's the screen's while it isn't Backable).
func TestKeys(t *testing.T) {
	f := &fake{}
	for _, withClient := range []bool{false, true} {
		s := New(f.service(withClient))
		for name, st := range map[string]Status{
			"saved":     {Users: []string{"nick"}},
			"logged in": {User: user("nick")},
			"form":      {},
			"client":    {NoClient: true},
		} {
			s.Update(context.Background(), st)
			km := s.KeyMap()
			var extra []key.Binding
			if s.phase == saved {
				km, extra = &s.keys, s.list.KeyMap.Browsing()
			}
			screen := slices.DeleteFunc(util.Bindings(km), func(b key.Binding) bool { return slices.Equal(b.Keys(), []string{"esc"}) })
			if keys := base.KeyCollisions(util.KeyMap{Short: screen}, extra...); len(keys) != 0 {
				t.Errorf("%s (client %v): %v", name, withClient, keys)
			}
		}
	}
}

// / on the saved logins doesn't start a filter: its typing would be the screen's keys too
// (MT-A6)
func TestSavedLoginsDontFilter(t *testing.T) {
	for _, withClient := range []bool{false, true} {
		s := New((&fake{}).service(withClient))
		s.Resize(base.Size{Width: 80, Height: 10})
		s.Update(context.Background(), Status{Users: []string{"nick", "bob"}})
		if s.phase != saved {
			t.Fatalf("phase %v, want the saved logins", s.phase)
		}
		press(s, "/")
		if s.list.Filtering() || s.Inputting() {
			t.Error("/ started a filter")
		}
	}
}

// TestForm: the fields take the typing (i, n are no keys there), tab and shift+tab move,
// enter validates and logs in, esc goes back to the saved logins.
func TestForm(t *testing.T) {
	f := &fake{status: Status{Users: []string{"nick"}}}
	s := New(f.service(false))
	s.Update(context.Background(), f.status)
	press(s, "n")
	if s.phase != newLogin || !s.Inputting() || s.Backable() {
		t.Fatalf("n: phase %v, inputting %v, backable %v", s.phase, s.Inputting(), s.Backable())
	}
	press(s, "i", "n", "enter")
	if len(f.logins) != 0 || !strings.Contains(s.View(), "required") || s.form.Current() != 1 {
		t.Fatalf("an empty password logged in: %v\n%s", f.logins, s.View())
	}
	press(s, "shift+tab", "o", "tab", "pw")
	if got := s.form.Fields[0].Raw(); got != "ino" {
		t.Errorf("user %q, want the typed ino", got)
	}
	press(s, "enter")
	if !slices.EqualFunc(f.logins, [][]string{{"ino", "pw"}}, slices.Equal) {
		t.Fatalf("logins %q", f.logins)
	}
	// rejected: the form stays, without the password
	if s.phase != newLogin || s.form.Fields[1].Raw() != "" || s.form.Fields[0].Raw() != "ino" {
		t.Errorf("after the rejected login: phase %v, fields %q %q", s.phase, s.form.Fields[0].Raw(), s.form.Fields[1].Raw())
	}
	press(s, "esc")
	if s.phase != saved || !s.Backable() {
		t.Errorf("esc: phase %v, backable %v", s.phase, s.Backable())
	}
	press(s, "i")
	if !slices.Equal(f.saved, []string{"nick"}) {
		t.Errorf("saved logins %q", f.saved)
	}

	// without saved logins the form is the screen, esc leaves it
	s = New(f.service(false))
	s.Update(context.Background(), Status{})
	if s.phase != newLogin || !s.Backable() {
		t.Errorf("no logins: phase %v, backable %v", s.phase, s.Backable())
	}
}

// TestValues: the saved API client fills the empty form, never over what was typed.
func TestValues(t *testing.T) {
	s := New((&fake{}).service(false))
	s.Update(context.Background(), Status{Values: []string{"id", "secret"}})
	if s.form.Fields[0].Raw() != "id" || s.form.Fields[1].Raw() != "secret" {
		t.Fatal("not filled")
	}
	press(s, "x")
	s.Update(context.Background(), Status{Values: []string{"other", ""}})
	if s.form.Fields[0].Raw() != "idx" {
		t.Errorf("typed value replaced: %q", s.form.Fields[0].Raw())
	}
}

// TestClient: without a client its form opens (esc leaves the screen), e edits the saved
// one (esc cancels), enter saves.
func TestClient(t *testing.T) {
	f := &fake{status: Status{NoClient: true}}
	s := New(f.service(true))
	s.Update(context.Background(), f.status)
	if s.phase != client || !s.Inputting() || !s.Backable() {
		t.Fatalf("no client: phase %v, inputting %v, backable %v", s.phase, s.Inputting(), s.Backable())
	}
	// e and i are typed
	press(s, "my-id-ei", "tab", "my-secret")
	if view := s.View(); !strings.Contains(view, "my-id-ei") || strings.Contains(view, "my-secret") {
		t.Errorf("view:\n%s", view)
	}
	f.status = Status{}
	press(s, "enter")
	if !slices.EqualFunc(f.clients, [][]string{{"my-id-ei", "my-secret"}}, slices.Equal) {
		t.Fatalf("clients %q", f.clients)
	}
	if s.phase != newLogin || s.Inputting() || !strings.Contains(s.View(), "i logs in") {
		t.Fatalf("after saving: phase %v\n%s", s.phase, s.View())
	}

	press(s, "e")
	if s.phase != client || s.client.Fields[0].Raw() != "id" || s.Backable() {
		t.Fatalf("e: phase %v, id %q", s.phase, s.client.Fields[0].Raw())
	}
	press(s, "esc")
	if s.phase != newLogin {
		t.Errorf("esc: phase %v, want the login", s.phase)
	}
}

func TestExpired(t *testing.T) {
	f := &fake{status: Status{Users: []string{"nick"}}}
	svc := f.service(false)
	svc.LoginSaved = func(context.Context, string) error { return expiredError{} }
	s := New(svc)
	s.Update(context.Background(), f.status)
	press(s, "i")
	if s.phase != newLogin {
		t.Errorf("expired: phase %v, want the form", s.phase)
	}
}

type user string

func (u user) Name() string              { return string(u) }
func (u user) String() string            { return string(u) }
func (u user) ID() int                   { return 1 }
func (u user) About() string             { return "" }
func (u user) Avatar() string            { return "" }
func (u user) URL() string               { return "" }
func (u user) Source() metadata.IDSource { return metadata.IDSourceAnilist }
