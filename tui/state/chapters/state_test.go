package chapters

import (
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/spf13/afero"

	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui/base"
	tuijobs "github.com/luevano/mangal/v5/tui/jobs"
)

type fakeManga struct {
	id   string
	meta metadata.Metadata
}

func (m *fakeManga) String() string { return "Berserk" }
func (m *fakeManga) Info() mangadata.MangaInfo {
	return mangadata.MangaInfo{ID: m.id, Title: "Berserk"}
}
func (m *fakeManga) Metadata() metadata.Metadata        { return m.meta }
func (m *fakeManga) SetMetadata(meta metadata.Metadata) { m.meta = meta }

type fakeVolume struct{ manga *fakeManga }

func (v fakeVolume) String() string             { return "1" }
func (v fakeVolume) Info() mangadata.VolumeInfo { return mangadata.VolumeInfo{Number: 1} }
func (v fakeVolume) Manga() mangadata.Manga     { return v.manga }

type fakeChapter struct {
	volume      fakeVolume
	number      float32
	unavailable string
}

func (c fakeChapter) String() string { return fmt.Sprintf("Chapter %v", c.number) }
func (c fakeChapter) Info() mangadata.ChapterInfo {
	return mangadata.ChapterInfo{Number: c.number, Title: c.String(), Unavailable: c.unavailable}
}
func (c fakeChapter) Volume() mangadata.Volume { return c.volume }

func press(k string) tea.KeyPressMsg {
	if k == " " {
		return util.KeyPress("space")
	}
	return util.KeyPress(k)
}

func chapters(n int) (*fakeManga, []mangadata.Chapter) {
	manga := &fakeManga{id: "berserk"}
	var list []mangadata.Chapter
	for i := 1; i <= n; i++ {
		list = append(list, fakeChapter{volume: fakeVolume{manga}, number: float32(i)})
	}
	return manga, list
}

// the confirm names the toggled chapter and that one downloads, not the hovered one (A10)
func TestDownloadTheToggledChapter(t *testing.T) {
	manga, list := chapters(3)
	s := New(nil, manga, nil, list).(*state)
	s.Resize(base.Size{Width: 80, Height: 30})
	ctx := context.Background()

	s.Update(ctx, press(" "))
	s.Update(ctx, press("j"))
	s.Update(ctx, press("j"))
	if i := s.List.SelectedItem().(*item); i.chapter.Info().Number != 3 {
		t.Fatalf("hovering chapter %v, want 3", i.chapter.Info().Number)
	}
	s.Update(ctx, press("d"))
	if !s.Confirm.Active() || !strings.Contains(s.Confirm.View(), "Download chapter 1 ") {
		t.Fatalf("confirm doesn't name chapter 1:\n%s", s.Confirm.View())
	}
	msg := s.Confirm.Update(press("y"))()
	got, ok := msg.(downloadMsg)
	if !ok || got.item == nil || got.item.chapter.Info().Number != 1 {
		t.Fatalf("yes: %#v, want a download of chapter 1", msg)
	}
}

func TestCheckMetadata(t *testing.T) {
	manga, _ := chapters(0)
	if err := checkMetadata(manga, libmangal.DownloadOptions{Strict: true}); !errors.Is(err, errNoMetadata) {
		t.Errorf("strict without metadata: %v", err)
	}
	if err := checkMetadata(manga, libmangal.DownloadOptions{}); err != nil {
		t.Errorf("not strict: %v", err)
	}
}

