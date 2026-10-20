package client

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/util/cache"
)

type (
	fManga struct {
		info mangadata.MangaInfo
		meta metadata.Metadata
	}
	fVolume  struct{ manga *fManga }
	fChapter struct {
		info   mangadata.ChapterInfo
		volume fVolume
	}
	fPage struct{ i int }
)

func (m *fManga) String() string                     { return m.info.Title }
func (m *fManga) Info() mangadata.MangaInfo          { return m.info }
func (m *fManga) Metadata() metadata.Metadata        { return m.meta }
func (m *fManga) SetMetadata(meta metadata.Metadata) { m.meta = meta }
func (v fVolume) String() string                     { return "1" }
func (v fVolume) Info() mangadata.VolumeInfo         { return mangadata.VolumeInfo{Number: 1} }
func (v fVolume) Manga() mangadata.Manga             { return v.manga }
func (c *fChapter) String() string                   { return c.info.Title }
func (c *fChapter) Info() mangadata.ChapterInfo      { return c.info }
func (c *fChapter) Volume() mangadata.Volume         { return c.volume }
func (p fPage) String() string                       { return fmt.Sprint(p.i) }
func (fPage) Extension() string                      { return ".jpg" }
func (fPage) Chapter() mangadata.Chapter             { return nil }
func (pagesProvider) String() string                 { return "pages" }
func (pagesProvider) Close() error                   { return nil }
func (pagesProvider) Info() libmangal.ProviderInfo   { return pagesInfo }
func (pagesProvider) SetLogger(*logger.Logger)       {}
func (pagesProvider) GetPageImage(context.Context, mangadata.Page) ([]byte, error) {
	return []byte("image"), nil
}

var pagesInfo = libmangal.ProviderInfo{ID: "mango-mangapill", Name: "Mangapill", Version: "1.0.0"}

// pagesProvider has only the pages of its own chapters (2 each).
type pagesProvider struct{ libmangal.Provider }

func (pagesProvider) ChapterPages(_ context.Context, c mangadata.Chapter) ([]mangadata.Page, error) {
	if _, ok := c.(*fChapter); !ok {
		return nil, errors.New("not my chapter")
	}
	return []mangadata.Page{fPage{0}, fPage{1}}, nil
}

type pagesLoader struct{}

func (pagesLoader) String() string                                   { return "pages" }
func (pagesLoader) Info() libmangal.ProviderInfo                     { return pagesInfo }
func (pagesLoader) Load(context.Context) (libmangal.Provider, error) { return pagesProvider{}, nil }

// the chapter goes to the home manga's folder with its title, recorded as the other
// provider's; the folder stays the home manga's
func TestDownloadFill(t *testing.T) {
	ctx := context.Background()
	c, err := libmangal.NewClientContext(ctx, pagesLoader{}, libmangal.DefaultClientOptions())
	if err != nil {
		t.Fatal(err)
	}
	meta := &mangadata.Metadata{EnglishTitle: "Dark Gathering", AuthorList: []string{"Kenichi Kondou"}, DateStart: metadata.Date{Year: 2019},
		PublicationStatus: metadata.StatusFinished, ProviderID: "md", ProviderIDCode: "md"}
	home := &fManga{info: mangadata.MangaInfo{ID: "md", Title: "Dark Gathering"}, meta: meta}
	from := &fManga{info: mangadata.MangaInfo{ID: "mp", Title: "Dark Gathering (mangapill)"}}
	src := &fChapter{info: mangadata.ChapterInfo{Number: 1, Title: "Chapter 1", URL: "https://mangapill.com/chapters/1"}, volume: fVolume{from}}

	folder := filepath.Join(t.TempDir(), "Dark Gathering")
	homeInfo := libmangal.ProviderInfo{ID: "mango-mangadex", Name: "MangaDex"}
	fill := Fill{Provider: homeInfo, Manga: home, From: pagesInfo, Folder: folder, MangaDir: true}
	options := libmangal.DefaultDownloadOptions()
	options.SearchMetadata, options.WriteComicInfoXML = false, true
	chapter := fill.Chapter(src, "Lie Low", mangadata.VolumeInfo{Number: 1})

	results := DownloadFill(ctx, c, fill, []mangadata.Chapter{chapter}, options, nil)
	if len(results.Succeed()) != 1 {
		t.Fatalf("results %+v (%v)", results[0], results[0].Err)
	}
	path := results[0].Down.Path()
	if filepath.Dir(path) != folder || !strings.Contains(filepath.Base(path), "Lie Low") {
		t.Errorf("path %s", path)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var comicInfo string
	for _, f := range zr.File {
		if f.Name == metadata.FilenameComicInfoXML {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			comicInfo = string(b)
		}
	}
	for _, want := range []string{"<Title>Lie Low</Title>", "<Web>https://mangapill.com/chapters/1</Web>", "<Notes>mangal: provider=mango-mangapill manga_id=mp chapter_url=https://mangapill.com/chapters/1 title_provider=mango-mangadex title_manga_id=md&#xA;", "<Series>Dark Gathering</Series>"} {
		if !strings.Contains(comicInfo, want) {
			t.Errorf("ComicInfo.xml without %q:\n%s", want, comicInfo)
		}
	}

	dirInfo, err := libmangal.ReadMangaDirInfo(c.FS(), folder)
	if err != nil || dirInfo.Provider != "mango-mangadex" || dirInfo.Manga.ID != "md" {
		t.Errorf(".libmangal.json %+v %v", dirInfo, err)
	}
	library, err := cache.Library()
	if err != nil {
		t.Fatal(err)
	}
	m, ok, err := library.MangaByFolder(filepath.Dir(folder), filepath.Base(folder))
	if err != nil || !ok || len(m.Sources) != 1 || m.Sources[0].Provider != "mango-mangadex" || m.Sources[0].MangaID != "md" {
		t.Fatalf("library manga %+v %t %v", m, ok, err)
	}
	downloads, err := library.Downloads(m.ID)
	if err != nil || len(downloads) != 1 {
		t.Fatalf("downloads %+v %v", downloads, err)
	}
	if d := downloads[0]; d.Provider != "mango-mangapill" || d.MangaID != "mp" || d.ChapterURL != src.info.URL || d.ChapterTitle != "Lie Low" ||
		d.TitleProvider != "mango-mangadex" || d.TitleMangaID != "md" || d.TitleChanged.IsZero() {
		t.Errorf("download %+v", d)
	}
}
