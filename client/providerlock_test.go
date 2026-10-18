package client

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
)

// withLockStore makes the downloads lock in a temp data db, polling fast. The db's path is
// returned for other handles (another process).
func withLockStore(t *testing.T) (storage.Watches, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), storage.FilenameData)
	store := openLockStore(t, path)
	oldStore, oldPoll := providerLockStore, providerLockPoll
	providerLockStore = func() (storage.Watches, error) { return store, nil }
	providerLockPoll = 10 * time.Millisecond
	t.Cleanup(func() { providerLockStore, providerLockPoll = oldStore, oldPoll })
	return store, path
}

func openLockStore(t *testing.T, path string) storage.Watches {
	t.Helper()
	db, err := storage.OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return storage.Watches{DB: db}
}

// fakeLib is what's downloaded: a chapter downloaded twice is skipped, as libmangal does
// with the file already there.
type fakeLib struct {
	mu   sync.Mutex
	have map[string]bool
	// gets are the chapters actually downloaded, in order
	gets []string
}

func (l *fakeLib) get(gate <-chan struct{}) getFunc {
	return func(ctx context.Context, ch mangadata.Chapter) (*metadata.DownloadedChapter, error) {
		if gate != nil {
			select {
			case <-gate:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		l.mu.Lock()
		defer l.mu.Unlock()
		key := fmt.Sprintf("%s/%g", ch.Volume().Manga().Info().ID, ch.Info().Number)
		if l.have[key] {
			return &metadata.DownloadedChapter{Skipped: "already there"}, nil
		}
		l.have[key] = true
		l.gets = append(l.gets, key)
		return &metadata.DownloadedChapter{}, nil
	}
}

// started downloads in the background, closing started once its first chapter starts.
func started(provider string, chapters []mangadata.Chapter, get getFunc) (<-chan struct{}, <-chan Results) {
	start, done := make(chan struct{}), make(chan Results, 1)
	var once sync.Once
	go func() {
		done <- download(context.Background(), provider, chapters, func(e Event) {
			if e.Kind == EventStart {
				once.Do(func() { close(start) })
			}
		}, lockStored, chapterStored, get)
	}()
	return start, done
}

func TestDownloadSameProviderWaits(t *testing.T) {
	withLockStore(t)
	lib := &fakeLib{have: map[string]bool{}}
	gate := make(chan struct{})
	start, first := started("p", []mangadata.Chapter{chapter{number: 1}, chapter{number: 2}}, lib.get(gate))
	<-start

	// another manga of the provider waits too
	busy := make(chan ProviderBusy, 1)
	second := make(chan Results)
	go func() {
		second <- download(context.Background(), "p", []mangadata.Chapter{chapter{number: 2}, chapter{number: 1, manga: "other"}}, func(e Event) {
			if e.Kind == EventBusy {
				busy <- *e.Busy
			}
		}, lockStored, chapterStored, lib.get(nil))
	}()
	select {
	case b := <-busy:
		if b.Watch || b.Provider != "p" || b.Message() != "p is busy with another download, waiting…" {
			t.Errorf("busy %+v: %s", b, b.Message())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second download didn't wait")
	}
	close(gate)
	r1, r2 := <-first, <-second
	if len(r1.Succeed()) != 2 || len(r2.Succeed()) != 2 {
		t.Fatalf("first %+v, second %+v", r1, r2)
	}
	// the second skips what the first got
	if r2[0].Down.Skipped == "" || r2[1].Down.Skipped != "" {
		t.Errorf("second: %+v, %+v", r2[0].Down, r2[1].Down)
	}
	if !slices.Equal(lib.gets, []string{"m/1", "m/2", "other/1"}) {
		t.Errorf("downloaded %v", lib.gets)
	}
}

func TestDownloadOtherProviderDoesntWait(t *testing.T) {
	withLockStore(t)
	lib := &fakeLib{have: map[string]bool{}}
	gate := make(chan struct{})
	defer close(gate)
	start, _ := started("p", []mangadata.Chapter{chapter{number: 1}}, lib.get(gate))
	<-start

	done := make(chan Results)
	go func() {
		done <- download(context.Background(), "q", []mangadata.Chapter{chapter{number: 1, manga: "b"}}, func(e Event) {
			if e.Kind == EventBusy {
				t.Error("waited for another provider")
			}
		}, lockStored, chapterStored, lib.get(nil))
	}()
	select {
	case r := <-done:
		if len(r.Succeed()) != 1 {
			t.Errorf("results %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked by another provider")
	}
}

func TestDownloadCancelledWhileWaiting(t *testing.T) {
	store, _ := withLockStore(t)
	_, unlock, err := LockProvider(context.Background(), store, "p", LockForDownload, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	ctx, cancel := context.WithCancel(context.Background())
	lib := &fakeLib{have: map[string]bool{}}
	results := download(ctx, "p", []mangadata.Chapter{chapter{number: 1}}, func(e Event) {
		if e.Kind == EventBusy {
			cancel()
		}
	}, lockStored, chapterStored, lib.get(nil))
	if len(results.ToDownload()) != 1 || len(lib.gets) != 0 {
		t.Errorf("results %+v, downloaded %v", results, lib.gets)
	}
}

// TestWatchCheckAndDownload: a watch check in another process (its own db handle) has the
// provider: a manual download waits for it, the check's own downloads don't.
func TestWatchCheckAndDownload(t *testing.T) {
	_, path := withLockStore(t)
	other := openLockStore(t, path)
	check, err := other.LockAs(ProviderLockName("p"), LockForWatch, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx := HoldingProvider(context.Background(), "p")

	lib := &fakeLib{have: map[string]bool{}}
	own := make(chan Results)
	go func() {
		own <- download(ctx, "p", []mangadata.Chapter{chapter{number: 1}}, func(e Event) {
			if e.Kind == EventBusy {
				t.Error("the check waited for its own lock")
			}
		}, lockStored, chapterStored, lib.get(nil))
	}()
	select {
	case r := <-own:
		if len(r.Succeed()) != 1 {
			t.Fatalf("own %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the check's download deadlocked")
	}

	busy := make(chan ProviderBusy, 1)
	manual := make(chan Results)
	go func() {
		manual <- download(context.Background(), "p", []mangadata.Chapter{chapter{number: 1}, chapter{number: 2}}, func(e Event) {
			if e.Kind == EventBusy {
				busy <- *e.Busy
			}
		}, lockStored, chapterStored, lib.get(nil))
	}()
	b := <-busy
	if !b.Watch || !strings.HasPrefix(b.Message(), "p is being checked by a watch (pid ") {
		t.Errorf("busy %+v: %s", b, b.Message())
	}
	if err := check.Unlock(); err != nil {
		t.Fatal(err)
	}
	r := <-manual
	if r[0].Down == nil || r[0].Down.Skipped == "" || r[1].Down == nil || r[1].Down.Skipped != "" {
		t.Errorf("manual %+v", r)
	}
	// released after the download
	if l, err := other.Lock(ProviderLockName("p"), time.Minute); errors.Is(err, storage.ErrLocked) {
		t.Error("still locked")
	} else if err == nil {
		_ = l.Unlock()
	}
}

// TestLostLockStopsTheDownload: another mangal taking the provider's lock over (it went
// stale) stops the download holding it.
func TestLostLockStopsTheDownload(t *testing.T) {
	store, _ := withLockStore(t)
	oldBeat := providerLockBeat
	providerLockBeat = 10 * time.Millisecond
	t.Cleanup(func() { providerLockBeat = oldBeat })
	lib := &fakeLib{have: map[string]bool{}}
	start, done := started("p", []mangadata.Chapter{chapter{number: 1}, chapter{number: 2}}, lib.get(make(chan struct{})))
	<-start
	if _, err := store.DB.Exec(`UPDATE locks SET token = 'other' WHERE name = ?`, ProviderLockName("p")); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if !errors.Is(r[0].Err, storage.ErrLockLost) || len(lib.gets) != 0 {
			t.Errorf("results %+v, gets %v", r, lib.gets)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("still downloading without the lock")
	}
}

// TestWaitProviderFree: it waits while the lock is held, without taking it.
func TestWaitProviderFree(t *testing.T) {
	store, _ := withLockStore(t)
	lock, err := store.LockAs(ProviderLockName("p"), LockForDownload, ProviderLockStale)
	if err != nil {
		t.Fatal(err)
	}
	var told []ProviderBusy
	done := make(chan error, 1)
	go func() {
		done <- WaitProviderFree(context.Background(), store, "p", func(b ProviderBusy) { told = append(told, b) })
	}()
	select {
	case err := <-done:
		t.Fatalf("returned while held: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(told) != 1 || told[0].Provider != "p" {
		t.Errorf("told %+v", told)
	}
	if _, held, _ := store.Holder(ProviderLockName("p"), ProviderLockStale); held {
		t.Error("it took the lock")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.LockAs(ProviderLockName("p"), LockForDownload, ProviderLockStale); err != nil {
		t.Fatal(err)
	}
	if err := WaitProviderFree(ctx, store, "p", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}
