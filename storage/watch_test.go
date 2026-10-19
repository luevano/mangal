package storage

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
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

//go:fix inline
func TestWatches(t *testing.T) {
	data := openData(t)
	watches := Watches{DB: data}

	w, err := watches.Add(Watch{Provider: "mango-mangaplus", MangaID: "100171", Title: "Dandadan", Enabled: true, From: new(float32(200))})
	if err != nil || w.ID == 0 {
		t.Fatalf("add: %+v, %v", w, err)
	}
	if _, err := watches.Add(Watch{Provider: "mango-mangaplus", MangaID: "100171"}); !errors.Is(err, ErrWatchExists) {
		t.Errorf("added twice: %v", err)
	}
	got, err := watches.Get(w.ID)
	if err != nil || got.Title != "Dandadan" || !got.Enabled || got.From == nil || *got.From != 200 || got.Schedule != "" || got.LastNew != nil {
		t.Fatalf("get: %+v, %v", got, err)
	}

	got.Schedule = "0 */2 * * *"
	got.LastChecked, got.LastNew, got.LastNewAt = time.Unix(100, 0), new(float32(201.5)), time.Unix(100, 0)
	got.Failures, got.LastError, got.Enabled = 2, "boom", false
	if err := watches.Save(got); err != nil {
		t.Fatal(err)
	}
	got, _ = watches.Get(w.ID)
	if got.Schedule != "0 */2 * * *" || *got.LastNew != 201.5 || got.LastChecked.Unix() != 100 || got.Failures != 2 || got.Enabled {
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
		From: new(float32(200)), Status: StatusNew, Started: time.Unix(200, 0), Finished: time.Unix(210, 0),
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
	if n := runs[1].Results[0].NewChapters; n != 2 {
		t.Errorf("a check's count is its chapters: %d", n)
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

	// by provider and manga: the removed watch's checks still
	_, _ = watches.RecordWatchRun(WatchRun{CheckRun: other.ID, Provider: "mango-mangapill", MangaID: "2", Title: "Berserk", Status: StatusNone})
	_, _ = watches.RecordWatchRun(WatchRun{CheckRun: other.ID, Provider: "mango-mangaplus", MangaID: "3", Title: "Frieren", Status: StatusNone})
	runs, _ = watches.Runs(RunFilter{Provider: "mango-mangaplus"})
	if len(runs) != 2 || len(runs[0].Results) != 1 || runs[0].Results[0].Title != "Frieren" || runs[1].Results[0].Title != "Dandadan" {
		t.Errorf("runs of the provider: %+v", runs)
	}
	runs, _ = watches.Runs(RunFilter{Provider: "mango-mangaplus", MangaID: "100171"})
	if len(runs) != 1 || runs[0].ID != run.ID {
		t.Errorf("runs of the manga: %+v", runs)
	}
	if runs, _ := watches.Runs(RunFilter{Provider: "mango-mangadex"}); len(runs) != 0 {
		t.Errorf("runs of another provider: %+v", runs)
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
	if len(list) == 2 && (list[0].NeedsRelink || !list[1].NeedsRelink || list[1].RelinkReason != movedReason("mango-mangasee")) {
		t.Errorf("re-link flags %+v", list)
	}
	r, _, _ := watches.Run(run.ID)
	if len(r.Results) != 2 || r.Results[0].Watch != kept.ID || r.Results[0].Provider != "mango-weebcentral" || r.Results[1].Watch != only.ID {
		t.Errorf("runs %+v", r.Results)
	}
}

// TestMigrateWatchSchedules: the per-watch intervals (schema 8) become cron schedules.
func TestMigrateWatchSchedules(t *testing.T) {
	path := filepath.Join(t.TempDir(), FilenameData)
	old, err := Open(path, dataMigrations[:8])
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO watches (provider, manga_id, title, interval, added) VALUES ('p', 'a', 'A', NULL, 10)`,
		`INSERT INTO watches (provider, manga_id, title, interval, added) VALUES ('p', 'b', 'B', 7200, 10)`,
		`INSERT INTO watches (provider, manga_id, title, interval, added) VALUES ('p', 'c', 'C', 259200, 10)`,
		`INSERT INTO watches (provider, manga_id, title, interval, added) VALUES ('p', 'd', 'D', 18000, 10)`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()

	data, err := OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	list, err := (Watches{DB: data}).List()
	if err != nil || len(list) != 4 {
		t.Fatalf("watches: %+v, %v", list, err)
	}
	for i, want := range []string{"", "0 */2 * * *", "0 0 */3 * *", "@every 5h"} {
		if list[i].Schedule != want {
			t.Errorf("%s: schedule %q, want %q", list[i].Title, list[i].Schedule, want)
		}
	}
}

func TestWatchRelink(t *testing.T) {
	data := openData(t)
	watches := Watches{DB: data}
	bindings := Bindings{DB: data}

	w, _ := watches.Add(Watch{Provider: "mango-weebcentral", MangaID: "title:Berserk", Title: "Berserk", NeedsRelink: true, RelinkReason: "placeholder manga id"})
	other, _ := watches.Add(Watch{Provider: "mango-weebcentral", MangaID: "01J76XY", Title: "Vagabond"})
	if got, _ := watches.Get(w.ID); !got.NeedsRelink || got.RelinkReason != "placeholder manga id" {
		t.Errorf("flag not stored: %+v", got)
	}
	_ = bindings.Set(Binding{Provider: w.Provider, MangaID: w.MangaID, Source: "al", ID: "30002"})
	_ = bindings.Set(Binding{Provider: "mango-mangapill", MangaID: "1/berserk", Source: "al", ID: "1"})
	_ = bindings.Set(Binding{Provider: "mango-mangapill", MangaID: "1/berserk", Source: "kt", ID: "2"})

	if err := watches.Relink(w.ID, other.Provider, other.MangaID, "x", 0, time.Now()); !errors.Is(err, ErrWatchExists) {
		t.Errorf("re-linked to another watch's manga: %v", err)
	}
	got, _ := watches.Get(w.ID)
	got.Failures, got.LastError, got.From = 3, "manga not found", new(float32(10))
	_ = watches.Save(got)
	at := time.Unix(1_000_000, 0)
	if err := watches.Relink(w.ID, "mango-mangapill", "1/berserk", "Berserk (pill)", 0, at); err != nil {
		t.Fatal(err)
	}
	got, _ = watches.Get(w.ID)
	if got.Provider != "mango-mangapill" || got.MangaID != "1/berserk" || got.Title != "Berserk (pill)" || got.NeedsRelink ||
		got.Failures != 0 || got.LastError != "" || got.From == nil || *got.From != 10 || !got.Resumed.Equal(at) {
		t.Errorf("re-linked: %+v", got)
	}
	// a re-link of a watch that didn't need one keeps when it was resumed
	if err := watches.Relink(w.ID, "mango-mangapill", "1/berserk", "Berserk (pill)", 0, at.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ = watches.Get(w.ID); !got.Resumed.Equal(at) {
		t.Errorf("resumed %s", got.Resumed)
	}
	list, _ := bindings.Get("mango-mangapill", "1/berserk")
	if len(list) != 2 || list[0].ID != "30002" || list[1].ID != "2" {
		t.Errorf("bindings %+v", list)
	}
	if old, _ := bindings.Get("mango-weebcentral", "title:Berserk"); len(old) != 0 {
		t.Errorf("old bindings left %+v", old)
	}
	if err := watches.Relink(99, "p", "m", "t", 0, time.Now()); !errors.Is(err, ErrWatchNotFound) {
		t.Errorf("missing watch: %v", err)
	}
}

// TestMigrateRelink: placeholder ids (schema 9) need a re-link.
func TestMigrateRelink(t *testing.T) {
	path := filepath.Join(t.TempDir(), FilenameData)
	old, err := Open(path, dataMigrations[:9])
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO watches (provider, manga_id, title, added) VALUES ('p', 'title:Berserk', 'Berserk', 10)`,
		`INSERT INTO watches (provider, manga_id, title, added) VALUES ('p', '123', 'B', 10)`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()
	data, err := OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	list, err := (Watches{DB: data}).List()
	if err != nil || len(list) != 2 || !list[0].NeedsRelink || list[1].NeedsRelink {
		t.Errorf("watches %+v, %v", list, err)
	}
}

func TestWatchRunNewChapters(t *testing.T) {
	watches := Watches{DB: openData(t)}
	// imported history with only a count: the count stays, more than the chapters recorded
	imported, _ := watches.StartRun(CheckRun{Kind: "import", Started: time.Unix(150, 0)})
	if _, err := watches.RecordWatchRun(WatchRun{CheckRun: imported.ID, Provider: "mango-mangadex", MangaID: "title:Berserk",
		Title: "Berserk", Status: StatusNew, NewChapters: 384, Chapters: []RunChapter{{Number: 376}}}); err != nil {
		t.Fatal(err)
	}
	if r, _, _ := watches.Run(imported.ID); len(r.Results) != 1 || r.Results[0].New() != 384 || len(r.Results[0].Chapters) != 1 {
		t.Errorf("imported %+v", r.Results)
	}
}

func TestWatchesWithDownloads(t *testing.T) {
	watches := Watches{DB: openData(t)}
	a, _ := watches.Add(Watch{Provider: "p", MangaID: "a", Title: "A"})
	b, _ := watches.Add(Watch{Provider: "p", MangaID: "b", Title: "B"})
	run, _ := watches.StartRun(CheckRun{Kind: "check", Started: time.Now()})
	_, _ = watches.RecordWatchRun(WatchRun{CheckRun: run.ID, Watch: a.ID, Provider: "p", MangaID: "a", Status: StatusNew, Started: time.Now(), Finished: time.Now(),
		Chapters: []RunChapter{{Number: 1, Path: "/dl/a/1.cbz"}}})
	_, _ = watches.RecordWatchRun(WatchRun{CheckRun: run.ID, Watch: b.ID, Provider: "p", MangaID: "b", Status: StatusNone, Started: time.Now(), Finished: time.Now()})
	with, err := watches.WithDownloads()
	if err != nil || !with[a.ID] || with[b.ID] {
		t.Errorf("with downloads %v, %v", with, err)
	}
}

func TestSaveCheck(t *testing.T) {
	data, err := OpenData(filepath.Join(t.TempDir(), FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	s := Watches{DB: data}
	w, err := s.Add(Watch{Provider: "p", MangaID: "a", Title: "A", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	checked := w
	checked.Enabled, checked.Skip, checked.Title, checked.LastChecked, checked.Retry = true, nil, "A2", time.Unix(100, 0), []float32{3}
	// paused and a skip list set while it was checked
	w.Enabled, w.Skip = false, []float32{9}
	if err := s.Save(w); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCheck(checked); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(w.ID)
	if got.Enabled || !slices.Equal(got.Skip, []float32{9}) || got.Title != "A2" || !got.LastChecked.Equal(time.Unix(100, 0)) || !slices.Equal(got.Retry, []float32{3}) {
		t.Errorf("got %+v", got)
	}
	// re-linked meanwhile: the check was of the old manga
	if err := s.Relink(w.ID, "p", "b", "B", 0, time.Unix(200, 0)); err != nil {
		t.Fatal(err)
	}
	checked.Title = "A3"
	if err := s.SaveCheck(checked); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(w.ID); got.Title != "B" {
		t.Errorf("re-linked watch overwritten: %+v", got)
	}

	// the check sets and clears an id re-link
	w, _ = s.Get(w.ID)
	checked = w
	checked.NeedsRelink, checked.RelinkReason = true, "gone"
	if err := s.SaveCheck(checked); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(w.ID); got.RelinkLabel() != "needs re-link" || got.RelinkReason != "gone" {
		t.Errorf("id re-link not set: %+v", got)
	}
	checked.NeedsRelink, checked.RelinkReason = false, ""
	if err := s.SaveCheck(checked); err != nil {
		t.Fatal(err)
	}
	// a new provider asked for while it was checked stays
	w, _ = s.Get(w.ID)
	w.NeedsRelink, w.RelinkKind, w.RelinkReason = true, RelinkProvider, "has 1-5"
	if err := s.Save(w); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCheck(checked); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(w.ID)
	if got.RelinkLabel() != "needs a new provider" || got.RelinkReason != "has 1-5" {
		t.Errorf("provider re-link wiped: %+v", got)
	}
	// and one cleared while it was checked stays cleared
	checked = got
	got.NeedsRelink, got.RelinkKind, got.RelinkReason = false, "", ""
	if err := s.Save(got); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCheck(checked); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(w.ID); got.NeedsRelink {
		t.Errorf("cleared provider re-link back: %+v", got)
	}
}

func TestWatchUpdate(t *testing.T) {
	data, err := OpenData(filepath.Join(t.TempDir(), FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	s := Watches{DB: data}
	w, err := s.Add(Watch{Provider: "p", MangaID: "a", Title: "A", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(w.ID, func(w *Watch) { w.Enabled = false }); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(w.ID); got.Enabled {
		t.Errorf("not updated: %+v", got)
	}
	if _, err := s.Update(w.ID+1, func(*Watch) {}); !errors.Is(err, ErrWatchNotFound) {
		t.Errorf("missing watch: %v", err)
	}
}
