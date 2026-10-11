package storage

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/luevano/libmangal"
)

func TestMigrateWatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), FilenameData)
	// a data database from mangal 5.17 (schema 4)
	old, err := Open(path, dataMigrations[:4])
	if err != nil {
		t.Fatal(err)
	}
	if err := (History{DB: old}).Record(Read{Provider: "p", MangaID: "m", Chapter: 1, Last: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	old.Close()

	data, err := OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	if reads, _ := (History{DB: data}).List("", ""); len(reads) != 1 {
		t.Errorf("reads lost: %+v", reads)
	}
	if watches, err := (Watches{DB: data}).List(); err != nil || len(watches) != 0 {
		t.Errorf("watches %+v, %v", watches, err)
	}
}

func f32(f float32) *float32 { return &f }

func TestWatches(t *testing.T) {
	data := openData(t)
	watches := Watches{DB: data}

	w, err := watches.Add(Watch{Provider: "mango-mangaplus", MangaID: "100171", Title: "Dandadan", AnilistID: 132029, Enabled: true, From: f32(200)})
	if err != nil || w.ID == 0 {
		t.Fatalf("add: %+v, %v", w, err)
	}
	if _, err := watches.Add(Watch{Provider: "mango-mangaplus", MangaID: "100171"}); !errors.Is(err, ErrWatchExists) {
		t.Errorf("added twice: %v", err)
	}
	got, err := watches.Get(w.ID)
	if err != nil || got.Title != "Dandadan" || got.AnilistID != 132029 || !got.Enabled || got.From == nil || *got.From != 200 || got.Interval != 0 || got.LastNew != nil {
		t.Fatalf("get: %+v, %v", got, err)
	}

	got.Interval = Duration(2 * time.Hour)
	got.LastChecked, got.LastNew, got.LastNewAt = time.Unix(100, 0), f32(201.5), time.Unix(100, 0)
	got.Failures, got.LastError, got.Enabled = 2, "boom", false
	if err := watches.Save(got); err != nil {
		t.Fatal(err)
	}
	got, _ = watches.Get(w.ID)
	if time.Duration(got.Interval) != 2*time.Hour || *got.LastNew != 201.5 || got.LastChecked.Unix() != 100 || got.Failures != 2 || got.Enabled {
		t.Errorf("saved: %+v", got)
	}
	if _, found, _ := watches.Find("mango-mangaplus", "100171"); !found {
		t.Error("not found by manga")
	}

	// runs: the history stays after the watch goes
	run, err := watches.StartRun(CheckRun{Kind: "manual", Started: time.Unix(200, 0)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = watches.RecordWatchRun(WatchRun{CheckRun: run.ID, Watch: w.ID, Provider: w.Provider, MangaID: w.MangaID, Title: w.Title,
		From: f32(200), Status: StatusNew, Started: time.Unix(200, 0), Finished: time.Unix(210, 0),
		Chapters: []RunChapter{{Number: 202, Title: "b", Path: "/x/202.cbz"}, {Number: 201.5, Title: "a", Path: "/x/201.5.cbz"}}})
	if err != nil {
		t.Fatal(err)
	}
	run.Finished, run.Watches, run.NewChapters = time.Unix(220, 0), 1, 2
	if err := watches.FinishRun(run); err != nil {
		t.Fatal(err)
	}
	other, _ := watches.StartRun(CheckRun{Kind: "scheduled", Started: time.Unix(300, 0)})

	runs, err := watches.Runs(RunFilter{})
	if err != nil || len(runs) != 2 || runs[0].ID != other.ID {
		t.Fatalf("runs %+v, %v", runs, err)
	}
	if r := runs[1]; r.NewChapters != 2 || len(r.Results) != 1 || len(r.Results[0].Chapters) != 2 || r.Results[0].Chapters[0].Number != 201.5 {
		t.Errorf("run %+v", r)
	}
	if runs, _ := watches.Runs(RunFilter{Watch: w.ID}); len(runs) != 1 {
		t.Errorf("runs of the watch: %+v", runs)
	}
	if runs, _ := watches.Runs(RunFilter{Since: time.Unix(250, 0)}); len(runs) != 1 || runs[0].ID != other.ID {
		t.Errorf("runs since: %+v", runs)
	}
	if runs, _ := watches.Runs(RunFilter{Limit: 1}); len(runs) != 1 {
		t.Errorf("limited: %+v", runs)
	}

	if err := watches.Remove(w.ID); err != nil {
		t.Fatal(err)
	}
	if err := watches.Remove(w.ID); !errors.Is(err, ErrWatchNotFound) {
		t.Errorf("removed twice: %v", err)
	}
	if _, err := watches.Get(w.ID); !errors.Is(err, ErrWatchNotFound) {
		t.Errorf("get removed: %v", err)
	}
	r, found, err := watches.Run(run.ID)
	if err != nil || !found || len(r.Results) != 1 || r.Results[0].Watch != 0 || r.Results[0].Title != "Dandadan" {
		t.Errorf("history after remove: %+v, %v", r, err)
	}
}

func TestWatchesLibrary(t *testing.T) {
	data := openData(t)
	watches, library := Watches{DB: data}, Library{DB: data}
	for _, dir := range []string{"Berserk", "Berserk (1989)"} {
		for _, n := range []float32{1, 2.5} {
			if err := library.RecordDownload("/lib", dir, Download{Path: filepath.Join("/lib", dir, fmt.Sprintf("%g.cbz", n)), Provider: "p", MangaID: dir, MangaTitle: dir, Chapter: n, Format: "CBZ"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	keep, _, _ := library.MangaByFolder("/lib", "Berserk")
	other, _, _ := library.MangaByFolder("/lib", "Berserk (1989)")
	have, err := watches.LibraryNumbers(other.ID)
	if err != nil || len(have) != 2 || !have[2.5] {
		t.Fatalf("numbers %v, %v", have, err)
	}

	w, _ := watches.Add(Watch{Provider: "p", MangaID: "Berserk (1989)", Title: "Berserk", LibraryManga: other.ID, Enabled: true})
	if err := library.Merge(keep.ID, other.ID, nil, []string{"/lib/Berserk (1989)/1.cbz", "/lib/Berserk (1989)/2.5.cbz"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := watches.Get(w.ID); got.LibraryManga != keep.ID {
		t.Errorf("watch not moved by the merge: %d, want %d", got.LibraryManga, keep.ID)
	}
	if err := library.RemoveManga(keep.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := watches.Get(w.ID); err != nil || got.LibraryManga != 0 {
		t.Errorf("watch after the library manga went: %+v, %v", got, err)
	}
}

func TestMigrateOldIDsWatches(t *testing.T) {
	cache, data := openBoth(t)
	st := Storage{Cache: cache, Data: data}
	watches := st.Watches()
	loaders := []libmangal.ProviderLoader{loader{weebCentral}}

	both, _ := watches.Add(Watch{Provider: "mango-mangasee", MangaID: "berserk", Title: "Berserk"})
	kept, _ := watches.Add(Watch{Provider: "mango-weebcentral", MangaID: "berserk", Title: "Berserk"})
	only, _ := watches.Add(Watch{Provider: "mango-mangasee", MangaID: "vagabond", Title: "Vagabond"})
	run, _ := watches.StartRun(CheckRun{Kind: "manual", Started: time.Unix(1, 0)})
	for _, w := range []Watch{both, only} {
		if _, err := watches.RecordWatchRun(WatchRun{CheckRun: run.ID, Watch: w.ID, Provider: w.Provider, MangaID: w.MangaID, Status: StatusNone}); err != nil {
			t.Fatal(err)
		}
	}

	oldIDs, err := st.FindOldIDs(loaders)
	if err != nil || len(oldIDs) != 1 || oldIDs[0].Watches != 2 || oldIDs[0].WatchRuns != 2 {
		t.Fatalf("got %+v, %v", oldIDs, err)
	}
	if err := st.MigrateOldIDs(oldIDs); err != nil {
		t.Fatal(err)
	}
	if oldIDs, _ := st.FindOldIDs(loaders); len(oldIDs) != 0 {
		t.Errorf("still %+v", oldIDs)
	}
	list, _ := watches.List()
	if len(list) != 2 || list[0].ID != kept.ID || list[1].ID != only.ID || list[1].Provider != "mango-weebcentral" {
		t.Errorf("watches %+v", list)
	}
	r, _, _ := watches.Run(run.ID)
	if len(r.Results) != 2 || r.Results[0].Watch != kept.ID || r.Results[0].Provider != "mango-weebcentral" || r.Results[1].Watch != only.ID {
		t.Errorf("runs %+v", r.Results)
	}
}

func TestLock(t *testing.T) {
	data := openData(t)
	watches := Watches{DB: data}
	lock, err := watches.Lock("check", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var lockErr *LockError
	if _, err := watches.Lock("check", time.Minute); !errors.As(err, &lockErr) || !errors.Is(err, ErrLocked) || lockErr.Holder.PID == 0 {
		t.Fatalf("locked twice: %v", err)
	}
	if other, err := watches.Lock("other", time.Minute); err != nil {
		t.Errorf("another lock: %v", err)
	} else {
		_ = other.Unlock()
	}
	if err := lock.Heartbeat(); err != nil {
		t.Error(err)
	}

	// a stale lock is taken over, the old holder lost it
	if _, err := data.Exec(`UPDATE locks SET heartbeat = heartbeat - 3600`); err != nil {
		t.Fatal(err)
	}
	taken, err := watches.Lock("check", time.Minute)
	if err != nil {
		t.Fatalf("stale lock not taken: %v", err)
	}
	if err := lock.Heartbeat(); err == nil {
		t.Error("the old holder still has it")
	}
	_ = lock.Unlock() // not its lock anymore, no-op
	if _, err := watches.Lock("check", time.Minute); !errors.Is(err, ErrLocked) {
		t.Errorf("unlocked by the old holder: %v", err)
	}
	if err := taken.Unlock(); err != nil {
		t.Fatal(err)
	}
	if l, err := watches.Lock("check", time.Minute); err != nil {
		t.Errorf("after unlock: %v", err)
	} else {
		_ = l.Unlock()
	}

	// a holder of this host that doesn't run anymore loses it at once
	dead, err := watches.Lock("check", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.Exec(`UPDATE locks SET pid = 0`); err != nil {
		t.Fatal(err)
	}
	if l, err := watches.Lock("check", time.Minute); err != nil {
		t.Errorf("lock of a dead process not taken: %v", err)
	} else {
		_ = l.Unlock()
	}
	_ = dead.Unlock()
}
