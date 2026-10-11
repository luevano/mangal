package anilist

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/luevano/mangal/v5/util/cache"
	"golang.org/x/oauth2"
)

func TestMain(m *testing.M) {
	// keep the logins away from the user's databases
	dir, err := os.MkdirTemp("", "mangal-anilist-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	os.Setenv("MANGAL_AUTH_KEYRING", "false")
	xdg.DataHome = filepath.Join(dir, "data")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestLogoutSticks(t *testing.T) {
	token := &oauth2.Token{AccessToken: "x", Expiry: time.Now().Add(time.Hour)}
	if err := cache.SetAnilistAuthData("me", token); err != nil {
		t.Fatal(err)
	}
	if err := updateUsers(func(h *cache.UserHistory) { h.Add("me") }); err != nil {
		t.Fatal(err)
	}
	if _, found, err := SavedLogin(); err != nil || !found {
		t.Fatalf("saved login before the logout: %v, %v", found, err)
	}

	if err := Logout(); err != nil {
		t.Fatal(err)
	}
	// what the next run would log in with
	if _, found, err := SavedLogin(); err != nil || found {
		t.Fatalf("saved login after the logout: %v, %v", found, err)
	}
	if users, _ := Users(); len(users) != 1 {
		t.Errorf("the user is still saved: %v", users)
	}
	if notice := LoginNotice(); notice != "" {
		t.Errorf("notice after the logout: %q", notice)
	}

	// a login (here as LoginSaved/LoginNew do after logging in) undoes it
	if err := cache.SetAnilistLoggedOut(false); err != nil {
		t.Fatal(err)
	}
	if _, found, err := SavedLogin(); err != nil || !found {
		t.Fatalf("saved login after logging in again: %v, %v", found, err)
	}
}
