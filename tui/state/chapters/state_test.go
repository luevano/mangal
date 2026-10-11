package chapters

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui/base"
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
	volume fakeVolume
	number float32
}

func (c fakeChapter) String() string { return fmt.Sprintf("Chapter %v", c.number) }
func (c fakeChapter) Info() mangadata.ChapterInfo {
	return mangadata.ChapterInfo{Number: c.number, Title: c.String()}
}
func (c fakeChapter) Volume() mangadata.Volume { return c.volume }

func press(k string) tea.KeyMsg {
	if k == " " {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
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
	if !s.confirm.Active() || !strings.Contains(s.confirm.View(), "Download chapter 1 ") {
		t.Fatalf("confirm doesn't name chapter 1:\n%s", s.confirm.View())
	}
	msg := s.confirm.Update(press("y"))()
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
	m, ok, err := lib.MangaFor(provider, manga, false)
	if err != nil || !ok {
		t.Fatalf("library manga: %t, %v", ok, err)
	}
	got, err := libraryDownloads(lib, nil, "/dl", m, false)
	if err != nil {
		t.Fatal(err)
	}
	if got[1][libmangal.FormatCBZ] != "/dl/Berserk/1.cbz" || got[1][libmangal.FormatPDF] != "/dl/Berserk/1.pdf" || got[2][libmangal.FormatCBZ] != "/dl/Berserk/2.cbz" {
		t.Errorf("got %v", got)
	}

	other := &fakeManga{id: "other"}
	if _, ok, err := lib.MangaFor(provider, other, false); err != nil || ok {
		t.Errorf("another manga: %t, %v", ok, err)
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
	history := storage.History{DB: db}
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
	read, err := readChapters(history, provider, manga, 0)
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
