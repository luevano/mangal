package myanimelist

import (
	"github.com/luevano/mangal/v5/tui/util"

	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/util/cache"
)

// press sends the keys, running what they start.
func press(s base.State, keys ...tea.KeyPressMsg) {
	ctx := context.Background()
	for _, k := range keys {
		msgs := base.Drain(s.Update(ctx, k))
		for len(msgs) != 0 {
			var next []tea.Msg
			for _, m := range msgs {
				next = append(next, base.Drain(s.Update(ctx, m))...)
			}
			msgs = next
		}
	}
}

func text(s string) tea.KeyPressMsg { return util.KeyPress(s) }

func TestSetupSavesClient(t *testing.T) {
	ctx := context.Background()
	s := New()
	st, err := status(ctx)
	if err != nil || !st.NoClient {
		t.Fatalf("status without a client: %+v, %v", st, err)
	}
	press(s)
	s.Update(ctx, st)
	if !s.Inputting() || !s.Backable() {
		t.Fatalf("inputting %v, backable %v: want the client form, esc going back", s.Inputting(), s.Backable())
	}

	// "e" and "i" are typed, not the client and login keys
	press(s, text("my-id-ei"), util.KeyPress("tab"), text("my-secret"))
	if view := s.View(); !strings.Contains(view, "my-id-ei") || !strings.Contains(view, "*********") || strings.Contains(view, "my-secret") {
		t.Errorf("view:\n%s", view)
	}
	press(s, util.KeyPress("enter"))
	if id, secret, err := cache.MyAnimeListClient(); err != nil || id != "my-id-ei" || secret != "my-secret" {
		t.Fatalf("saved %q %q, %v", id, secret, err)
	}
	if s.Inputting() || !myanimelist.Configured() || !strings.Contains(s.View(), "opens the MyAnimeList login") {
		t.Fatalf("after saving, want the login:\n%s", s.View())
	}

	// e edits the saved client, esc cancels
	press(s, text("e"))
	if !s.Inputting() || s.Backable() || !strings.Contains(s.View(), "my-id-ei") {
		t.Fatalf("edit:\n%s", s.View())
	}
	press(s, util.KeyPress("esc"))
	if s.Inputting() {
		t.Fatal("esc doesn't cancel the edit")
	}

	// an empty id removes it
	press(s, text("e"))
	for range len("my-id-ei") {
		press(s, util.KeyPress("backspace"))
	}
	press(s, util.KeyPress("enter"))
	if id, secret, err := cache.MyAnimeListClient(); err != nil || id != "" || secret != "" {
		t.Errorf("after removing: %q %q, %v", id, secret, err)
	}
}

// without a saved client the ID is required: enter on an empty form doesn't "remove" a
// client (and reset the login)
func TestSetupRequiresID(t *testing.T) {
	ctx := context.Background()
	if err := myanimelist.SetClient("", ""); err != nil {
		t.Fatal(err)
	}
	s := New()
	st, err := status(ctx)
	if err != nil || !st.NoClient {
		t.Fatalf("status without a client: %+v, %v", st, err)
	}
	s.Update(ctx, st)
	var msgs []tea.Msg
	for _, m := range base.Drain(s.Update(ctx, util.KeyPress("enter"))) {
		msgs = append(msgs, m)
		msgs = append(msgs, base.Drain(s.Update(ctx, m))...)
	}
	for _, m := range msgs {
		if n, ok := m.(base.NotificationMsg); ok && strings.Contains(n.Message, "Removed") {
			t.Errorf("notified %q", n.Message)
		}
	}
	if !s.Inputting() || !strings.Contains(s.View(), "required") {
		t.Errorf("want the form with the ID required:\n%s", s.View())
	}
}
