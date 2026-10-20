package library

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/spf13/afero"
)

// o opens the current source's web page, looked up when it isn't recorded; a gone manga
// is a notice, nothing opened
func TestOpenSourceURL(t *testing.T) {
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lib := storage.Library{DB: db}
	c := useCore(t, lib, afero.NewOsFs(), t.TempDir())
	env := c.Env()
	asked := 0
	env.ProviderManga = func(_ context.Context, provider, id, _ string) (libmangal.ProviderInfo, mangadata.MangaInfo, bool, error) {
		asked++
		info := libmangal.ProviderInfo{ID: provider, Name: "MangaDex"}
		return info, mangadata.MangaInfo{ID: id, URL: "https://mangadex.org/title/" + id}, id != "gone", nil
	}
	if c, err = core.New(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	base.Core = func() (*core.Core, error) { return c, nil }
	var opened []string
	old := openURL
	openURL = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { openURL = old })

	for _, id := range []string{"md", "gone"} {
		if err := lib.RememberMangaDir("/lib", id, libmangal.MangaDirInfo{Provider: "mango-mangadex", Manga: mangadata.MangaInfo{ID: id, Title: id}}); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	m, _, _ := lib.MangaBySource("mango-mangadex", "md")
	s := newMangaState(m)
	for range 2 {
		base.Drain(s.Update(ctx, press("o")))
	}
	if asked != 1 || len(opened) != 2 || opened[0] != "https://mangadex.org/title/md" {
		t.Errorf("asked %d, opened %q", asked, opened)
	}

	gone, _, _ := lib.MangaBySource("mango-mangadex", "gone")
	msgs := base.Drain(newMangaState(gone).Update(ctx, press("o")))
	if len(opened) != 2 || len(msgs) == 0 || !strings.Contains(strings.Join(msgStrings(msgs), "\n"), "isn't on MangaDex any more") {
		t.Errorf("gone: opened %q, msgs %v", opened, msgs)
	}
}

func msgStrings(msgs []tea.Msg) []string {
	var out []string
	for _, m := range msgs {
		if err, ok := m.(error); ok {
			out = append(out, err.Error())
		}
	}
	return out
}
