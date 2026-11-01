package accounts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/tui/base"
)

// TestRemoteTrackerLogin: i on a tracker starts its login on the server, the address is
// shown and what's pasted finishes it; o logs out; the rest is the server machine's.
func TestRemoteTrackerLogin(t *testing.T) {
	var logins []map[string]string
	var logout string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/me/accounts/anilist/login":
			var b map[string]string
			_ = json.NewDecoder(r.Body).Decode(&b)
			logins = append(logins, b)
			_, _ = w.Write([]byte(`{"url":"https://anilist.example/authorize"}`))
		case "/api/v1/me/accounts/anilist/logout":
			logout = "anilist"
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	rc, err := remote.New(remote.Options{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	base.CurrentRemote = &base.Remote{URL: srv.URL, Client: rc, Perms: []string{core.ScopeRead, core.ScopeProgress}}
	t.Cleanup(func() { base.CurrentRemote = nil })
	t.Setenv("PATH", t.TempDir()) // no browser opens

	ctx := context.Background()
	s := New(false).(*state)
	// nothing to list first: the cursor still lands on the first row after
	s.Update(ctx, usersMsg{})
	s.Update(ctx, loadedMsg{
		{Service: "AniList", User: "nick", Metadata: metadata.IDSourceAnilist},
		{Service: "Kitsu", Metadata: metadata.IDSourceKitsu},
		{Service: "MangaPlus"},
	})
	settle := func(cmd tea.Cmd) {
		for _, m := range base.Drain(cmd) {
			s.Update(ctx, m)
		}
	}
	settle(press(s, "i"))
	if !s.Inputting() || !strings.Contains(s.View(), "anilist.example/authorize") {
		t.Fatalf("no paste form:\n%s", s.View())
	}
	press(s, "the code")
	settle(press(s, "enter"))
	if len(logins) != 2 || logins[0]["paste"] != "" || logins[1]["paste"] != "the code" {
		t.Errorf("logins %v", logins)
	}
	if s.Inputting() {
		t.Error("the form stays")
	}
	settle(press(s, "o"))
	if logout != "anilist" {
		t.Error("o doesn't log out")
	}

	press(s, "down")
	press(s, "i")
	if !s.Inputting() || s.formKind != formTrackerPassword {
		t.Errorf("kitsu asks for a password: %v %v", s.Inputting(), s.formKind)
	}
	press(s, "esc")

	press(s, "down")
	if msg, ok := press(s, "i")().(base.NotificationMsg); !ok || !strings.Contains(msg.Message, "on the server's machine") {
		t.Errorf("mangaplus: %#v", msg)
	}
}
