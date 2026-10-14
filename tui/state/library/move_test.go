package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/state/pick"
	"github.com/spf13/afero"
)

type testManga struct{ info mangadata.MangaInfo }

func (m *testManga) String() string                  { return m.info.Title }
func (m *testManga) Info() mangadata.MangaInfo       { return m.info }
func (m *testManga) Metadata() metadata.Metadata     { return nil }
func (m *testManga) SetMetadata(_ metadata.Metadata) {}

func TestMove(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	lib := storage.Library{DB: db}
	dir := filepath.Join(root, "Berserk")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "1.cbz"), []byte("x"), 0o644)
	if err := lib.RecordDownload(root, "Berserk", storage.Download{Path: filepath.Join(dir, "1.cbz"), Provider: "mango-asurascans", MangaID: "berserk", MangaTitle: "Berserk", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	m, _, _ := lib.MangaBySource("mango-asurascans", "berserk")

	mover := library.Mover{
		Files:        library.Files{FS: afero.NewOsFs(), Library: lib, Root: root, Modes: library.DefaultModes},
		ProviderDirs: true,
		ProviderName: func(p libmangal.ProviderInfo) string { return p.Name },
		MangaName:    func(_ libmangal.ProviderInfo, m mangadata.Manga) string { return m.Info().Title },
	}
	oldMover, oldInfo, oldSearch := newMover, acquireInfo, pick.SearchProvider
	t.Cleanup(func() { newMover, acquireInfo, pick.SearchProvider = oldMover, oldInfo, oldSearch })
	newMover = func() (library.Mover, error) { return mover, nil }
	acquireInfo = func(_ context.Context, provider string) (libmangal.ProviderInfo, error) {
		return libmangal.ProviderInfo{ID: provider, Name: "Weeb Central"}, nil
	}
	pick.SearchProvider = func(_ context.Context, provider, query string) ([]mangadata.Manga, string, error) {
		return []mangadata.Manga{&testManga{mangadata.MangaInfo{ID: "01J", Title: query}}, &testManga{mangadata.MangaInfo{ID: "berserk", Title: "old"}}}, provider, nil
	}

	// M picks the provider first, then searches it
	s := newMangasState(mover.Files)
	s.onLoaded(mangasLoadedMsg{items: []*mangaItem{{manga: m}}})
	if _, ok := s.Update(ctx, press("X"))().(*pick.Providers); !ok {
		t.Fatal("M doesn't open the provider picker")
	}
	ms := newMoveState(m, "mango-weebcentral")
	ms.Resize(base.Size{Width: 120, Height: 40})
	if ms.Search.Query() != "Berserk" || ms.Provider() != "mango-weebcentral" {
		t.Errorf("search %q on %s", ms.Search.Query(), ms.Provider())
	}
	if res, _ := ms.Results(ctx, "mango-asurascans", "Berserk"); !res.Items[1].(*pick.MangaItem).Current {
		t.Error("the library manga's source isn't marked")
	}
	res, _ := ms.Results(ctx, "mango-weebcentral", "Berserk")
	res.Search = ms.NewSearch()
	ms.Update(ctx, res)

	// another title: keep it or adopt the new one
	asked, err := planMove(ctx, m, "mango-weebcentral", &testManga{mangadata.MangaInfo{ID: "01K", Title: "Berserk Deluxe"}}, nil)
	if err != nil || asked.full == nil {
		t.Fatalf("planned %+v, %v", asked, err)
	}
	ms.Update(ctx, asked)
	view := ms.View()
	for _, want := range []string{`"Berserk" -> "Berserk Deluxe"`, "Weeb Central/Berserk Deluxe", "k keep the title", "a adopt the new title"} {
		if !strings.Contains(strings.Join(strings.Fields(view), " "), want) {
			t.Errorf("question misses %q:\n%s", want, view)
		}
	}
	chosen, ok := ms.Update(ctx, press("a"))().(moveChosenMsg)
	if !ok || !chosen.plan.Full || chosen.plan.Title != "Berserk Deluxe" {
		t.Fatalf("a: %+v", chosen)
	}
	ms.Update(ctx, chosen)
	if !ms.Confirm.Active() || !strings.Contains(ms.View(), "title:") {
		t.Fatalf("no confirmation of the full move:\n%s", ms.View())
	}
	if ms.Backable() || !ms.Inputting() || !strings.Contains(fmt.Sprint(ms.KeyMap().ShortHelp()), "no") {
		t.Error("asking: esc, the keys and the help aren't the dialog's")
	}
	ms.Update(ctx, press("n"))

	planned, err := planMove(ctx, m, "mango-weebcentral", res.Items[0].(*pick.MangaItem).Manga, nil)
	if err != nil {
		t.Fatal(err)
	}
	if planned.full != nil {
		t.Error("same title, still asked")
	}
	ms.Update(ctx, planned)
	if !ms.Confirm.Active() || !strings.Contains(ms.View(), "Weeb Central/Berserk") {
		t.Fatalf("no confirmation:\n%s", ms.View())
	}
	act, ok := ms.Update(ctx, press("y"))().(actionMsg)
	if !ok {
		t.Fatal("yes isn't the move")
	}
	reload, err := act.do(ctx, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if got, _, _ := lib.MangaByID(m.ID); got.Path() != filepath.Join(root, "Weeb Central", "Berserk") || len(got.Sources) != 2 {
		t.Errorf("moved %+v", got)
	}
	if cmd := ms.Update(ctx, reload); cmd == nil {
		t.Error("done: no cmd")
	}
}

// xdevFs renames no folder: every folder move is a copy across drives.
type xdevFs struct{ afero.Fs }

func (f xdevFs) Rename(from, to string) error {
	if info, err := f.Stat(from); err == nil && info.IsDir() {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
	}
	return f.Fs.Rename(from, to)
}

// the move runs with the screen's ctx: a copy across drives reports its progress, esc
// (the ctx cancelled) stops it
func TestMoveProgress(t *testing.T) {
	root := t.TempDir()
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lib := storage.Library{DB: db}
	dir := filepath.Join(root, "Berserk")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "1.cbz"), make([]byte, 1<<20), 0o644)
	if err := lib.RecordDownload(root, "Berserk", storage.Download{Path: filepath.Join(dir, "1.cbz"), Provider: "mango-asurascans", MangaID: "berserk", MangaTitle: "Berserk", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	m, _, _ := lib.MangaBySource("mango-asurascans", "berserk")
	mover := library.Mover{
		Files:        library.Files{FS: xdevFs{afero.NewOsFs()}, Library: lib, Root: root, Modes: library.DefaultModes},
		ProviderDirs: true,
		ProviderName: func(p libmangal.ProviderInfo) string { return p.Name },
		MangaName:    func(_ libmangal.ProviderInfo, m mangadata.Manga) string { return m.Info().Title },
	}
	oldMover, oldInfo := newMover, acquireInfo
	t.Cleanup(func() { newMover, acquireInfo = oldMover, oldInfo })
	newMover = func() (library.Mover, error) { return mover, nil }
	acquireInfo = func(_ context.Context, provider string) (libmangal.ProviderInfo, error) {
		return libmangal.ProviderInfo{ID: provider, Name: "Weeb Central"}, nil
	}
	planned, err := planMove(context.Background(), m, "mango-weebcentral", &testManga{mangadata.MangaInfo{ID: "01J", Title: "Berserk"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ms := newMoveState(m, "mango-weebcentral")
	ms.Resize(base.Size{Width: 120, Height: 40})
	ms.Update(context.Background(), planned)
	act, ok := ms.Update(context.Background(), press("y"))().(actionMsg)
	if !ok {
		t.Fatal("yes isn't the move")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := act.do(ctx, func(string) {}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "1.cbz")); err != nil {
		t.Fatal("cancelled, still moved")
	}

	var reports []string
	ctx = progress.WithReporter(context.Background(), func(s progress.Status) { reports = append(reports, s.String()) })
	if _, err := act.do(ctx, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if len(reports) == 0 {
		t.Error("the copy didn't report its progress")
	}
	if _, err := os.Stat(filepath.Join(root, "Weeb Central", "Berserk", "1.cbz")); err != nil {
		t.Error("not moved")
	}
}

// the link's folder rename runs with the screen's ctx: across drives it reports the copy's
// progress, esc (the ctx cancelled) stops it
func TestLinksProgress(t *testing.T) {
	root := t.TempDir()
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lib := storage.Library{DB: db}
	dir := filepath.Join(root, "Berserk")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "1.cbz"), make([]byte, 1<<20), 0o644)
	if err := lib.RecordDownload(root, "Berserk", storage.Download{Path: filepath.Join(dir, "1.cbz"), Provider: "mango-asurascans", MangaID: "berserk", MangaTitle: "Berserk", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	m, _, _ := lib.MangaBySource("mango-asurascans", "berserk")
	to := filepath.Join(root, "Berserk (1989) [alid-30002]")
	linker := library.Linker{Files: library.Files{FS: xdevFs{afero.NewOsFs()}, Library: lib, Root: root, Modes: library.DefaultModes}}
	plan := library.LinkPlan{
		Info:    library.LinkInfo{Manga: m},
		Links:   library.Links{"al": "30002"},
		Changes: []library.LinkChange{{Code: "al", New: "30002"}},
		Rename:  &library.Rename{From: dir, To: to},
		Root:    root,
	}
	d := newDialog()
	d.Resize(base.Size{Width: 120, Height: 40})
	if cmd := askLinks(d, "", linkPlannedMsg{linker: linker, plan: plan}); cmd != nil || !d.Active() {
		t.Fatal("no choice dialog")
	}
	act, ok := d.Update(press("r"))().(actionMsg)
	if !ok {
		t.Fatal("r isn't the link")
	}
	if !strings.Contains(act.label, "renaming") {
		t.Errorf("label %q", act.label)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := act.do(ctx, func(string) {}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "1.cbz")); err != nil {
		t.Fatal("cancelled, still renamed")
	}
	if _, err := os.Stat(to); err == nil {
		t.Error("cancelled, the copy is left")
	}

	var reports []string
	ctx = progress.WithReporter(context.Background(), func(s progress.Status) { reports = append(reports, s.String()) })
	if _, err := act.do(ctx, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if len(reports) == 0 {
		t.Error("the copy didn't report its progress")
	}
	if _, err := os.Stat(filepath.Join(to, "1.cbz")); err != nil {
		t.Error("not renamed")
	}
}
