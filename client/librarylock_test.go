package client

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
)

func TestLockLibrary(t *testing.T) {
	old := libraryLockPoll
	libraryLockPoll = 10 * time.Millisecond
	t.Cleanup(func() { libraryLockPoll = old })
	store := openLockStore(t, filepath.Join(t.TempDir(), storage.FilenameData))

	ctx, unlock, err := LockLibrary(context.Background(), store, "merge", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	// the holder's ctx takes it again
	if _, again, err := LockLibrary(ctx, store, "mark", false, nil); err != nil {
		t.Fatalf("reentrant: %v", err)
	} else {
		again()
	}
	// someone else: busy, or waits
	_, _, err = LockLibrary(context.Background(), store, "rename", false, nil)
	var busy *LibraryBusyError
	if !errors.As(err, &busy) || !errors.Is(err, ErrLibraryBusy) || busy.Holder.Purpose != "merge" {
		t.Fatalf("not busy: %v", err)
	}
	waited := make(chan LibraryBusy, 1)
	got := make(chan error, 1)
	go func() {
		_, unlock, err := LockLibrary(context.Background(), store, "rename", true, func(b LibraryBusy) { waited <- b })
		if err == nil {
			unlock()
		}
		got <- err
	}()
	b := <-waited
	if b.Holder.Purpose != "merge" || b.Message() != "the library is busy with another change (merge), waiting…" {
		t.Errorf("busy %+v %q", b.Holder, b.Message())
	}
	unlock()
	if err := <-got; err != nil {
		t.Fatal(err)
	}
	// a cancelled wait stops
	_, unlock, _ = LockLibrary(context.Background(), store, "merge", false, nil)
	defer unlock()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := LockLibrary(cancelled, store, "x", true, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled wait: %v", err)
	}
}

// an operation holds the library's lock while it runs
func TestOpsHoldTheLock(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.OpenData(filepath.Join(dir, storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	o := Ops{Storage: storage.Storage{Data: db}, BackupsDir: filepath.Join(dir, "backups"), Keep: 3}
	info, err := o.Run(context.Background(), "test", "a test", func(library.Journal) error {
		h, held, err := o.Storage.Watches().Holder(LibraryLockName, LibraryLockStale)
		if err != nil || !held || h.Purpose != "test" {
			t.Errorf("lock not held: %+v %t %v", h, held, err)
		}
		return nil
	})
	if err != nil || info.ID == 0 || info.Snapshot == "" {
		t.Fatalf("%+v %v", info, err)
	}
	if _, held, _ := o.Storage.Watches().Holder(LibraryLockName, LibraryLockStale); held {
		t.Error("still held")
	}
}
