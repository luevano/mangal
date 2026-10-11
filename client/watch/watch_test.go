package watch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/storage"
)

type fakeManga struct {
	info mangadata.MangaInfo
	meta metadata.Metadata
}

func (m *fakeManga) String() string                     { return m.info.Title }
func (m *fakeManga) Info() mangadata.MangaInfo          { return m.info }
func (m *fakeManga) Metadata() metadata.Metadata        { return m.meta }
func (m *fakeManga) SetMetadata(meta metadata.Metadata) { m.meta = meta }
func (m *fakeManga) chapter(n float32) *fakeChapter {
	return &fakeChapter{info: mangadata.ChapterInfo{Number: n, Title: fmt.Sprintf("ch %g", n)}, vol: &fakeVolume{m}}
}
func (m *fakeManga) unavailable(n float32) *fakeChapter {
	c := m.chapter(n)
	c.info.Unavailable = "external"
	return c
}
func (m *fakeManga) chapters(ns ...float32) []*fakeChapter { return chapterList(m, ns) }

func chapterList(m *fakeManga, ns []float32) []*fakeChapter {
	var chs []*fakeChapter
	for _, n := range ns {
		chs = append(chs, m.chapter(n))
	}
	return chs
}

type fakeVolume struct{ manga *fakeManga }

func (v *fakeVolume) String() string             { return "vol" }
func (v *fakeVolume) Info() mangadata.VolumeInfo { return mangadata.VolumeInfo{Number: 1} }
func (v *fakeVolume) Manga() mangadata.Manga     { return v.manga }

type fakeChapter struct {
	info mangadata.ChapterInfo
	vol  *fakeVolume
}

func (c *fakeChapter) String() string              { return c.info.Title }
func (c *fakeChapter) Info() mangadata.ChapterInfo { return c.info }
func (c *fakeChapter) Volume() mangadata.Volume    { return c.vol }

type fakeMeta struct {
	metadata.Metadata
	id int
}

func (m fakeMeta) String() string { return "meta" }
func (m fakeMeta) ID() metadata.ID {
	return metadata.ID{Raw: fmt.Sprint(m.id), Source: metadata.IDSourceAnilist}
}
func (m fakeMeta) ExtraIDs() []metadata.ID { return nil }

// fakeSource is a provider whose downloads are recorded in the library like client.Download.
type fakeSource struct {
	id      string
	noByID  bool
	mangas  []*fakeManga
	chs     map[string][]*fakeChapter
	library storage.Library
	// fail are the chapter numbers that fail to download
	fail map[float32]bool
	err  error
	// gate, when set, makes Chapters say so on entered and wait for it (or the ctx)
	gate    chan struct{}
	entered chan struct{}

	mu         sync.Mutex
	downloaded []float32
	// checked are the bindings checked with BoundMetadata
	checked []string
}

func (s *fakeSource) Info() libmangal.ProviderInfo {
	return libmangal.ProviderInfo{ID: s.id, Name: s.id}
}

func (s *fakeSource) MangaByID(_ context.Context, id string) (mangadata.Manga, bool, error) {
	if s.noByID {
		return nil, false, fmt.Errorf("provider %q: %w", s.id, libmangal.ErrMangaByIDUnsupported)
	}
	for _, m := range s.mangas {
		if m.info.ID == id {
			return m, true, nil
		}
	}
	return nil, false, nil
}

func (s *fakeSource) SearchMangas(_ context.Context, query string) ([]mangadata.Manga, error) {
	var found []mangadata.Manga
	for _, m := range s.mangas {
		if m.info.Title == query {
			found = append(found, m)
		}
	}
	return found, nil
}

