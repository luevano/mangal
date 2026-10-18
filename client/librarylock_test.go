package client

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
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

// a library operation waits for the chapter being written, not for whole provider runs,
// and a download pauses before its next chapter while the operation runs.
func TestLibraryOpsAndDownloads(t *testing.T) {
	store, _ := withLockStore(t)
	old := libraryLockPoll
	libraryLockPoll = 10 * time.Millisecond
	t.Cleanup(func() { libraryLockPoll = old })
	ops := Ops{Storage: storage.Storage{Data: store.DB}}
	lib := &fakeLib{have: map[string]bool{}}

	// a provider's lock without a chapter being written doesn't block it
	_, unlockProvider, err := LockProvider(context.Background(), store, "q", LockForWatch, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, unlock, err := ops.Lock(context.Background(), "rename", false, nil); err != nil {
		t.Fatalf("blocked by a provider's lock: %v", err)
	} else {
		unlock()
	}
	unlockProvider()

	// chapter 1 being written: busy, or the operation waits for it
	gate := make(chan struct{})
	busy := make(chan ProviderBusy, 1)
	results := make(chan Results)
	started1 := make(chan struct{})
	started2 := make(chan struct{})
	go func() {
		results <- download(context.Background(), "p", []mangadata.Chapter{chapter{number: 1}, chapter{number: 2}}, func(e Event) {
			switch {
			case e.Kind == EventBusy:
				busy <- *e.Busy
			case e.Kind == EventStart && e.Index == 0:
				close(started1)
			case e.Kind == EventStart && e.Index == 1:
				close(started2)
			}
		}, lockStored, chapterStored, func(ctx context.Context, ch mangadata.Chapter) (*metadata.DownloadedChapter, error) {
			if ch.Info().Number == 1 {
				<-gate
			}
			return lib.get(nil)(ctx, ch)
		})
	}()
	<-started1
	if _, _, err := ops.Lock(context.Background(), "rename", false, nil); !errors.Is(err, ErrLibraryBusy) {
		t.Fatalf("not busy: %v", err)
	}
	locked := make(chan func())
	go func() {
		_, unlock, err := ops.Lock(context.Background(), "rename", true, nil)
		if err != nil {
			t.Error(err)
		}
		locked <- unlock
	}()
	select {
	case <-locked:
		t.Fatal("locked while a chapter is written")
	case <-time.After(100 * time.Millisecond):
	}
	// chapter 1 written: the operation goes, chapter 2 waits for it
	close(gate)
	unlock := <-locked
	b := <-busy
	if !b.Library || b.Holder.Purpose != "rename" || b.Message() == "" {
		t.Errorf("busy %+v", b)
	}
	select {
	case <-started2:
		t.Fatal("chapter 2 started during the operation")
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	if r := <-results; len(r.Succeed()) != 2 {
		t.Errorf("results %+v", r)
	}
}
