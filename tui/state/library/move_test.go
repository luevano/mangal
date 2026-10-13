package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
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
	if !ms.confirm.Active() || !strings.Contains(ms.View(), "title:") {
		t.Fatalf("no confirmation of the full move:\n%s", ms.View())
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
	if !ms.confirm.Active() || !strings.Contains(ms.View(), "Weeb Central/Berserk") {
		t.Fatalf("no confirmation:\n%s", ms.View())
	}
	act, ok := ms.Update(ctx, press("y"))().(actionMsg)
	if !ok {
		t.Fatal("yes isn't the move")
	}
	reload, err := act.do(func(string) {})
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