func (s *fakeSource) Chapters(ctx context.Context, manga mangadata.Manga) ([]mangadata.Chapter, error) {
	if s.gate != nil {
		s.entered <- struct{}{}
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	var chs []mangadata.Chapter
	for _, c := range s.chs[manga.Info().ID] {
		chs = append(chs, c)
	}
	return chs, nil
}

func (s *fakeSource) BoundMetadata(_ context.Context, code metadata.IDCode, id string) (metadata.Metadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checked = append(s.checked, string(code)+":"+id)
	n, err := strconv.Atoi(id)
	if err != nil || n == 404 {
		return nil, fmt.Errorf("%s manga %s not found", code, id)
	}
	return fakeMeta{id: n}, nil
}

func (s *fakeSource) Download(ctx context.Context, chapters []mangadata.Chapter, onEvent func(client.Event)) client.Results {
	results := client.NewResults(chapters)
	for i, r := range results {
		info := r.Chapter.Info()
		if info.Unavailable != "" {
			r.Unavailable = info.Unavailable
			continue
		}
		if s.fail[info.Number] {
			r.Err = errors.New("broken")
			continue
		}
		manga := r.Chapter.Volume().Manga().Info()
		dir := filepath.Join("/lib", manga.Title)
		down := &metadata.DownloadedChapter{Number: info.Number, Directory: dir, Filename: fmt.Sprintf("%g.cbz", info.Number), Pages: 1}
		if err := s.library.RecordDownload("/lib", manga.Title, storage.Download{Path: down.Path(), Provider: s.id, MangaID: manga.ID,
			MangaTitle: manga.Title, Chapter: info.Number, Format: "CBZ", Downloaded: time.Now()}); err != nil {
			r.Err = err
			continue
		}
		r.Down = down
		s.mu.Lock()
		s.downloaded = append(s.downloaded, info.Number)
		s.mu.Unlock()
		if onEvent != nil {
			onEvent(client.Event{Kind: client.EventDone, Index: i, Result: r})
		}
	}
	return results
}

type fakeNotifier struct {
	mu         sync.Mutex
	news, errs []Result
}

func (n *fakeNotifier) New(_ context.Context, r Result) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.news = append(n.news, r)
	return nil
}

func (n *fakeNotifier) Error(_ context.Context, r Result) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.errs = append(n.errs, r)
	return nil
}

type env struct {
	svc      *Service
	sources  map[string]*fakeSource
	notifier *fakeNotifier
	now      time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	data, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	e := &env{sources: map[string]*fakeSource{}, notifier: &fakeNotifier{}, now: time.Unix(1_000_000, 0)}
	e.svc = &Service{
		Watches: storage.Watches{DB: data},
		Library: storage.Library{DB: data},
		Options: Options{Interval: 4 * time.Hour, ParallelProviders: 2, Backoff: 3, Notify: NotifyBoth, LockStale: time.Minute},
		Source: func(_ context.Context, id string) (Source, error) {
			src, ok := e.sources[id]
			if !ok {
				return nil, fmt.Errorf("provider with id %q not found", id)
			}
			if src.err != nil && src.chs == nil {
				return nil, src.err
			}
			return src, nil
		},
		Notifier: e.notifier,
		Now:      func() time.Time { return e.now },
		OnError:  func(err error) { t.Errorf("unexpected: %s", err) },
	}
	return e
}

func (e *env) source(id string, mangas ...*fakeManga) *fakeSource {
	src := &fakeSource{id: id, mangas: mangas, chs: map[string][]*fakeChapter{}, library: e.svc.Library}
	e.sources[id] = src
	return src
}

