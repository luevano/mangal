package accounts

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
)

// run sends the messages of cmd back to the screen, the notifications are returned.
func run(s *state, cmd tea.Cmd) []string {
	var notes []string
	note := func(msg tea.Msg) {
		switch m := msg.(type) {
		case base.NotificationMsg:
			notes = append(notes, m.Message)
		case base.NotificationWithDurationMsg:
			notes = append(notes, m.Message)
		}
	}
	for _, msg := range base.Drain(cmd) {
		note(msg)
		for _, more := range base.Drain(s.Update(context.Background(), msg)) {
			note(more)
			s.Update(context.Background(), more)
		}
	}
	return notes
}

// TestUsersRows: the users and tokens are rows after the accounts: n adds (a user, a
// token on the tokens' rows), e the permissions, d removes on the second press.
func TestUsersRows(t *testing.T) {
	data, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	s := loaded()
	s.usersSvc = core.NewUsersOf(storage.Users{DB: data})
	run(s, s.loadUsers(context.Background()))
	if v := s.View(); !strings.Contains(v, "mangal users") || !strings.Contains(v, "API tokens") {
		t.Fatalf("no placeholders:\n%s", v)
	}

	// down to the users' placeholder: n opens the user form
	for range len(s.entries) {
		press(s, "down")
	}
	press(s, "n")
	if s.formKind != formAddUser || !s.Inputting() {
		t.Fatalf("form %d", s.formKind)
	}
	press(s, "david")
	press(s, "tab")
	press(s, "correct horse")
	notes := run(s, press(s, "enter"))
	if len(s.users) != 1 || !s.users[0].Admin || !strings.Contains(strings.Join(notes, "|"), "Added the user david") {
		t.Fatalf("users %+v, notes %q", s.users, notes)
	}

	// e changes the permissions: the last admin keeps them
	press(s, "e")
	if s.formKind != formPerms {
		t.Fatalf("form %d", s.formKind)
	}
	if s.form.Fields[0].Choice() != "admin" {
		t.Fatalf("role %q", s.form.Fields[0].Choice())
	}
	// → picks the next role: custom after admin, which needs its permissions
	press(s, "right")
	if s.form.Fields[0].Choice() != custom {
		t.Fatalf("after →: %q", s.form.Fields[0].Choice())
	}
	if s.form.Valid() {
		t.Error("custom without permissions is valid")
	}
	s.form.Fields[1].SetValue("read")
	run(s, press(s, "enter"))
	if !s.users[0].Admin {
		t.Error("the last admin was demoted")
	}

	// the tokens' placeholder: n makes a token, its secret shown once
	press(s, "down")
	press(s, "n")
	if s.formKind != formAddToken {
		t.Fatalf("form %d", s.formKind)
	}
	press(s, "claude")
	notes = run(s, press(s, "enter"))
	if len(s.tokens) != 1 || !strings.Contains(strings.Join(notes, "|"), "API token claude: ") {
		t.Fatalf("tokens %+v, notes %q", s.tokens, notes)
	}

	// d asks, a second d removes
	notes = run(s, press(s, "d"))
	if len(s.tokens) != 1 || !strings.Contains(strings.Join(notes, "|"), "again") {
		t.Fatalf("first d: %+v %q", s.tokens, notes)
	}
	run(s, press(s, "d"))
	if len(s.tokens) != 0 {
		t.Errorf("not removed: %+v", s.tokens)
	}
}