func TestLibraryDownloads(t *testing.T) {
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lib := storage.Library{DB: db}
	// downloaded under the provider's old id
	for _, dl := range []storage.Download{
		{Path: "/dl/Berserk/1.cbz", Chapter: 1, Format: libmangal.FormatCBZ.String()},
		{Path: "/dl/Berserk/1.pdf", Chapter: 1, Format: libmangal.FormatPDF.String()},
		{Path: "/dl/Berserk/2.cbz", Chapter: 2, Format: libmangal.FormatCBZ.String()},
	} {
		dl.Provider, dl.MangaID, dl.MangaTitle = "mango-old", "berserk", "Berserk"
		if err := lib.RecordDownload("/dl", "Berserk", dl); err != nil {
			t.Fatal(err)
		}
	}
	provider := libmangal.ProviderInfo{ID: "mango-new", Name: "New", History: []libmangal.ProviderAlias{{ID: "mango-old"}}}

	manga, _ := chapters(0)
	c := testCore(t, db)
	files, err := c.Library.ChapterFiles(t.Context(), core.ChapterFilesReq{Provider: provider, MangaID: manga.Info().ID})
	if err != nil || !files.Found || len(files.Errors) != 0 {
		t.Fatalf("library manga: %+v, %v", files, err)
	}
	got := downloadPaths(files.Downloads)
	if got[1][libmangal.FormatCBZ] != "/dl/Berserk/1.cbz" || got[1][libmangal.FormatPDF] != "/dl/Berserk/1.pdf" || got[2][libmangal.FormatCBZ] != "/dl/Berserk/2.cbz" {
		t.Errorf("got %v", got)
	}

	if files, err := c.Library.ChapterFiles(t.Context(), core.ChapterFilesReq{Provider: provider, MangaID: "other"}); err != nil || files.Found {
		t.Errorf("another manga: %+v, %v", files, err)
	}

	// the items show the library's formats and read the download first
	s := New(nil, manga, nil, []mangadata.Chapter{fakeChapter{volume: fakeVolume{manga}, number: 1}}).(*state)
	items := s.items()
	s.setFiles(filesMsg{downloads: got, items: items, temp: []map[libmangal.Format]string{{libmangal.FormatPDF: "/tmp/1.pdf"}}})
	if items[0].readPath == "" || len(items[0].downloaded) != 2 {
		t.Errorf("item: read %q, downloaded %v", items[0].readPath, items[0].downloaded)
	}
}

func TestReadMarks(t *testing.T) {
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	history := storage.History{DB: db, User: storage.DefaultUser}
	now := time.Unix(1, 0)
	for _, r := range []storage.Read{
		{Provider: "mango-old", MangaID: "berserk", Chapter: 1, Last: now},              // under the old id
		{Provider: "mango-other", MangaID: "x", Chapter: 2, LibraryManga: 0, Last: now}, // another manga
	} {
		if err := history.Record(r); err != nil {
			t.Fatal(err)
		}
	}
	provider := libmangal.ProviderInfo{ID: "mango-new", History: []libmangal.ProviderAlias{{ID: "mango-old"}}}
	manga, list := chapters(2)
	files, err := testCore(t, db).Library.ChapterFiles(t.Context(), core.ChapterFilesReq{Provider: provider, MangaID: manga.Info().ID})
	read := readSet(files.Read)
	if err != nil || !read[1] || read[2] {
		t.Fatalf("read %v, %v", read, err)
	}

	s := New(nil, manga, nil, list).(*state)
	items := s.items()
	s.setFiles(filesMsg{read: read, items: items, temp: make([]map[libmangal.Format]string, len(items))})
	mark := s.renderedSep + icon.Read.Colored()
	if title := items[0].Title(); !strings.Contains(title, mark) {
		t.Errorf("chapter 1 not marked read: %q", title)
	}
	if title := items[1].Title(); strings.Contains(title, mark) {
		t.Errorf("chapter 2 marked read: %q", title)
	}
}

// TestKeys: no key means two things on the screen (its keys, the list's, the global ones).
func TestKeys(t *testing.T) {
	manga, list := chapters(3)
	s := New(nil, manga, nil, list).(*state)
	if keys := base.KeyCollisions(s.keyMap(), s.List.KeyMap.Browsing()...); len(keys) != 0 {
		t.Error(keys)
	}
}

func TestWebURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://mangadex.org/chapter/1": true,
		"http://example.com":             true,
		"HTTPS://EXAMPLE.COM/x":          true,
		"":                               false,
		"file:///etc/passwd":             false,
		`\\server\share\x.exe`:           false,
		"//server/share":                 false,
		"javascript:alert(1)":            false,
		"https:///nohost":                false,
		"C:\\Windows\\notepad.exe":       false,
	} {
		if got := webURL(raw); got != want {
			t.Errorf("webURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

type fakeLoader struct{}

func (fakeLoader) String() string { return "fake" }
func (fakeLoader) Info() libmangal.ProviderInfo {
	return libmangal.ProviderInfo{ID: "fake", Name: "Fake", Version: "0.1.0"}
}
func (l fakeLoader) Load(context.Context) (libmangal.Provider, error) {
	return &fakeProvider{}, nil
}

type fakeProvider struct{ libmangal.Provider }

func (p *fakeProvider) Info() libmangal.ProviderInfo { return fakeLoader{}.Info() }
func (p *fakeProvider) String() string               { return "Fake" }
func (p *fakeProvider) SetLogger(*logger.Logger)     {}
func (p *fakeProvider) Close() error                 { return nil }

// A download goes to the provider's queue (behind a job that has it): the batch stays on
// the chapters with a notice of how to open its view, the chapters are marked queued, a
// second download of them is refused.
func TestDownloadQueues(t *testing.T) {
	m := jobs.New(context.Background())
	tuijobs.SetManager(m)
	// the queued download must never start (the fake provider has no pages): cancel every
	// job before the blocker could end the check
	t.Cleanup(func() {
		m.CancelAll()
		if !m.Shutdown(time.Second) {
			t.Error("a job still runs: the queued download started")
		}
		tuijobs.SetManager(nil)
	})
	blocker := make(chan struct{})
	m.Add(jobs.Spec{Kind: jobs.KindCheck, Title: "check", Providers: []string{"fake"}, Run: func(ctx context.Context, p *jobs.Progress) error {
		select {
		case <-blocker:
		case <-ctx.Done():
		}
		return nil
	}})

	c, err := libmangal.NewClient(fakeLoader{}, libmangal.DefaultClientOptions())
	if err != nil {
		t.Fatal(err)
	}
	manga, list := chapters(3)
	s := New(c, manga, nil, list).(*state)
	s.Resize(base.Size{Width: 80, Height: 30})
	ctx := context.Background()
	old := config.Download.Metadata.Strict.Get()
	if err := config.Download.Metadata.Strict.Set(false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Download.Metadata.Strict.Set(old) })

	got := base.Drain(s.Update(ctx, downloadMsg{chapters: list[:2]}))
	if len(got) != 1 {
		t.Fatalf("the batch sent %v, want a notice", got)
	}
	want := "Queued 2 chapters behind check (" + base.KeyDownloads.Help().Key + " downloads, enter opens it)"
	if n, ok := got[0].(base.NotificationWithDurationMsg); !ok || n.Message != want {
		t.Errorf("the batch sent %#v, want the notice %q", got[0], want)
	}
	var job jobs.Job
	for _, j := range m.Jobs() {
		if j.Kind == jobs.KindDownload {
			job = j
		}
	}
	if job.Status != jobs.Queued || job.Origin != s.Key() || job.Total != 2 {
		t.Errorf("job %s, origin %q, %d chapters", job.Status, job.Origin, job.Total)
	}
	items := s.items()
	if !strings.Contains(items[0].Title(), icon.Queued.Colored()) || strings.Contains(items[2].Title(), icon.Queued.Colored()) {
		t.Errorf("marks: %q, %q", items[0].Title(), items[2].Title())
	}
	got = base.Drain(s.Update(ctx, downloadMsg{item: items[0]}))
	if len(got) != 1 || !strings.Contains(fmt.Sprint(got[0]), "already being downloaded") {
		t.Errorf("downloading it again: %v", got)
	}
	if n := len(m.Jobs()); n != 2 {
		t.Errorf("%d jobs, want 2", n)
	}
}

// testCore is a core over the database.
func testCore(t *testing.T, db *storage.DB) *core.Core {
	t.Helper()
	c, err := core.New(t.Context(), core.Env{
		Storage:      storage.Storage{Data: db},
		FS:           afero.NewMemMapFs(),
		Root:         func() string { return "/dl" },
		ProviderDirs: func() bool { return false },
		Loaders:      func() ([]libmangal.ProviderLoader, error) { return nil, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSelectAllSkipsUnavailable(t *testing.T) {
	manga, list := chapters(3)
	list[1] = fakeChapter{volume: fakeVolume{manga}, number: 2, unavailable: "retired"}
	s := New(nil, manga, nil, list).(*state)
	s.Resize(base.Size{Width: 80, Height: 30})
	if !strings.Contains(s.View(), "[retired]") {
		t.Errorf("the reason isn't shown:\n%s", s.View())
	}
	cmd := s.selectAll()
	if len(s.selected) != 2 {
		t.Errorf("selected %d, want 2", len(s.selected))
	}
	if cmd == nil {
		t.Fatal("no notice")
	}
	if msg, ok := cmd().(base.NotificationMsg); !ok || msg.Message != "Selected 2 chapters (1 unavailable skipped)" {
		t.Errorf("notice: %#v", msg)
	}
	if _, list := chapters(2); New(nil, manga, nil, list).(*state).selectAll() != nil {
		t.Error("a notice with nothing skipped")
	}
}