// own records chapters of the manga as downloaded before.
func (e *env) own(t *testing.T, provider string, m *fakeManga, ns ...float32) {
	t.Helper()
	for _, n := range ns {
		err := e.svc.Library.RecordDownload("/lib", m.info.Title, storage.Download{Path: fmt.Sprintf("/lib/%s/%g.cbz", m.info.Title, n),
			Provider: provider, MangaID: m.info.ID, MangaTitle: m.info.Title, Chapter: n, Format: "CBZ"})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func numbers(chs []storage.RunChapter) []float32 {
	var ns []float32
	for _, c := range chs {
		ns = append(ns, c.Number)
	}
	slices.Sort(ns)
	return ns
}

func f32(f float32) *float32 { return &f }

func TestPick(t *testing.T) {
	m := &fakeManga{info: mangadata.MangaInfo{ID: "m", Title: "M"}}
	all := []mangadata.Chapter{m.chapter(1), m.chapter(2), m.chapter(3), m.chapter(4), m.chapter(4.5), m.chapter(5), m.chapter(5), m.unavailable(6), m.chapter(7)}
	have := map[float32]bool{1: true, 3: true, 4: true}
	pick := func(have map[float32]bool, last, from *float32, gaps bool) ([]float32, int) {
		picked, unavailable := Pick(all, have, last, from, gaps)
		var ns []float32
		for _, c := range picked {
			ns = append(ns, c.Info().Number)
		}
		return ns, unavailable
	}
	for _, tc := range []struct {
		name        string
		have        map[float32]bool
		last, from  *float32
		gaps        bool
		want        []float32
		unavailable int
	}{
		{name: "from the last, .5 caught", have: have, last: f32(4), want: []float32{4.5, 5, 7}, unavailable: 1},
		{name: "gaps", have: have, last: f32(4), gaps: true, want: []float32{2, 4.5, 5, 7}, unavailable: 1},
		{name: "first check", want: []float32{1, 2, 3, 4, 4.5, 5, 7}, unavailable: 1},
		{name: "from", from: f32(4), want: []float32{4.5, 5, 7}, unavailable: 1},
		{name: "from above the last", have: have, last: f32(4), from: f32(5), want: []float32{7}, unavailable: 1},
		{name: "gaps above from", have: map[float32]bool{5: true}, last: f32(5), from: f32(1), gaps: true, want: []float32{2, 3, 4, 4.5, 7}, unavailable: 1},
	} {
		got, unavailable := pick(tc.have, tc.last, tc.from, tc.gaps)
		if !slices.Equal(got, tc.want) || unavailable != tc.unavailable {
			t.Errorf("%s: got %v (%d unavailable), want %v (%d)", tc.name, got, unavailable, tc.want, tc.unavailable)
		}
	}
}

func TestCheck(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	m := &fakeManga{info: mangadata.MangaInfo{ID: "dandadan", Title: "Dandadan"}}
	src := e.source("mango-mangaplus", m)
	src.chs["dandadan"] = append(m.chapters(10, 63, 64, 65, 65.5, 66), m.unavailable(67))
	e.own(t, "mango-mangaplus", m, 1, 2, 63, 64, 65)

	w, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangaplus", Query: "mid:dandadan", AnilistID: 132029})
	if err != nil {
		t.Fatal(err)
	}
	if w.Title != "Dandadan" || w.LibraryManga == 0 || w.LastDownloaded == nil || *w.LastDownloaded != 65 || !w.NextCheck.Equal(e.now) {
		t.Fatalf("added %+v", w)
	}
	// --anilist-id is a default AniList binding
	if w.AnilistID != 132029 || len(w.Bindings) != 1 || w.Bindings[0].Source != metadata.IDCodeAnilist || w.Bindings[0].ID != "132029" ||
		!w.Bindings[0].Default || w.Bindings[0].Provider != "" || !slices.Equal(src.checked, []string{"al:132029"}) {
		t.Errorf("binding %+v, checked %v", w.Bindings, src.checked)
	}
	if b, found, _ := (storage.Bindings{DB: e.svc.Watches.DB}).Default("mango-mangaplus", "dandadan"); !found || b.ID != "132029" {
		t.Errorf("stored binding %+v", b)
	}
	if _, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangaplus", Query: "mid:dandadan", AnilistID: 404}); err == nil {
		t.Error("added with a missing anilist manga")
	}
	if _, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangaplus", MangaID: "dandadan"}); !errors.Is(err, ErrExists) {
		t.Errorf("added twice: %v", err)
	}

	// dry run: nothing downloaded, recorded or notified
	report, err := e.svc.Check(ctx, nil, CheckOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	r := report.Results[0]
	if report.ID != 0 || r.Status != storage.StatusNew || !slices.Equal(numbers(r.Chapters), []float32{65.5, 66}) || r.Unavailable != 1 || *r.From != 65 {
		t.Errorf("dry run %+v", report)
	}
	if len(src.downloaded) != 0 || len(e.notifier.news) != 0 {
		t.Errorf("dry run downloaded %v, notified %d", src.downloaded, len(e.notifier.news))
	}
	if runs, _ := e.svc.Runs(ctx, RunFilter{}); len(runs) != 0 {
		t.Errorf("dry run recorded %+v", runs)
	}

	e.now = e.now.Add(time.Minute)
	var events []EventKind
	report, err = e.svc.Check(ctx, nil, CheckOptions{OnEvent: func(ev Event) { events = append(events, ev.Kind) }})
	if err != nil {
		t.Fatal(err)
	}
	r = report.Results[0]
	if report.ID == 0 || report.Watches != 1 || report.NewChapters != 2 || r.Status != storage.StatusNew || r.ID == 0 {
		t.Errorf("check %+v", report)
	}
	if !slices.Equal(src.downloaded, []float32{65.5, 66}) {
		t.Errorf("downloaded %v", src.downloaded)
	}
	if !slices.Equal(events, []EventKind{EventStart, EventFound, EventChapter, EventChapter, EventDone}) {
		t.Errorf("events %v", events)
	}
	// one notification for the manga
	if len(e.notifier.news) != 1 || len(e.notifier.errs) != 0 || *e.notifier.news[0].Newest() != 66 || *e.notifier.news[0].From != 65 {
		t.Errorf("notified %+v / %+v", e.notifier.news, e.notifier.errs)
	}
	w, _ = e.svc.Get(ctx, w.ID)
	if *w.LastNew != 66 || !w.LastNewAt.Equal(e.now) || !w.LastChecked.Equal(e.now) || *w.LastDownloaded != 66 || !w.NextCheck.Equal(e.now.Add(4*time.Hour)) {
		t.Errorf("watch after %+v", w)
	}
	runs, _ := e.svc.Runs(ctx, RunFilter{Watch: w.ID})
	if len(runs) != 1 || runs[0].NewChapters != 2 || len(runs[0].Results) != 1 || runs[0].Results[0].Chapters[1].Path != "/lib/Dandadan/66.cbz" {
		t.Errorf("runs %+v", runs)
	}

	// nothing new: still recorded
	e.now = e.now.Add(time.Minute)
	report, _ = e.svc.Check(ctx, []int64{w.ID}, CheckOptions{})
	if r := report.Results[0]; r.Status != storage.StatusNone || len(r.Chapters) != 0 || *r.From != 66 {
		t.Errorf("second check %+v", r)
	}
	if len(e.notifier.news) != 1 {
		t.Errorf("notified nothing new")
	}

	// gaps: 10 is below the last and missing
	report, _ = e.svc.Check(ctx, nil, CheckOptions{Kind: KindGaps, Gaps: true})
	if r := report.Results[0]; r.Status != storage.StatusNew || !slices.Equal(numbers(r.Chapters), []float32{10}) || !r.Gaps {
		t.Errorf("gaps %+v", r)
	}
	if w, _ = e.svc.Get(ctx, w.ID); !w.LastGapCheck.Equal(e.now) {
		t.Errorf("last gap check %s", w.LastGapCheck)
	}
	if runs, _ := e.svc.Runs(ctx, RunFilter{}); len(runs) != 3 || runs[0].Kind != KindGaps {
		t.Errorf("runs %+v", runs)
	}
}

func TestCheckFirstAndFrom(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	opm := &fakeManga{info: mangadata.MangaInfo{ID: "opm", Title: "One-Punch Man"}}
	other := &fakeManga{info: mangadata.MangaInfo{ID: "x", Title: "X"}}
	src := e.source("mango-mangapill", opm, other)
	// a provider that can't get mangas by id: found by its title
	src.noByID = true
	src.chs["opm"] = opm.chapters(1, 2, 3, 3.5, 4)
	src.chs["x"] = other.chapters(1, 2)

	if _, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangapill", Query: "mid:opm"}); err == nil {
		t.Error("an id without a provider that can look it up")
	}
	w1, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangapill", Query: "One-Punch Man", From: f32(3)})
	if err != nil {
		t.Fatal(err)
	}
	w2, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangapill", Query: "X"})
	if err != nil {
		t.Fatal(err)
	}
	report, err := e.svc.Check(ctx, nil, CheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r := report.Results[0]; r.Watch != w1.ID || !slices.Equal(numbers(r.Chapters), []float32{3.5, 4}) || *r.From != 3 {
		t.Errorf("from 3: %+v", r)
	}
	if r := report.Results[1]; r.Watch != w2.ID || !slices.Equal(numbers(r.Chapters), []float32{1, 2}) || r.From != nil {
		t.Errorf("first check: %+v", r)
	}
	// the first check linked the library manga
	if w, _ := e.svc.Get(ctx, w2.ID); w.LibraryManga == 0 || *w.LastDownloaded != 2 {
		t.Errorf("after first check %+v", w)
	}
}

func TestCheckFailures(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	a := &fakeManga{info: mangadata.MangaInfo{ID: "a", Title: "A"}}
	b := &fakeManga{info: mangadata.MangaInfo{ID: "b", Title: "B"}}
	c := &fakeManga{info: mangadata.MangaInfo{ID: "c", Title: "C"}}
	good := e.source("good", a, c)
	good.chs["a"] = a.chapters(1, 2, 3)
	good.chs["c"] = c.chapters(1)
	good.fail = map[float32]bool{3: true}
	broken := e.source("broken", b)
	broken.chs["b"] = b.chapters(1)

	var ids []int64
	for _, opts := range []AddOptions{{Provider: "good", MangaID: "a"}, {Provider: "broken", MangaID: "b"}, {Provider: "good", MangaID: "c"}} {
		w, err := e.svc.Add(ctx, opts)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, w.ID)
	}
	broken.err = errors.New("site down")

	report, err := e.svc.Check(ctx, nil, CheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ra, rb, rc := report.Results[0], report.Results[1], report.Results[2]
	// a chapter failing: what downloaded counts, the error is kept
	if ra.Status != storage.StatusNew || ra.Failed != 1 || ra.Error == "" || !slices.Equal(numbers(ra.Chapters), []float32{1, 2}) {
		t.Errorf("a: %+v", ra)
	}
	if rb.Status != storage.StatusError || rb.Error == "" {
		t.Errorf("b: %+v", rb)
	}
	// the broken provider didn't stop the good one
	if rc.Status != storage.StatusNew {
		t.Errorf("c: %+v", rc)
	}
	if report.Errors != 1 || report.Watches != 3 || report.NewChapters != 3 {
		t.Errorf("report %+v", report.CheckRun)
	}
	if len(e.notifier.news) != 2 || len(e.notifier.errs) != 2 {
		t.Errorf("notified %d new, %d errors", len(e.notifier.news), len(e.notifier.errs))
	}

	// backoff: after 3 failures in a row the interval doubles
	for range 3 {
		e.now = e.now.Add(time.Hour)
		_, _ = e.svc.Check(ctx, []int64{ids[1]}, CheckOptions{})
	}
	w, _ := e.svc.Get(ctx, ids[1])
	if w.Failures != 4 || !w.NextCheck.Equal(w.LastChecked.Add(16*time.Hour)) {
		t.Errorf("backoff: %d failures, next %s after %s", w.Failures, w.NextCheck, w.LastChecked)
	}
	broken.err = nil
	e.now = e.now.Add(time.Hour)
	_, _ = e.svc.Check(ctx, []int64{ids[1]}, CheckOptions{})
	if w, _ := e.svc.Get(ctx, ids[1]); w.Failures != 0 || w.LastError != "" {
		t.Errorf("recovered: %+v", w)
	}

	// notify none
	e.svc.Options.Notify = NotifyNone
	broken.err = errors.New("down again")
	news, errs := len(e.notifier.news), len(e.notifier.errs)
	_, _ = e.svc.Check(ctx, nil, CheckOptions{})
	if len(e.notifier.news) != news || len(e.notifier.errs) != errs {
		t.Error("notified with watch.notify none")
	}
}

func TestCheckBusy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	m := &fakeManga{info: mangadata.MangaInfo{ID: "m", Title: "M"}}
	n := &fakeManga{info: mangadata.MangaInfo{ID: "n", Title: "N"}}
	e.source("p", m).chs["m"] = m.chapters(1)
	e.source("q", n).chs["n"] = n.chapters(1)
	wp, err := e.svc.Add(ctx, AddOptions{Provider: "p", MangaID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	wq, err := e.svc.Add(ctx, AddOptions{Provider: "q", MangaID: "n"})
	if err != nil {
		t.Fatal(err)
	}
	// another process checks p: q goes on, p's watch is skipped (not recorded)
	lock, err := e.svc.Watches.Lock(lockName("p"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var busy []string
	report, err := e.svc.Check(ctx, nil, CheckOptions{OnBusy: func(p []string) { busy = p }})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(report.Busy, []string{"p"}) || !slices.Equal(busy, []string{"p"}) || report.Watches != 1 ||
		report.Results[0].Status != storage.StatusSkipped || report.Results[0].Error != "provider busy" || report.Results[1].Status != storage.StatusNew {
		t.Errorf("one provider busy: %+v", report)
	}
	run, _ := e.svc.RunDetails(ctx, report.ID)
	if len(run.Results) != 1 || run.Results[0].Watch != wq.ID {
		t.Errorf("recorded %+v", run.Results)
	}
	if _, err := e.svc.Check(ctx, []int64{wp.ID}, CheckOptions{}); !errors.Is(err, ErrBusy) {
		t.Errorf("every provider busy: %v", err)
	}
	if _, err := e.svc.Check(ctx, nil, CheckOptions{DryRun: true}); err != nil {
		t.Errorf("a dry run doesn't need the locks: %v", err)
	}
	_ = lock.Unlock()
	if _, err := e.svc.Check(ctx, []int64{wp.ID}, CheckOptions{}); err != nil {
		t.Errorf("after unlock: %v", err)
	}
	// the check released them
	for _, p := range []string{"p", "q"} {
		if l, err := e.svc.Watches.Lock(lockName(p), time.Minute); err != nil {
			t.Errorf("lock kept: %v", err)
		} else {
			_ = l.Unlock()
		}
	}
}

// TestCheckParallel: checks of different providers run at once, never two of the same.
func TestCheckParallel(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.svc.Events = NewHub()
	m := &fakeManga{info: mangadata.MangaInfo{ID: "m", Title: "M"}}
	n := &fakeManga{info: mangadata.MangaInfo{ID: "n", Title: "N"}}
	p := e.source("p", m)
	p.chs["m"] = m.chapters(1, 2)
	q := e.source("q", n)
	q.chs["n"] = n.chapters(5)
	wp, _ := e.svc.Add(ctx, AddOptions{Provider: "p", MangaID: "m"})
	wq, _ := e.svc.Add(ctx, AddOptions{Provider: "q", MangaID: "n"})
	p.gate, p.entered = make(chan struct{}), make(chan struct{}, 1)
	q.gate, q.entered = make(chan struct{}), make(chan struct{}, 1)

	events := e.svc.Events.Subscribe(t.Context())
	type result struct {
		report Report
		err    error
	}
	check := func(id int64) chan result {
		done := make(chan result, 1)
		go func() {
			r, err := e.svc.Check(ctx, []int64{id}, CheckOptions{})
			done <- result{r, err}
		}()
		return done
	}
	donep := check(wp.ID)
	<-p.entered
	doneq := check(wq.ID)
	<-q.entered

	// both running: the status says what each one does, p can't be checked twice
	st, err := e.svc.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ps, ok := st.Provider("p")
	if !st.Checking || len(st.Providers) != 2 || !ok || ps.Watch != wp.ID || ps.Title != "M" || ps.Run == 0 || ps.State != "checking" {
		t.Errorf("status %+v", st)
	}
	if _, err := e.svc.Check(ctx, []int64{wp.ID}, CheckOptions{}); !errors.Is(err, ErrBusy) {
		t.Errorf("p checked twice: %v", err)
	}
	// a subscriber that comes now sees what happened
	var kinds []string
	for _, ev := range e.svc.Events.Recent() {
		if ev.Provider == "p" {
			kinds = append(kinds, ev.Kind)
		}
	}
	if !slices.Equal(kinds, []string{LiveProviderStart, LiveStart}) {
		t.Errorf("recent of p %v", kinds)
	}

	close(p.gate)
	close(q.gate)
	runs := map[int64]string{}
	for name, done := range map[string]chan result{"p": donep, "q": doneq} {
		r := <-done
		if r.err != nil || r.report.NewChapters == 0 {
			t.Errorf("check %+v %v", r.report, r.err)
		}
		runs[r.report.ID] = name
	}
	if st, _ := e.svc.Status(ctx); st.Checking || len(st.Providers) != 0 {
		t.Errorf("status after %+v", st)
	}

	byRun := map[string][]string{}
	for len(byRun["p"]) == 0 || byRun["p"][len(byRun["p"])-1] != LiveRunDone || len(byRun["q"]) == 0 || byRun["q"][len(byRun["q"])-1] != LiveRunDone {
		select {
		case ev := <-events:
			byRun[runs[ev.Run]] = append(byRun[runs[ev.Run]], ev.Kind)
		case <-time.After(5 * time.Second):
			t.Fatalf("events %v", byRun)
		}
	}
	want := []string{LiveRunStart, LiveProviderStart, LiveStart, LiveFound, LiveChapterDone, LiveChapterDone, LiveDone, LiveProviderDone, LiveRunDone}
	if !slices.Equal(byRun["p"], want) {
		t.Errorf("events of p\n got %v\nwant %v", byRun["p"], want)
	}
}

func TestCheckCancelled(t *testing.T) {
	e := newEnv(t)
	m := &fakeManga{info: mangadata.MangaInfo{ID: "m", Title: "M"}}
	e.source("p", m).chs["m"] = m.chapters(1)
	w, _ := e.svc.Add(context.Background(), AddOptions{Provider: "p", MangaID: "m"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := e.svc.Check(ctx, nil, CheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r := report.Results[0]; r.Status != storage.StatusSkipped || report.Watches != 0 {
		t.Errorf("cancelled: %+v", report)
	}
	if w, _ := e.svc.Get(context.Background(), w.ID); !w.LastChecked.IsZero() {
		t.Errorf("a skipped watch counts as checked: %+v", w)
	}
}

func TestDueAndPaused(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.svc.Options.Jitter = 10 * time.Minute
	m1 := &fakeManga{info: mangadata.MangaInfo{ID: "1", Title: "One"}}
	m2 := &fakeManga{info: mangadata.MangaInfo{ID: "2", Title: "Two"}}
	src := e.source("p", m1, m2)
	src.chs["1"], src.chs["2"] = m1.chapters(1), m2.chapters(1)
	w1, _ := e.svc.Add(ctx, AddOptions{Provider: "p", MangaID: "1"})
	w2, _ := e.svc.Add(ctx, AddOptions{Provider: "p", MangaID: "2"})
	if due, _ := e.svc.Due(ctx, e.now); len(due) != 2 {
		t.Errorf("new watches are due: %+v", due)
	}
	if _, err := e.svc.SetEnabled(ctx, w2.ID, false); err != nil {
		t.Fatal(err)
	}
	report, _ := e.svc.Check(ctx, nil, CheckOptions{})
	if len(report.Results) != 1 || report.Results[0].Watch != w1.ID {
		t.Errorf("checked the paused one: %+v", report.Results)
	}
	w1, _ = e.svc.Get(ctx, w1.ID)
	if d := w1.NextCheck.Sub(w1.LastChecked); d < 4*time.Hour || d > 4*time.Hour+10*time.Minute {
		t.Errorf("next check %s after", d)
	}
	if due, _ := e.svc.Due(ctx, e.now.Add(4*time.Hour+11*time.Minute)); len(due) != 1 || due[0].ID != w1.ID {
		t.Errorf("due %+v", due)
	}
	interval := time.Hour
	if w, _ := e.svc.Update(ctx, w1.ID, UpdateOptions{Interval: &interval}); w.NextCheck.Sub(w.LastChecked) > time.Hour+10*time.Minute {
		t.Errorf("interval override: %+v", w)
	}
	if stale, _ := e.svc.Stale(ctx, time.Hour); len(stale) != 0 {
		t.Errorf("stale %+v", stale)
	}
	e.now = e.now.Add(48 * time.Hour)
	if stale, _ := e.svc.Stale(ctx, 24*time.Hour); len(stale) != 1 || stale[0].ID != w1.ID {
		t.Errorf("stale %+v", stale)
	}
}

func TestOfflineAndMissingProvider(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	m := &fakeManga{info: mangadata.MangaInfo{ID: "dandadan", Title: "Dandadan"}}
	src := e.source("mango-mangaplus", m)
	src.chs["dandadan"] = m.chapters(1, 2)
	e.svc.Installed = func() (func(string) bool, error) {
		return func(p string) bool { _, ok := e.sources[p]; return ok }, nil
	}

	// a provider that's gone: added as given, paused, nothing asked
	gone, err := e.svc.Add(ctx, AddOptions{Offline: true, Provider: "mango-mangasee", Query: "mid:Gachiakuta", Title: "Gachiakuta", AnilistID: 144946})
	if err != nil {
		t.Fatal(err)
	}
	if gone.Enabled || gone.MangaID != "Gachiakuta" || gone.Title != "Gachiakuta" || !gone.ProviderMissing || gone.AnilistID != 144946 {
		t.Fatalf("offline watch %+v", gone)
	}
	if _, err := e.svc.Add(ctx, AddOptions{Offline: true, Provider: "mango-mangasee", MangaID: "Gachiakuta"}); !errors.Is(err, ErrExists) {
		t.Errorf("added twice: %v", err)
	}

	// enabled, its provider still missing: not due, left out of the checks, no error
	enabled := true
	if _, err := e.svc.SetEnabled(ctx, gone.ID, enabled); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangaplus", MangaID: "dandadan"}); err != nil {
		t.Fatal(err)
	}
	due, err := e.svc.Due(ctx, e.now)
	if err != nil || len(due) != 1 || due[0].Provider != "mango-mangaplus" {
		t.Fatalf("due %v, %v", due, err)
	}
	report, err := e.svc.Check(ctx, nil, CheckOptions{})
	if err != nil || len(report.Results) != 1 || report.Errors != 0 {
		t.Fatalf("check all %+v, %v", report, err)
	}

	// asked for explicitly: skipped, not a failure, no notification
	report, err = e.svc.Check(ctx, []int64{gone.ID}, CheckOptions{})
	if err != nil || len(report.Results) != 1 || report.Results[0].Status != storage.StatusSkipped || report.Errors != 0 {
		t.Fatalf("check the missing one %+v, %v", report, err)
	}
	if got, _ := e.svc.Get(ctx, gone.ID); got.Failures != 0 {
		t.Errorf("failures %d", got.Failures)
	}
	if len(e.notifier.errs) != 0 {
		t.Errorf("notified %v", e.notifier.errs)
	}
}
