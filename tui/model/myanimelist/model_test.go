package myanimelist

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/util/cache"
)

func TestMain(m *testing.M) {
	// keep the logins away from the user's databases
	dir, err := os.MkdirTemp("", "mangal-tui-mal-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	os.Setenv("MANGAL_DATA_PATH", filepath.Join(dir, "data"))
	os.Setenv("MANGAL_AUTH_KEYRING", "false")
	code := m.Run()
	_ = cache.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// run runs cmd and the cmds of the sequences and batches it gives, returning the other msgs.
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, c := range batch {
			msgs = append(msgs, run(c)...)
		}
		return msgs
	}
	// tea.Sequence's msg is an unexported []tea.Cmd
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice {
		var msgs []tea.Msg
		for i := range v.Len() {
			if c, ok := v.Index(i).Interface().(tea.Cmd); ok {
				msgs = append(msgs, run(c)...)
			}
		}
		return msgs
	}
	return []tea.Msg{msg}
}

func typeText(ctx context.Context, s *state, text string) {
	s.Update(ctx, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func TestSetupSavesClient(t *testing.T) {
	ctx := context.Background()
	s := New().(*state)
	msg, err := status(ctx)
	if err != nil || !msg.notConfigured {
		t.Fatalf("status without a client: %+v, %v", msg, err)
	}
	s.Update(ctx, msg)
	if s.phase != setup || !s.Inputting() || !s.Backable() {
		t.Fatalf("phase %v, inputting %v, backable %v: want the setup, esc going back", s.phase, s.Inputting(), s.Backable())
	}

	// "e" and "i" are typed, not the client and login keys
	typeText(ctx, s, "my-id-ei")
	s.Update(ctx, tea.KeyMsg{Type: tea.KeyTab})
	typeText(ctx, s, "my-secret")
	if s.inputs[idField].Value() != "my-id-ei" || s.inputs[secretField].Value() != "my-secret" {
		t.Fatalf("typed %q %q", s.inputs[idField].Value(), s.inputs[secretField].Value())
	}
	if view := s.View(); !strings.Contains(view, "*********") || strings.Contains(view, "my-secret") {
		t.Errorf("secret not masked:\n%s", view)
	}

	for _, m := range run(s.Update(ctx, tea.KeyMsg{Type: tea.KeyEnter})) {
		s.Update(ctx, m)
	}
	if id, secret, err := cache.MyAnimeListClient(); err != nil || id != "my-id-ei" || secret != "my-secret" {
		t.Fatalf("saved %q %q, %v", id, secret, err)
	}
	// the load wraps the status, read it again as the screen would get it
	msg, err = status(ctx)
	if err != nil || msg.notConfigured || msg.id != "my-id-ei" {
		t.Fatalf("status with the client: %+v, %v", msg, err)
	}
	s.Update(ctx, msg)
	if s.phase != form || s.Inputting() || !myanimelist.Configured() {
		t.Fatalf("phase %v after saving, want the login", s.phase)
	}

	// e edits the saved client, esc cancels
	for _, m := range run(s.Update(ctx, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})) {
		s.Update(ctx, m)
	}
	if s.phase != setup || s.inputs[idField].Value() != "my-id-ei" || s.inputs[secretField].Value() != "my-secret" || s.Backable() {
		t.Fatalf("edit: phase %v, fields %q %q", s.phase, s.inputs[idField].Value(), s.inputs[secretField].Value())
	}
	s.Update(ctx, tea.KeyMsg{Type: tea.KeyEsc})
	if s.phase != form {
		t.Fatalf("phase %v after cancel, want the login", s.phase)
	}

	// an empty id removes it
	for _, m := range run(s.Update(ctx, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})) {
		s.Update(ctx, m)
	}
	s.inputs[idField].SetValue("")
	run(s.Update(ctx, tea.KeyMsg{Type: tea.KeyEnter}))
	if id, secret, err := cache.MyAnimeListClient(); err != nil || id != "" || secret != "" {
		t.Errorf("after removing: %q %q, %v", id, secret, err)
	}
}
