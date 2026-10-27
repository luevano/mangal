package storage

import (
	"path/filepath"
	"testing"
	"time"
)

// TestRevisions: a change moves its topic's counter, a login's last_login doesn't.
func TestRevisions(t *testing.T) {
	data, err := OpenData(filepath.Join(t.TempDir(), FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	revs := func() map[string]int64 {
		r, err := data.Revisions()
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	before := revs()
	users := Users{DB: data}
	if err := users.Add(User{Name: "kid", Hash: "h", Perms: []string{"read"}}); err != nil {
		t.Fatal(err)
	}
	if err := (History{DB: data, User: "kid"}).Record(Read{Provider: "p", MangaID: "m", Chapter: 1, Last: time.Now()}); err != nil {
		t.Fatal(err)
	}
	after := revs()
	if after[RevAuth] == before[RevAuth] || after[RevHistory] == before[RevHistory] || after[RevLibrary] != before[RevLibrary] {
		t.Errorf("before %v, after %v", before, after)
	}
	if err := users.LoggedIn("kid", time.Now()); err != nil {
		t.Fatal(err)
	}
	if again := revs(); again[RevAuth] != after[RevAuth] {
		t.Errorf("a login moved auth: %v", again)
	}
}
