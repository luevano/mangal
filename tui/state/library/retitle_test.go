package library

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/state/pick"
	"github.com/spf13/afero"
)

type testChapter struct{ info mangadata.ChapterInfo }

func (c *testChapter) String() string              { return c.info.Title }
func (c *testChapter) Info() mangadata.ChapterInfo { return c.info }
func (c *testChapter) Volume() mangadata.Volume    { return nil }

func TestRetitle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	lib := storage.Library{DB: db}
	dir := filepath.Join(root, "Dark Gathering")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "1.cbz"), []byte("x"), 0o644)
	if err := lib.RecordDownload(root, "Dark Gathering", storage.Download{Path: filepath.Join(dir, "1.cbz"), Provider: "mango-mangapill", MangaID: "dg", MangaTitle: "Dark Gathering", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	m, _, _ := lib.MangaBySource("mango-mangapill", "dg")

	env := useCore(t, lib, afero.NewOsFs(), root).Env()
	env.ChapterName = func(_ libmangal.ProviderInfo, ch mangadata.Chapter) string { return ch.Info().Title }
	env.AllChapters = func(_ context.Context, provider, id, _, _ string) (libmangal.ProviderInfo, mangadata.Manga, []mangadata.Chapter, error) {
		return libmangal.ProviderInfo{ID: provider}, &testManga{mangadata.MangaInfo{ID: id, Title: "Dark Gathering"}},
			[]mangadata.Chapter{&testChapter{mangadata.ChapterInfo{Number: 1, Title: "Gathering", Unavailable: "retired"}}}, nil
	}
	c, err := core.New(ctx, env)
	if err != nil {
		t.Fatal(err)
	}
	base.Core = func() (*core.Core, error) { return c, nil }

	if _, ok := newMangaState(m).Update(ctx, press("alt+t"))().(*pick.Providers); !ok {
		t.Fatal("alt+t doesn't open the provider picker")
	}
	s := newRetitleState(m, "mango-mangadex")
	s.Resize(base.Size{Width: 120, Height: 40})
	if s.Search.Query() != "Dark Gathering" || s.Provider() != "mango-mangadex" {
		t.Errorf("search %q on %s", s.Search.Query(), s.Provider())
	}
	plan, err := planRetitle(ctx, m, "mango-mangadex", &testManga{mangadata.MangaInfo{ID: "md", Title: "Dark Gathering"}})
	if err != nil {
		t.Fatal(err)
	}
	s.Update(ctx, retitlePlannedMsg{plan: plan})
	if !s.Confirm.Active() || !strings.Contains(s.View(), `"" -> "Gathering"`) {
		t.Fatalf("no confirmation:\n%s", s.View())
	}
	act, ok := s.Update(ctx, press("y"))().(actionMsg)
	if !ok {
		t.Fatal("yes isn't the retitle")
	}
	if _, err := act.do(ctx, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if dls, _ := lib.Downloads(m.ID); dls[0].ChapterTitle != "Gathering" || dls[0].Path != filepath.Join(dir, "Gathering.cbz") {
		t.Errorf("retitled %+v", dls)
	}
}
