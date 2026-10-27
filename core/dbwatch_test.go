package core

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/storage"
)

// TestDBWatcher: another connection's change (another process) is seen on the next tick,
// this one's on a Poke.
func TestDBWatcher(t *testing.T) {
	path := filepath.Join(t.TempDir(), storage.FilenameData)
	mine, err := storage.OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	defer mine.Close()
	other, err := storage.OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	got := make(chan []string, 4)
	w := NewDBWatcher(mine)
	go w.Run(t.Context(), func(topics []string) { got <- topics })
	time.Sleep(50 * time.Millisecond)
	wait := func(want string) {
		t.Helper()
		select {
		case topics := <-got:
			if !slices.Contains(topics, want) {
				t.Errorf("topics %v, want %s", topics, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no %s change seen", want)
		}
	}

	if err := (storage.Users{DB: other}).Add(storage.User{Name: "kid", Hash: "h"}); err != nil {
		t.Fatal(err)
	}
	wait(storage.RevAuth)

	if err := (storage.History{DB: mine, User: "kid"}).Record(storage.Read{Provider: "p", MangaID: "m", Chapter: 1, Last: time.Now()}); err != nil {
		t.Fatal(err)
	}
	w.Poke()
	wait(storage.RevHistory)
}
