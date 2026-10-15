package accounts

import (
	"github.com/luevano/mangal/v5/tui/util"

	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/util/cache"
)

func press(s *state, k string) tea.Cmd {
	switch k {
	case "enter":
		return s.Update(context.Background(), util.KeyPress("enter"))
	case "esc":
		return s.Update(context.Background(), util.KeyPress("esc"))
	case "down":
		return s.Update(context.Background(), util.KeyPress("down"))
	}
	return s.Update(context.Background(), util.KeyPress(k))
}

func loaded() *state {
	s := New(false).(*state)
	s.Update(context.Background(), loadedMsg{
		{Account: libmangal.Account{Service: "Anilist", User: "nick", Status: "logged in", URL: "https://anilist.co/user/nick"}, Metadata: metadata.IDSourceAnilist, Checked: time.Now()},
		{Account: libmangal.Account{Service: "MangaPlus", Status: "free device", Details: []string{"no secret"}}, Provider: mangaPlusID, Checked: time.Now().Add(-2 * time.Hour)},
		{Account: libmangal.Account{Service: "Discord", Status: "not set"}, Discord: true, Checked: time.Now()},
		{Account: libmangal.Account{Service: "Other"}, Provider: "mango-other", Err: errTest, Checked: time.Now()},
	})
	return s
}

type testError struct{}

func (testError) Error() string { return "site down" }

var errTest = testError{}

func TestRows(t *testing.T) {
	s := loaded()
	view := s.View()
	for _, want := range []string{"Anilist", "nick", "logged in", "free device", "no secret", "2h ago", "site down"} {
		if !strings.Contains(view, want) {
			t.Errorf("no %q:\n%s", want, view)
		}
	}
}

// TestLogIn: i opens the service's login screen, or asks for the secret.
func TestLogIn(t *testing.T) {
	s := loaded()
	cmd := press(s, "i")
	if st, ok := cmd().(base.Keyed); !ok || st.Key() != "login/anilist" {
		t.Errorf("anilist: %#v", cmd())
	}
	if cmd := press(s, "c"); cmd == nil {
		t.Error("c doesn't copy the url")
	}

	press(s, "down")
	press(s, "i")
	if !s.Inputting() || s.Backable() || !strings.Contains(s.View(), "MangaPlus secret") {
		t.Fatalf("mangaplus: inputting %v\n%s", s.Inputting(), s.View())
	}
	// i and c are typed
	press(s, "ic")
	if s.secret.Value() != "ic" {
		t.Errorf("typed %q", s.secret.Value())
	}
	press(s, "esc")
	if s.Inputting() || !s.Backable() {
		t.Error("esc doesn't cancel")
	}
	if cmd := press(s, "c"); cmd != nil {
		t.Error("c copies a row without url")
	}

	press(s, "down")
	press(s, "i")
	press(s, "not a url")
	if msg, ok := press(s, "enter")().(base.NotificationMsg); !ok || msg.Message == "" {
		t.Errorf("invalid webhook: %#v", msg)
	}
	press(s, "i")
	const url = "https://discord.com/api/webhooks/1/abc"
	press(s, url)
	for _, msg := range base.Drain(press(s, "enter")) {
		s.Update(context.Background(), msg)
	}
	if saved, err := cache.DiscordWebhook(); err != nil || saved != url {
		t.Errorf("saved %q, %v", saved, err)
	}
	if !strings.Contains(s.View(), "webhook set") {
		t.Errorf("not reloaded:\n%s", s.View())
	}

	press(s, "down")
	if msg, ok := press(s, "i")().(base.NotificationMsg); !ok || !strings.Contains(msg.Message, "Nothing to log into") {
		t.Errorf("other: %#v", msg)
	}
}
