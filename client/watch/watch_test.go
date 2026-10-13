package watch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cron"
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
	// onDownload, when set, is called before each download
	onDownload func(ctx context.Context)
	// status, when set, is reported by Chapters
	status string

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
	if s.status != "" {
		progress.Report(ctx, progress.Status{Message: s.status})
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
	if s.onDownload != nil {
		s.onDownload(ctx)
	}
	results := client.NewResults(chapters)
	for i, r := range results {
		if ctx.Err() != nil {
			// as client.Download: the rest isn't got to
			break
		}
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

// tick is the nth tick of the default schedule after t.
func (e *env) tick(t time.Time, n int) time.Time {
	for range n {
		t = e.svc.Options.Schedule.Next(t)
	}
	return t
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
		Options: Options{Schedule: cron.MustParse("@every 4h"), ParallelProviders: 2, Backoff: 3, Notify: NotifyBoth, LockStale: time.Minute},
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
		picked, unavailable, _ := Pick(all, have, last, from, gaps, nil, nil)
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

	// the skip list wins over gaps and retry; only what would have been picked is skipped
	picked, _, skipped := Pick(all, have, f32(4), nil, true, map[float32]bool{2: true}, map[float32]bool{2: true, 4.5: true, 3: true, 6: true})
	var got []float32
	for _, c := range picked {
		got = append(got, c.Info().Number)
	}
	if !slices.Equal(got, []float32{5, 7}) || !slices.Equal(skipped, []float32{2, 4.5, 6}) {
		t.Errorf("skip: got %v, skipped %v", got, skipped)
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
	if w.Title != "Dandadan" || w.LibraryManga == 0 || w.LastDownloaded == nil || *w.LastDownloaded != 65 || !w.NextCheck.Equal(e.tick(e.now, 1)) {
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
	src.status = "listing"
	report, err = e.svc.Check(ctx, nil, CheckOptions{OnEvent: func(ev Event) {
		events = append(events, ev.Kind)
		if ev.Kind == EventStatus && ev.Status.Message != "listing" {
			t.Errorf("status %q", ev.Status)
		}
	}})
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
	if !slices.Equal(events, []EventKind{EventStart, EventStatus, EventFound, EventChapter, EventChapter, EventDone}) {
		t.Errorf("events %v", events)
	}
	// one notification for the manga
	if len(e.notifier.news) != 1 || len(e.notifier.errs) != 0 || *e.notifier.news[0].Newest() != 66 || *e.notifier.news[0].From != 65 {
		t.Errorf("notified %+v / %+v", e.notifier.news, e.notifier.errs)
	}
	w, _ = e.svc.Get(ctx, w.ID)
	if *w.LastNew != 66 || !w.LastNewAt.Equal(e.now) || !w.LastChecked.Equal(e.now) || *w.LastDownloaded != 66 || !w.NextCheck.Equal(e.tick(e.now, 1)) {
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

func TestCheckSkip(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	m := &fakeManga{info: mangadata.MangaInfo{ID: "dandadan", Title: "Dandadan"}}
	src := e.source("mango-mangaplus", m)
	src.chs["dandadan"] = m.chapters(10, 64, 65, 65.5, 66)
	e.own(t, "mango-mangaplus", m, 64, 65)
	w, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangaplus", MangaID: "dandadan"})
	if err != nil {
		t.Fatal(err)
	}
	skip := []float32{65.5, 10, 65.5}
	if w, err = e.svc.Update(ctx, w.ID, UpdateOptions{Skip: &skip}); err != nil || !slices.Equal(w.Skip, []float32{10, 65.5}) {
		t.Fatalf("update %+v, %v", w.Skip, err)
	}
	report, err := e.svc.Check(ctx, nil, CheckOptions{Gaps: true})
	if err != nil {
		t.Fatal(err)
	}
	r := report.Results[0]
	if !slices.Equal(src.downloaded, []float32{66}) || !slices.Equal(r.Skipped, []float32{10, 65.5}) {
		t.Errorf("downloaded %v, skipped %v", src.downloaded, r.Skipped)
	}
	if d := Describe(r, false); !strings.Contains(d, "skipped 10, 65.5 (skip list)") {
		t.Errorf("describe %q", d)
	}
	runs, _ := e.svc.Runs(ctx, RunFilter{Watch: w.ID})
	if len(runs) != 1 || !slices.Equal(runs[0].Results[0].Skipped, []float32{10, 65.5}) {
		t.Errorf("runs %+v", runs)
	}
	// the list survives the check's save
	if w, _ = e.svc.Get(ctx, w.ID); !slices.Equal(w.Skip, []float32{10, 65.5}) {
		t.Errorf("skip after the check %v", w.Skip)
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

	// backoff: after 3 failures in a row it skips ticks, 4 failures = 4 ticks
	for range 3 {
		e.now = e.now.Add(time.Hour)
		_, _ = e.svc.Check(ctx, []int64{ids[1]}, CheckOptions{})
	}
	w, _ := e.svc.Get(ctx, ids[1])
	if w.Failures != 4 || !w.NextCheck.Equal(e.tick(w.LastChecked, 4)) {
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
	// new watches wait for their first tick (+ jitter)
	if due, _ := e.svc.Due(ctx, e.now); len(due) != 0 {
		t.Errorf("new watches due before their tick: %+v", due)
	}
	if due, _ := e.svc.Due(ctx, e.tick(e.now, 1).Add(11*time.Minute)); len(due) != 2 {
		t.Errorf("new watches due at their tick: %+v", due)
	}
	if _, err := e.svc.SetEnabled(ctx, w2.ID, false); err != nil {
		t.Fatal(err)
	}
	report, _ := e.svc.Check(ctx, nil, CheckOptions{})
	if len(report.Results) != 1 || report.Results[0].Watch != w1.ID {
		t.Errorf("checked the paused one: %+v", report.Results)
	}
	w1, _ = e.svc.Get(ctx, w1.ID)
	tick := e.tick(w1.LastChecked, 1)
	if d := w1.NextCheck.Sub(tick); d < 0 || d > 10*time.Minute {
		t.Errorf("next check %s after the tick", d)
	}
	if due, _ := e.svc.Due(ctx, tick.Add(-time.Second)); len(due) != 0 {
		t.Errorf("due before the tick: %+v", due)
	}
	if due, _ := e.svc.Due(ctx, tick.Add(11*time.Minute)); len(due) != 1 || due[0].ID != w1.ID {
		t.Errorf("due %+v", due)
	}
	every := "*/30 * * * *"
	if w, _ := e.svc.Update(ctx, w1.ID, UpdateOptions{Schedule: &every}); w.Schedule != every || w.NextCheck.Sub(w.LastChecked) > 40*time.Minute {
		t.Errorf("schedule override: %+v", w)
	}
	bad := "*/30 * *"
	if _, err := e.svc.Update(ctx, w1.ID, UpdateOptions{Schedule: &bad}); err == nil {
		t.Error("saved an invalid schedule")
	}
	if _, err := e.svc.Add(ctx, AddOptions{Provider: "p", MangaID: "1", Schedule: "@sometimes"}); err == nil || errors.Is(err, ErrExists) {
		t.Errorf("added with an invalid schedule: %v", err)
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
	due, err := e.svc.Due(ctx, e.tick(e.now, 1))
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

// TestCheckAndDownload: a check and a download of a provider share its lock: a check
// skips a provider a download has, its own downloads don't wait for the lock it holds.
func TestCheckAndDownload(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	m := &fakeManga{info: mangadata.MangaInfo{ID: "m", Title: "M"}}
	src := e.source("p", m)
	src.chs["m"] = m.chapters(1, 2)
	w, err := e.svc.Add(ctx, AddOptions{Provider: "p", MangaID: "m"})
	if err != nil {
		t.Fatal(err)
	}

	// a TUI download of the provider has it
	_, unlock, err := client.LockProvider(ctx, e.svc.Watches, "p", client.LockForDownload, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Check(ctx, []int64{w.ID}, CheckOptions{}); !errors.Is(err, ErrBusy) {
		t.Errorf("check while downloading: %v", err)
	}
	if st, err := e.svc.Status(ctx); err != nil || st.Checking {
		t.Errorf("a download is no check: %+v, %v", st, err)
	}
	unlock()

	src.onDownload = func(ctx context.Context) {
		if l, err := e.svc.Watches.Lock(lockName("p"), time.Minute); err == nil {
			_ = l.Unlock()
			t.Error("the check doesn't hold the provider's lock")
		}
		held := make(chan struct{})
		go func() {
			defer close(held)
			if _, unlock, err := client.LockProvider(ctx, e.svc.Watches, "p", client.LockForDownload, nil); err == nil {
				unlock()
			}
		}()
		select {
		case <-held:
		case <-time.After(5 * time.Second):
			t.Error("the check's download waits for its own lock")
		}
	}
	report, err := e.svc.Check(ctx, []int64{w.ID}, CheckOptions{})
	if err != nil || report.Results[0].Status != storage.StatusNew {
		t.Fatalf("check %+v, %v", report, err)
	}
	if l, err := e.svc.Watches.Lock(lockName("p"), time.Minute); err != nil {
		t.Errorf("lock kept: %v", err)
	} else {
		_ = l.Unlock()
	}
}

func TestNextCheckTicks(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip("no tz database:", err)
	}
	at := func(m time.Month, d, h, min, sec int) time.Time { return time.Date(2026, m, d, h, min, sec, 0, loc) }
	var warned []error
	svc := &Service{Options: Options{Backoff: 3}, OnError: func(err error) { warned = append(warned, err) }}
	w := storage.Watch{ID: 1, Provider: "p", Added: at(10, 1, 9, 0, 0)}

	if next := svc.NextCheck(w); !next.Equal(at(10, 1, 12, 0, 0)) {
		t.Errorf("never checked: next %s, want the first tick after it was added", next)
	}
	// the default schedule: every 4 hours on the hour, whenever the last check was
	for _, c := range []struct{ last, want time.Time }{
		{at(10, 11, 15, 59, 0), at(10, 11, 16, 0, 0)},
		{at(10, 11, 16, 0, 0), at(10, 11, 20, 0, 0)},
		{at(10, 11, 16, 0, 30), at(10, 11, 20, 0, 0)},
		{at(10, 11, 21, 7, 0), at(10, 12, 0, 0, 0)},
		// 02:00 CST → 03:00 CDT: still 04:00 on the wall clock, 2h30m later
		{at(3, 8, 0, 30, 0), at(3, 8, 4, 0, 0)},
		// 02:00 CDT → 01:00 CST: 04:00 on the wall clock, 4h30m later
		{at(11, 1, 0, 30, 0), at(11, 1, 4, 0, 0)},
	} {
		w.LastChecked = c.last
		if next := svc.NextCheck(w); !next.Equal(c.want) || next.Hour() != c.want.Hour() {
			t.Errorf("checked %s: next %s, want %s", c.last, next, c.want)
		}
	}

	// a manual check between ticks: due again at the next tick, not 4h later
	w.LastChecked = at(10, 11, 17, 30, 0)
	if next := svc.NextCheck(w); !next.Equal(at(10, 11, 20, 0, 0)) {
		t.Errorf("manual check: next %s", next)
	}

	// backoff skips ticks: 3 failures = 2 ticks, 4 = 4, 5 and more = 8
	w.LastChecked = at(10, 11, 15, 59, 0)
	for failures, want := range map[int]time.Time{
		2:  at(10, 11, 16, 0, 0),
		3:  at(10, 11, 20, 0, 0),
		4:  at(10, 12, 4, 0, 0),
		5:  at(10, 12, 20, 0, 0),
		10: at(10, 12, 20, 0, 0),
	} {
		w.Failures = failures
		if next := svc.NextCheck(w); !next.Equal(want) {
			t.Errorf("%d failures: next %s, want %s", failures, next, want)
		}
	}
	w.Failures = 0

	// its own schedule, an invalid one falls back (with a warning)
	w.Schedule = "@daily"
	if next := svc.NextCheck(w); !next.Equal(at(10, 12, 0, 0, 0)) {
		t.Errorf("own schedule: next %s", next)
	}
	w.Schedule = "nope"
	if next := svc.NextCheck(w); !next.Equal(at(10, 11, 16, 0, 0)) || len(warned) != 1 {
		t.Errorf("invalid own schedule: next %s, warned %v", next, warned)
	}
	w.Schedule = ""

	// jitter: the same for a provider's watches at a tick, at most watch.jitter
	svc.Options.Jitter = 5 * time.Minute
	other := w
	other.ID = 2
	next := svc.NextCheck(w)
	if d := next.Sub(at(10, 11, 16, 0, 0)); d < 0 || d > 5*time.Minute || !svc.NextCheck(other).Equal(next) {
		t.Errorf("jitter: next %s, other %s", next, svc.NextCheck(other))
	}
	svc.Options.Jitter = 0

	// gap filling: off without watch.gap_schedule or the watch's gaps
	if !svc.NextGapCheck(w).IsZero() || svc.GapsDue(w, at(12, 1, 0, 0, 0)) {
		t.Error("gaps due without a gap schedule")
	}
	svc.Options.GapSchedule = cron.MustParse("0 3 * * 0")
	if !svc.NextGapCheck(w).IsZero() {
		t.Error("gaps due on a watch without gap filling")
	}
	w.Gaps = true
	if first := at(10, 4, 3, 0, 0); svc.GapsDue(w, w.Added) || !svc.GapsDue(w, first) {
		t.Error("gaps never filled: due at the first gap tick after the watch was added")
	}
	w.LastGapCheck = at(10, 11, 3, 0, 5)
	if next := svc.NextGapCheck(w); !next.Equal(at(10, 18, 3, 0, 0)) || svc.GapsDue(w, at(10, 18, 2, 59, 0)) || !svc.GapsDue(w, next) {
		t.Errorf("gaps: next %s", next)
	}
}

func TestRelink(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	old := &fakeManga{info: mangadata.MangaInfo{ID: "berserk", Title: "Berserk"}}
	m := &fakeManga{info: mangadata.MangaInfo{ID: "01J76XYBERSERK", Title: "Berserk"}}
	vagabond := &fakeManga{info: mangadata.MangaInfo{ID: "01J76XYVAGABOND", Title: "Vagabond"}}
	src := e.source("mango-weebcentral", m, vagabond)
	src.chs[m.info.ID] = m.chapters(370, 371, 372)
	pill := e.source("mango-mangapill", &fakeManga{info: mangadata.MangaInfo{ID: "1/berserk", Title: "Berserk"}})
	pill.noByID = true
	e.own(t, "mango-mangasee", old, 369, 370)

	// a watch moved from mangasee by mangal migrate: its id is mangasee's slug
	w, err := e.svc.Add(ctx, AddOptions{Offline: true, Provider: "mango-weebcentral", MangaID: "berserk", Title: "Berserk", Enabled: new(true), AnilistID: 30002})
	if err != nil {
		t.Fatal(err)
	}
	other, _ := e.svc.Add(ctx, AddOptions{Provider: "mango-weebcentral", Query: "Vagabond"})
	if _, err := e.svc.Watches.DB.Exec(`UPDATE watches SET relink = 'moved from mango-mangasee', library_manga = (SELECT manga FROM library_sources) WHERE id = ?`, w.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.svc.Get(ctx, w.ID); !got.NeedsRelink || got.Scheduled() {
		t.Fatalf("not flagged: %+v", got)
	}

	// the scheduler and a check of every watch skip it, a check of it says not found
	e.now = e.tick(e.now, 2)
	if due, _ := e.svc.Due(ctx, e.now); len(due) != 1 || due[0].ID != other.ID {
		t.Errorf("due %+v", due)
	}
	report, err := e.svc.Check(ctx, []int64{w.ID}, CheckOptions{})
	if err != nil || len(report.Results) != 1 || report.Results[0].Relink == "" {
		t.Fatalf("check %+v, %v", report, err)
	}
	got, _ := e.svc.Get(ctx, w.ID)
	if !got.NeedsRelink || got.Failures != 0 || !strings.Contains(got.RelinkReason, "not found") || len(e.notifier.errs) != 0 {
		t.Errorf("after not found: %+v, %d error notifications", got.Watch, len(e.notifier.errs))
	}

	if _, err := e.svc.Relink(ctx, w.ID, RelinkOptions{Query: "Vagabond"}); !errors.Is(err, ErrExists) {
		t.Errorf("re-linked to another watch's manga: %v", err)
	}
	got, err = e.svc.Relink(ctx, w.ID, RelinkOptions{Query: "Berserk"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != w.ID || got.MangaID != m.info.ID || got.NeedsRelink || got.AnilistID != 30002 || got.LibraryManga == 0 ||
		got.LastDownloaded == nil || *got.LastDownloaded != 370 {
		t.Errorf("re-linked %+v", got)
	}
	report, _ = e.svc.Check(ctx, []int64{w.ID}, CheckOptions{})
	if len(report.Results) != 1 || !slices.Equal(numbers(report.Results[0].Chapters), []float32{371, 372}) {
		t.Errorf("check after the re-link %+v", report.Results)
	}

	// another provider, found by a search of the title; mid: on a provider without a lookup by id
	if got, err = e.svc.Relink(ctx, w.ID, RelinkOptions{Provider: "mango-mangapill", Query: "Berserk"}); err != nil || got.Provider != "mango-mangapill" ||
		got.MangaID != "1/berserk" || got.AnilistID != 30002 {
		t.Errorf("to mangapill: %+v, %v", got, err)
	}
	if _, err = e.svc.Relink(ctx, w.ID, RelinkOptions{Provider: "mango-weebcentral", MangaID: "nope"}); err == nil {
		t.Error("re-linked to a missing manga")
	}

	// a check that finds the manga clears the flag
	_, _ = e.svc.Watches.DB.Exec(`UPDATE watches SET relink = 'moved' WHERE id = ?`, other.ID)
	_, _ = e.svc.Check(ctx, []int64{other.ID}, CheckOptions{})
	if got, _ := e.svc.Get(ctx, other.ID); got.NeedsRelink {
		t.Errorf("still flagged after a good check: %+v", got.Watch)
	}

	// placeholders need one from the start
	p, _ := e.svc.Add(ctx, AddOptions{Offline: true, Provider: "mango-weebcentral", Query: "mid:title:Vinland Saga", Title: "Vinland Saga"})
	if !p.NeedsRelink {
		t.Errorf("placeholder %+v", p.Watch)
	}
}

func TestNextScheduled(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	m := &fakeManga{info: mangadata.MangaInfo{ID: "dandadan", Title: "Dandadan"}}
	e.source("mango-mangaplus", m)
	added := e.now
	if _, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangaplus", MangaID: "dandadan"}); err != nil {
		t.Fatal(err)
	}

	// ticks missed while nothing ran: due for a manual check, the scheduler waits
	e.now = e.tick(added, 3).Add(time.Minute)
	w, err := e.svc.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !w.NextCheck.Equal(e.tick(added, 1)) || !w.NextScheduled.Equal(e.tick(added, 4)) {
		t.Errorf("no server: next check %s, scheduled %s", w.NextCheck, w.NextScheduled)
	}

	// a running server counts from when it started
	lock, err := e.svc.Watches.Lock(ServeLock, ServeLockStale)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Unlock() }()
	all, err := e.svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := e.svc.Options.Schedule.Next(lock.Acquired); len(all) != 1 || !all[0].NextScheduled.Equal(want) {
		t.Errorf("server running: scheduled %v, want %s", all, want)
	}
	if st, err := e.svc.Status(ctx); err != nil || !st.NextCheck.Equal(e.svc.Options.Schedule.Next(lock.Acquired)) {
		t.Errorf("status next check %s (%v)", st.NextCheck, err)
	}
}

func TestCheckRetriesFailed(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.svc.Options.Notify = NotifyNone
	m := &fakeManga{info: mangadata.MangaInfo{ID: "do", Title: "Dungeon Odyssey"}}
	src := e.source("mango-asurascans", m)
	src.chs["do"] = m.chapters(1, 2, 3, 4, 5)
	src.fail = map[float32]bool{2: true, 3: true}
	w, err := e.svc.Add(ctx, AddOptions{Provider: "mango-asurascans", MangaID: "do"})
	if err != nil {
		t.Fatal(err)
	}

	report, err := e.svc.Check(ctx, []int64{w.ID}, CheckOptions{})
	if err != nil || report.Results[0].Failed != 2 {
		t.Fatalf("first check: %+v, %v", report.Results, err)
	}
	if w, _ = e.svc.Get(ctx, w.ID); !slices.Equal(w.Retry, []float32{2, 3}) {
		t.Fatalf("retry %v", w.Retry)
	}

	// below the last downloaded (5), without gaps: tried again, 3 still fails
	src.fail = map[float32]bool{3: true}
	report, _ = e.svc.Check(ctx, []int64{w.ID}, CheckOptions{})
	if got := numbers(report.Results[0].Chapters); !slices.Equal(got, []float32{2}) {
		t.Errorf("second check got %v", got)
	}
	if w, _ = e.svc.Get(ctx, w.ID); !slices.Equal(w.Retry, []float32{3}) {
		t.Errorf("retry %v", w.Retry)
	}

	src.fail = nil
	e.svc.Check(ctx, []int64{w.ID}, CheckOptions{})
	if w, _ = e.svc.Get(ctx, w.ID); len(w.Retry) != 0 {
		t.Errorf("retry %v after all downloaded", w.Retry)
	}
}

// a cancelled check keeps the chapters to retry it didn't get to
func TestCancelledCheckKeepsRetries(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.svc.Options.Notify = NotifyNone
	m := &fakeManga{info: mangadata.MangaInfo{ID: "do", Title: "Dungeon Odyssey"}}
	src := e.source("mango-asurascans", m)
	src.chs["do"] = m.chapters(1, 2, 3, 4, 5)
	src.fail = map[float32]bool{2: true, 3: true}
	w, err := e.svc.Add(ctx, AddOptions{Provider: "mango-asurascans", MangaID: "do"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Check(ctx, []int64{w.ID}, CheckOptions{}); err != nil {
		t.Fatal(err)
	}
	src.fail = nil
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	src.onDownload = func(context.Context) { cancel() }
	if _, err := e.svc.Check(cctx, []int64{w.ID}, CheckOptions{}); err != nil {
		t.Fatal(err)
	}
	if w, _ = e.svc.Get(ctx, w.ID); !slices.Equal(w.Retry, []float32{2, 3}) {
		t.Errorf("retry %v after a cancelled check", w.Retry)
	}
}

// pausing a watch or editing its skip list while it's checked survives the check
func TestCheckKeepsEditsMadeMeanwhile(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.svc.Options.Notify = NotifyNone
	m := &fakeManga{info: mangadata.MangaInfo{ID: "do", Title: "Dungeon Odyssey"}}
	src := e.source("mango-asurascans", m)
	src.chs["do"] = m.chapters(1, 2)
	w, err := e.svc.Add(ctx, AddOptions{Provider: "mango-asurascans", MangaID: "do"})
	if err != nil {
		t.Fatal(err)
	}
	src.onDownload = func(context.Context) {
		if _, err := e.svc.SetEnabled(ctx, w.ID, false); err != nil {
			t.Error(err)
		}
		skip := []float32{7}
		if _, err := e.svc.Update(ctx, w.ID, UpdateOptions{Skip: &skip}); err != nil {
			t.Error(err)
		}
	}
	if _, err := e.svc.Check(ctx, []int64{w.ID}, CheckOptions{}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.Watches.Get(w.ID)
	if got.Enabled || !slices.Equal(got.Skip, []float32{7}) || got.LastChecked.IsZero() {
		t.Errorf("after the check: %+v", got)
	}
}

func TestResumeWaitsForItsTick(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	m := &fakeManga{info: mangadata.MangaInfo{ID: "dandadan", Title: "Dandadan"}}
	e.source("mango-mangaplus", m)
	added := e.now
	w, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangaplus", MangaID: "dandadan"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SetEnabled(ctx, w.ID, false); err != nil {
		t.Fatal(err)
	}

	// paused through 3 ticks: resumed, it waits for the next one
	e.now = e.tick(added, 3).Add(time.Minute)
	if w, err = e.svc.SetEnabled(ctx, w.ID, true); err != nil {
		t.Fatal(err)
	}
	if !w.Resumed.Equal(e.now) || !w.NextCheck.Equal(e.tick(added, 4)) {
		t.Errorf("resumed %s: next check %s, want %s", w.Resumed, w.NextCheck, e.tick(added, 4))
	}
	if due, _ := e.svc.Due(ctx, e.now); len(due) != 0 {
		t.Errorf("due right after resuming: %v", due)
	}
	// resuming a watch that runs doesn't move it
	e.now = e.now.Add(time.Minute)
	if w, _ = e.svc.SetEnabled(ctx, w.ID, true); !w.Resumed.Equal(e.now.Add(-time.Minute)) {
		t.Errorf("resumed again: %s", w.Resumed)
	}
}

// TestLibraryLink: a watch finds its library manga by its bindings; one in another
// provider's directory is an issue until it's moved there.
func TestLibraryLink(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.svc.Options.ProviderDirs = true
	al := metadata.ID{Raw: "98522", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	if err := e.svc.Library.RememberMangaDir("/lib/Weeb Central", "Teisou Gyakuten Sekai", libmangal.MangaDirInfo{Provider: "mango-weebcentral",
		Manga: mangadata.MangaInfo{ID: "01J", Title: "Teisou Gyakuten Sekai"}, MetadataIDs: []metadata.ID{al}}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Library.RecordDownload("/lib/Weeb Central", "Teisou Gyakuten Sekai", storage.Download{Path: "/lib/Weeb Central/Teisou Gyakuten Sekai/5.cbz",
		Provider: "mango-weebcentral", MangaID: "01J", MangaTitle: "Teisou Gyakuten Sekai", Chapter: 5, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	lib, _, _ := e.svc.Library.MangaBySource("mango-weebcentral", "01J")
	m := &fakeManga{info: mangadata.MangaInfo{ID: "f6", Title: "Chastity Reverse World"}}
	// nothing new: the fake download would make a folder of its own
	e.source("mango-mangadex", m).chs["f6"] = m.chapters(5)

	w, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangadex", MangaID: "f6", AnilistID: 98522})
	if err != nil {
		t.Fatal(err)
	}
	if w.LibraryManga != 0 || !strings.Contains(w.LibraryIssue, "in the library under Weeb Central (Teisou Gyakuten Sekai): move it there") {
		t.Fatalf("added %d, issue %q", w.LibraryManga, w.LibraryIssue)
	}
	list, _ := e.svc.List(ctx)
	if len(list) != 1 || list[0].LibraryIssue != w.LibraryIssue {
		t.Errorf("list issue %+v", list)
	}
	if list[0].Scheduled() || !list[0].LibraryElsewhere {
		t.Error("an elsewhere watch is scheduled")
	}
	// not checked: neither due, nor in a check of every watch, nor by its id
	e.now = e.now.Add(48 * time.Hour)
	if due, _ := e.svc.Due(ctx, e.now); len(due) != 0 {
		t.Errorf("due %+v", due)
	}
	if report, err := e.svc.Check(ctx, nil, CheckOptions{}); err != nil || len(report.Results) != 0 {
		t.Errorf("check all: %+v, %v", report, err)
	}
	report, err := e.svc.Check(ctx, []int64{w.ID}, CheckOptions{})
	if err != nil || len(report.Results) != 1 || report.Results[0].Status != storage.StatusSkipped ||
		!strings.Contains(report.Results[0].Error, fmt.Sprintf("move it first (X in the library, or mangal library move %d mango-mangadex)", lib.ID)) {
		t.Fatalf("check by id: %+v, %v", report.Results, err)
	}
	if got := e.sources["mango-mangadex"].downloaded; len(got) != 0 {
		t.Errorf("downloaded %v", got)
	}

	// moved: its source now, no issue, linked by the next check
	if err := e.svc.Library.Move(storage.MangaMove{Manga: lib.ID, Source: storage.Source{Provider: "mango-mangadex", MangaID: "f6", Title: m.info.Title},
		From: lib.Path(), To: "/lib/mango-mangadex/Teisou Gyakuten Sekai", At: e.now}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.Get(ctx, w.ID)
	if got.LibraryIssue != "" || got.LastDownloaded == nil || *got.LastDownloaded != 5 {
		t.Errorf("after the move: issue %q, last %v", got.LibraryIssue, got.LastDownloaded)
	}
	if _, err := e.svc.Check(ctx, []int64{w.ID}, CheckOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.svc.Watches.Get(w.ID); got.LibraryManga != lib.ID {
		t.Errorf("not linked by the check: %d", got.LibraryManga)
	}

	// without provider directories the binding links it right away
	e2 := newEnv(t)
	_ = e2.svc.Library.RememberMangaDir("/lib/Weeb Central", "Teisou Gyakuten Sekai", libmangal.MangaDirInfo{Provider: "mango-weebcentral",
		Manga: mangadata.MangaInfo{ID: "01J"}, MetadataIDs: []metadata.ID{al}})
	e2.source("mango-mangadex", m)
	w2, err := e2.svc.Add(ctx, AddOptions{Provider: "mango-mangadex", MangaID: "f6", AnilistID: 98522})
	if err != nil || w2.LibraryManga == 0 || w2.LibraryIssue != "" {
		t.Fatalf("no provider dirs: %d %q, %v", w2.LibraryManga, w2.LibraryIssue, err)
	}
	// linked: its manga is a source of the library manga now (it had one of another provider)
	if m, _, _ := e2.svc.Library.MangaByID(w2.LibraryManga); len(m.Sources) != 2 ||
		!slices.ContainsFunc(m.Sources, func(src storage.Source) bool { return src.Provider == "mango-mangadex" && src.MangaID == "f6" }) {
		t.Errorf("sources %+v", m.Sources)
	}
}

func TestLibraryIssue(t *testing.T) {
	elsewhere := storage.LibraryMatch{Manga: storage.Manga{Title: "Berserk", Parent: "/dl/Weeb Central", Dir: "Berserk"}, Elsewhere: true}
	for _, c := range []struct {
		match      storage.LibraryMatch
		downloaded bool
		want       string
	}{
		{storage.LibraryMatch{Linked: true}, true, ""},
		{elsewhere, false, "in the library under Weeb Central (Berserk): move it there (X in the library)"},
		{storage.LibraryMatch{}, true, "its downloads aren't in the library (library scan?)"},
		{storage.LibraryMatch{}, false, ""},
	} {
		if got := libraryIssue(c.match, c.downloaded); got != c.want {
			t.Errorf("%+v, %t: %q, want %q", c.match, c.downloaded, got, c.want)
		}
	}
}

// TestDownloadsNotInLibrary: a watch whose downloads aren't in the library isn't
// scheduled (it would download every chapter again), an explicit check still runs.
func TestDownloadsNotInLibrary(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	m := &fakeManga{info: mangadata.MangaInfo{ID: "m", Title: "M"}}
	e.source("p", m).chs["m"] = m.chapters(1)
	w, err := e.svc.Add(ctx, AddOptions{Provider: "p", MangaID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	run, _ := e.svc.Watches.StartRun(storage.CheckRun{Kind: KindManual, Started: e.now})
	if _, err := e.svc.Watches.RecordWatchRun(storage.WatchRun{CheckRun: run.ID, Watch: w.ID, Provider: "p", MangaID: "m", Status: storage.StatusNew,
		Started: e.now, Finished: e.now, Chapters: []storage.RunChapter{{Number: 1, Path: "/gone/1.cbz"}}}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.Get(ctx, w.ID)
	if got.LibraryIssue != "its downloads aren't in the library (library scan?)" || got.LibraryElsewhere || got.Scheduled() {
		t.Fatalf("watch %+v", got)
	}
	if report, _ := e.svc.Check(ctx, nil, CheckOptions{}); len(report.Results) != 0 {
		t.Errorf("checked with the others: %+v", report.Results)
	}
	if report, err := e.svc.Check(ctx, []int64{w.ID}, CheckOptions{}); err != nil || report.Results[0].Status == storage.StatusSkipped {
		t.Errorf("explicit check: %+v, %v", report.Results, err)
	}
}

// a folder adopted by scan (no source, named after the AniList title) is linked to the
// watch by its AniList binding when it's added; listing only shows a link it'd make
func TestListLinksAdoptedFolder(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.svc.Options.ProviderDirs = true
	al := metadata.ID{Raw: "98522", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	if err := e.svc.Library.RememberMangaDir("/lib/mango-mangadex", "Teisou Gyakuten Sekai (2018) [alid-98522]",
		libmangal.MangaDirInfo{Manga: mangadata.MangaInfo{Title: "Teisou Gyakuten Sekai"}, MetadataIDs: []metadata.ID{al}}); err != nil {
		t.Fatal(err)
	}
	w, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangadex", MangaID: "f6333441", Title: "Chastity Reverse World", Offline: true, AnilistID: 98522})
	if err != nil {
		t.Fatal(err)
	}
	adopted, _, _ := e.svc.Library.MangaByFolder("/lib/mango-mangadex", "Teisou Gyakuten Sekai (2018) [alid-98522]")
	_ = e.svc.Library.SetStatuses(map[int64]string{adopted.ID: string(metadata.StatusHiatus)}, e.now)
	other, err := e.svc.Add(ctx, AddOptions{Provider: "mango-mangadex", MangaID: "x", Title: "Nope", Offline: true, AnilistID: 1})
	if err != nil {
		t.Fatal(err)
	}

	all, err := e.svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if all[0].LibraryManga == 0 || all[0].LibraryProblem() != "" {
		t.Fatalf("not linked: %+v", all[0])
	}
	if all[0].Status != "HIATUS" || all[1].Status != "" {
		t.Errorf("statuses %q %q, want the library manga's", all[0].Status, all[1].Status)
	}
	if stored, _ := e.svc.Watches.Get(w.ID); stored.LibraryManga != all[0].LibraryManga {
		t.Errorf("link not saved: %d", stored.LibraryManga)
	}
	if m, found, _ := e.svc.Library.MangaBySource("mango-mangadex", "f6333441"); !found || m.ID != all[0].LibraryManga {
		t.Errorf("source not added: %+v %v", m, found)
	}
	// adopted after the watch was added: shown linked, nothing written
	later := metadata.ID{Raw: "1", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	if err := e.svc.Library.RememberMangaDir("/lib/mango-mangadex", "Nope [alid-1]",
		libmangal.MangaDirInfo{Manga: mangadata.MangaInfo{Title: "Nope"}, MetadataIDs: []metadata.ID{later}}); err != nil {
		t.Fatal(err)
	}
	if listed, err := e.svc.Get(ctx, other.ID); err != nil || listed.LibraryManga == 0 {
		t.Errorf("not shown linked: %+v %v", listed, err)
	}
	if stored, _ := e.svc.Watches.Get(other.ID); stored.LibraryManga != 0 {
		t.Errorf("listing linked it: %d", stored.LibraryManga)
	}
	if _, found, _ := e.svc.Library.MangaBySource("mango-mangadex", "x"); found {
		t.Error("listing added a source")
	}
	if err := e.svc.Library.RemoveManga(func() int64 {
		m, _, _ := e.svc.Library.MangaByFolder("/lib/mango-mangadex", "Nope [alid-1]")
		return m.ID
	}()); err != nil {
		t.Fatal(err)
	}
	all, _ = e.svc.List(ctx)
	if p := all[1].LibraryProblem(); all[1].ID != other.ID || all[1].LibraryManga != 0 || !strings.Contains(p, "al 1") || all[1].LibraryIssue != "" {
		t.Errorf("unmatched watch: %q %+v", p, all[1])
	}
	if resumed, _ := e.svc.SetEnabled(ctx, other.ID, true); !resumed.Scheduled() {
		t.Errorf("a watch with only a note isn't checked: %+v", resumed)
	}
}

func TestRemoveRun(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	a := &fakeManga{info: mangadata.MangaInfo{ID: "a", Title: "A"}}
	b := &fakeManga{info: mangadata.MangaInfo{ID: "b", Title: "B"}}
	src := e.source("mango-mangaplus", a, b)
	src.chs["a"], src.chs["b"] = a.chapters(1, 2), b.chapters(1)
	wa, _ := e.svc.Add(ctx, AddOptions{Provider: "mango-mangaplus", MangaID: "a"})
	wb, _ := e.svc.Add(ctx, AddOptions{Provider: "mango-mangaplus", MangaID: "b"})
	report, err := e.svc.Check(ctx, nil, CheckOptions{})
	if err != nil || report.Watches != 2 || report.NewChapters != 3 {
		t.Fatalf("check %+v, %v", report.CheckRun, err)
	}
	var ops []string
	e.svc.Operation = func(kind, summary string, do func() error) (string, error) {
		ops = append(ops, kind+": "+summary)
		return "/backups/snap.sqlite", do()
	}
	runA := report.Results[0].ID
	if report.Results[0].Watch != wa.ID {
		runA = report.Results[1].ID
	}
	removed, err := e.svc.RemoveRun(ctx, runA)
	if err != nil || removed.CheckRunRemoved || removed.Run.Title != "A" || removed.Snapshot != "/backups/snap.sqlite" ||
		!slices.Equal(ops, []string{fmt.Sprintf("history: remove watch run %d from the history", runA)}) {
		t.Fatalf("removed %+v, %v, ops %v", removed, err, ops)
	}
	run, err := e.svc.RunDetails(ctx, report.ID)
	if err != nil || run.Watches != 1 || run.NewChapters != 1 || len(run.Results) != 1 || run.Results[0].Watch != wb.ID {
		t.Errorf("check run after %+v, %v", run, err)
	}
	if _, err := e.svc.RemoveRun(ctx, runA); !errors.Is(err, storage.ErrRunNotFound) || ErrorCode(err) != CodeNotFound || len(ops) != 1 {
		t.Errorf("removed twice: %v (%d operations)", err, len(ops))
	}
	removed, err = e.svc.RemoveRun(ctx, run.Results[0].ID)
	if err != nil || !removed.CheckRunRemoved {
		t.Errorf("last one %+v, %v", removed, err)
	}
	if runs, _ := e.svc.Runs(ctx, RunFilter{}); len(runs) != 0 {
		t.Errorf("runs left %+v", runs)
	}

	// a check still running (it holds a provider lock)
	lock, err := e.svc.Watches.LockAs(lockName("mango-mangaplus"), client.LockForWatch, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	started, _ := e.svc.Watches.StartRun(storage.CheckRun{Kind: KindManual, Started: e.now})
	r, _ := e.svc.Watches.RecordWatchRun(storage.WatchRun{CheckRun: started.ID, Watch: wa.ID, Provider: "mango-mangaplus", MangaID: "a", Title: "A", Status: storage.StatusNone})
	if _, err := e.svc.RemoveRun(ctx, r.ID); !errors.Is(err, storage.ErrRunUnfinished) || ErrorCode(err) != CodeBusy {
		t.Errorf("running: %v", err)
	}
	// it crashed: nothing holds a lock, it never finishes
	_ = lock.Unlock()
	if removed, err := e.svc.RemoveRun(ctx, r.ID); err != nil || !removed.CheckRunRemoved {
		t.Errorf("crashed: %+v, %v", removed, err)
	}
}

// removing several runs or watches is one operation
func TestRemoveBatches(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	a := &fakeManga{info: mangadata.MangaInfo{ID: "a", Title: "A"}}
	b := &fakeManga{info: mangadata.MangaInfo{ID: "b", Title: "B"}}
	src := e.source("mango-mangaplus", a, b)
	src.chs["a"], src.chs["b"] = a.chapters(1), b.chapters(1)
	wa, _ := e.svc.Add(ctx, AddOptions{Provider: "mango-mangaplus", MangaID: "a"})
	wb, _ := e.svc.Add(ctx, AddOptions{Provider: "mango-mangaplus", MangaID: "b"})
	report, err := e.svc.Check(ctx, nil, CheckOptions{})
	if err != nil || report.Watches != 2 {
		t.Fatalf("check %+v, %v", report.CheckRun, err)
	}
	var ops []string
	e.svc.Operation = func(kind, summary string, do func() error) (string, error) {
		ops = append(ops, kind+": "+summary)
		return "/backups/snap.sqlite", do()
	}
	ids := []int64{report.Results[0].ID, report.Results[1].ID}
	// one unknown: nothing removed, no snapshot
	if _, err := e.svc.RemoveRuns(ctx, append(slices.Clone(ids), 999)); !errors.Is(err, storage.ErrRunNotFound) || len(ops) != 0 {
		t.Fatalf("unknown run: %v, ops %v", err, ops)
	}
	removed, err := e.svc.RemoveRuns(ctx, ids)
	if err != nil || len(removed) != 2 || !removed[1].CheckRunRemoved || removed[0].Snapshot == "" || removed[0].Run.ID != ids[0] || len(ops) != 1 {
		t.Fatalf("removed %+v, %v, ops %v", removed, err, ops)
	}
	if _, err := e.svc.RemoveWatches(ctx, []int64{wa.ID, 999}); !errors.Is(err, storage.ErrWatchNotFound) || len(ops) != 1 {
		t.Fatalf("unknown watch: %v, ops %v", err, ops)
	}
	if snap, err := e.svc.RemoveWatches(ctx, []int64{wa.ID, wb.ID}); err != nil || snap == "" ||
		ops[1] != `unwatch: stop watching 1 "A", 2 "B"` {
		t.Fatalf("remove watches %q, %v, ops %v", snap, err, ops)
	}
	if all, _ := e.svc.List(ctx); len(all) != 0 {
		t.Errorf("left %+v", all)
	}
}

func TestNewChapters(t *testing.T) {
	e := newEnv(t)
	m := &fakeManga{info: mangadata.MangaInfo{ID: "dn", Title: "Death Note"}}
	src := e.source("mango-mangadex", m)
	src.chs["dn"] = m.chapters(1, 2, 3, 3.5, 4, 5)
	got, err := NewChapters(context.Background(), src, "dn", "Death Note", []float32{1, 3})
	if err != nil || !slices.Equal(got, []float32{3.5, 4, 5}) {
		t.Errorf("new %v, %v; want after 3 only", got, err)
	}
	if _, err := NewChapters(context.Background(), src, "gone", "Gone", nil); !errors.Is(err, ErrMangaNotFound) {
		t.Errorf("missing manga: %v", err)
	}
}

// a watch under a provider's old id and one under its current id check one after the
// other under the current id's lock, even one provider at a time
func TestCheckOldAndCurrentID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e := newEnv(t)
	e.svc.Options.ParallelProviders = 1
	wc := libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central", History: []libmangal.ProviderAlias{{ID: "mango-mangasee", Name: "MangaSee"}}}
	e.svc.Providers = func() ([]libmangal.ProviderInfo, error) { return []libmangal.ProviderInfo{wc}, nil }
	a := &fakeManga{info: mangadata.MangaInfo{ID: "a", Title: "A"}}
	b := &fakeManga{info: mangadata.MangaInfo{ID: "b", Title: "B"}}
	src := e.source("mango-weebcentral", a, b)
	src.chs["a"] = []*fakeChapter{a.chapter(1)}
	src.chs["b"] = []*fakeChapter{b.chapter(1)}
	e.sources["mango-mangasee"] = src
	old, err := e.svc.Watches.Add(storage.Watch{Provider: "mango-mangasee", MangaID: "a", Title: "A", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	cur, err := e.svc.Watches.Add(storage.Watch{Provider: "mango-weebcentral", MangaID: "b", Title: "B", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	report, err := e.svc.Check(ctx, []int64{cur.ID, old.ID}, CheckOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatal("deadlocked")
	}
	for _, r := range report.Results {
		if r.Status != storage.StatusNew {
			t.Errorf("%s: %s %s", r.Title, r.Status, r.Error)
		}
	}
}
