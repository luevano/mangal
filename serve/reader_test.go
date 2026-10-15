package serve

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	lmlogger "github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web/apiv1"
	"github.com/spf13/afero"
)

// a png's first bytes, enough for the content sniffing
var pngData = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)

// newReaderServer is a server whose library has One-Punch Man's chapter 1 as a cbz, 2 as
// a folder and 3 as a pdf, a cover, and the reader's provider "reader".
func newReaderServer(t *testing.T) (*testServer, afero.Fs) {
	s := newTestServer(t, "")
	fsys := afero.NewMemMapFs()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"0002.png", "0001.png"} {
		w, _ := zw.Create(name)
		w.Write(pngData)
	}
	zw.Close()
	afero.WriteFile(fsys, "/lib/One-Punch Man/1.cbz", buf.Bytes(), 0o644)
	afero.WriteFile(fsys, "/lib/One-Punch Man/2/0001.png", pngData, 0o644)
	afero.WriteFile(fsys, "/lib/One-Punch Man/3.pdf", []byte("%PDF-1.4"), 0o644)
	afero.WriteFile(fsys, "/lib/One-Punch Man/cover.jpg", []byte("\xff\xd8\xff"), 0o644)
	for _, d := range []storage.Download{
		{Path: "/lib/One-Punch Man/1.cbz", Chapter: 1, Format: "CBZ"},
		{Path: "/lib/One-Punch Man/2", Chapter: 2, Format: "IMAGES"},
		{Path: "/lib/One-Punch Man/3.pdf", Chapter: 3, Format: "PDF"},
	} {
		d.Provider, d.MangaID, d.MangaTitle = "fake", "opm", "One-Punch Man"
		if err := s.src.library.RecordDownload("/lib", "One-Punch Man", d); err != nil {
			t.Fatal(err)
		}
	}
	s.api.Core = func(m watch.Manager) (*core.Core, error) {
		return core.New(context.Background(), core.Env{
			Storage: storage.Storage{Data: s.src.library.DB},
			FS:      fsys,
			Root:    func() string { return "/lib" },
			Modes:   func() library.Modes { return library.DefaultModes },
			Loaders: func() ([]libmangal.ProviderLoader, error) {
				return []libmangal.ProviderLoader{readerLoader{}}, nil
			},
			Watches: func(context.Context) (watch.Manager, error) { return m, nil },
		})
	}
	return s, fsys
}

func (s *testServer) get(t *testing.T, path string) (int, http.Header, []byte) {
	t.Helper()
	resp, err := http.Get(s.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

// TestV1ReaderLibrary: the pages of a cbz and a folder, a pdf read whole, the file to
// download, the cover; only the manga's downloads are read.
func TestV1ReaderLibrary(t *testing.T) {
	s, _ := newReaderServer(t)
	q := func(p string) string { return "?path=" + url.QueryEscape(p) }

	var pages apiv1.ChapterPages
	if code := s.call(t, "GET", "/api/v1/library/mangas/1/pages"+q("/lib/One-Punch Man/1.cbz"), "", &pages); code != http.StatusOK ||
		pages.Format != "CBZ" || len(pages.Pages) != 2 || *pages.Pages[0].Name != "0001.png" || pages.Pdf != nil {
		t.Fatalf("cbz pages %d %+v", code, pages)
	}
	code, h, body := s.get(t, "/api/v1/library/mangas/1/pages/1"+q("/lib/One-Punch Man/1.cbz"))
	if code != http.StatusOK || h.Get("Content-Type") != "image/png" || !bytes.Equal(body, pngData) || h.Get("Cache-Control") == "" {
		t.Errorf("cbz page %d %v", code, h)
	}
	if code, _, _ := s.get(t, "/api/v1/library/mangas/1/pages/2"+q("/lib/One-Punch Man/1.cbz")); code != http.StatusNotFound {
		t.Errorf("past the last page %d", code)
	}
	if code := s.call(t, "GET", "/api/v1/library/mangas/1/pages"+q("/lib/One-Punch Man/2"), "", &pages); code != http.StatusOK ||
		pages.Format != "Images" || len(pages.Pages) != 1 {
		t.Errorf("folder pages %d %+v", code, pages)
	}
	if code := s.call(t, "GET", "/api/v1/library/mangas/1/pages"+q("/lib/One-Punch Man/3.pdf"), "", &pages); code != http.StatusOK ||
		pages.Pdf == nil || !*pages.Pdf || len(pages.Pages) != 0 {
		t.Errorf("pdf pages %d %+v", code, pages)
	}
	code, h, body = s.get(t, "/api/v1/library/mangas/1/file"+q("/lib/One-Punch Man/3.pdf")+"&inline=1")
	if code != http.StatusOK || h.Get("Content-Type") != "application/pdf" || string(body) != "%PDF-1.4" || !strings.HasPrefix(h.Get("Content-Disposition"), "inline") {
		t.Errorf("pdf file %d %v %q", code, h, body)
	}
	code, h, body = s.get(t, "/api/v1/library/mangas/1/file"+q("/lib/One-Punch Man/2"))
	if code != http.StatusOK || h.Get("Content-Type") != "application/zip" || !strings.Contains(h.Get("Content-Disposition"), "filename=2.zip") {
		t.Errorf("folder file %d %v", code, h)
	}
	if r, err := zip.NewReader(bytes.NewReader(body), int64(len(body))); err != nil || len(r.File) != 1 {
		t.Errorf("folder zip: %v", err)
	}
	// not a download of the manga
	for _, p := range []string{"/etc/passwd", "/lib/One-Punch Man/cover.jpg", "/lib/One-Punch Man/../One-Punch Man/1.cbz"} {
		if code, _, _ := s.get(t, "/api/v1/library/mangas/1/pages/0"+q(p)); code != http.StatusNotFound {
			t.Errorf("%s: %d", p, code)
		}
		if code, _, _ := s.get(t, "/api/v1/library/mangas/1/file"+q(p)); code != http.StatusNotFound {
			t.Errorf("%s file: %d", p, code)
		}
	}
	if code, _, _ := s.get(t, "/api/v1/library/mangas/1/pages"); code != http.StatusBadRequest {
		t.Errorf("no path: %d", code)
	}
	code, h, _ = s.get(t, "/api/v1/library/mangas/1/cover")
	if code != http.StatusOK || h.Get("Content-Type") != "image/jpeg" {
		t.Errorf("cover %d %v", code, h)
	}
	if code, _, _ := s.get(t, "/api/v1/library/mangas/9/cover"); code != http.StatusNotFound {
		t.Errorf("no manga's cover %d", code)
	}
}

// TestV1ReaderOnline: a provider's chapter's pages, through the provider.
func TestV1ReaderOnline(t *testing.T) {
	s, _ := newReaderServer(t)
	var pages apiv1.ChapterPages
	if code := s.call(t, "GET", "/api/v1/providers/reader/pages?manga_id=m&chapter=2", "", &pages); code != http.StatusOK ||
		pages.Format != "ONLINE" || len(pages.Pages) != 3 {
		t.Fatalf("pages %d %+v", code, pages)
	}
	code, h, body := s.get(t, "/api/v1/providers/reader/pages/2?manga_id=m&chapter=2")
	if code != http.StatusOK || h.Get("Content-Type") != "image/png" || !bytes.Equal(body, pngData) {
		t.Errorf("page %d %v", code, h)
	}
	if code, _, _ := s.get(t, "/api/v1/providers/reader/pages/3?manga_id=m&chapter=2"); code != http.StatusNotFound {
		t.Errorf("past the end %d", code)
	}
	if code, _, _ := s.get(t, "/api/v1/providers/reader/pages?manga_id=m&chapter=9"); code != http.StatusNotFound {
		t.Errorf("no chapter %d", code)
	}
	if code, _, _ := s.get(t, "/api/v1/providers/reader/pages?manga_id=m&chapter=1&volume=7"); code != http.StatusNotFound {
		t.Errorf("no such volume %d", code)
	}
}

// TestV1RecordRead: a read goes to the history with its library manga, the progress is
// synced with the library manga's ids; a read scope isn't enough.
func TestV1RecordRead(t *testing.T) {
	s, _ := newReaderServer(t)
	if err := s.src.library.RememberMangaDir("/lib", "One-Punch Man", libmangal.MangaDirInfo{MetadataIDs: []metadata.ID{{Code: metadata.IDCodeAnilist, Raw: "21087"}}}); err != nil {
		t.Fatal(err)
	}
	var gotIDs map[metadata.IDCode]string
	var gotChapter float32
	syncProgress = func(_ context.Context, _ []storage.Binding, ids map[metadata.IDCode]string, chapter float32) ([]string, error) {
		gotIDs, gotChapter = ids, chapter
		return []string{"Anilist"}, nil
	}
	t.Cleanup(func() { syncProgress = defaultSyncProgress })
	var out apiv1.ReadResponse
	if code := s.call(t, "POST", "/api/v1/reads", `{"provider":"fake","manga_id":"opm","manga_title":"One-Punch Man","chapter":2,"chapter_title":"Two"}`, &out); code != http.StatusOK ||
		!out.Recorded || len(out.Synced) != 1 {
		t.Fatalf("read %d %+v", code, out)
	}
	if gotChapter != 2 || gotIDs[metadata.IDCodeAnilist] != "21087" {
		t.Errorf("synced %v %v", gotChapter, gotIDs)
	}
	reads, err := storage.History{DB: s.src.library.DB}.List("fake", "opm")
	if err != nil || len(reads) != 1 || reads[0].LibraryManga != 1 || reads[0].ChapterTitle != "Two" {
		t.Fatalf("history %v %+v", err, reads)
	}
	gotIDs = nil
	if code := s.call(t, "POST", "/api/v1/reads", `{"provider":"fake","manga_id":"opm","chapter":2,"sync":false}`, &out); code != http.StatusOK || len(out.Synced) != 0 || gotIDs != nil {
		t.Errorf("no sync %d %+v", code, out)
	}
	if reads, _ := (storage.History{DB: s.src.library.DB}).List("fake", "opm"); reads[0].Count != 2 {
		t.Errorf("count %d", reads[0].Count)
	}
	// a scanned folder's chapter: no manga id, synced with the library manga's ids only
	gotIDs = nil
	if code := s.call(t, "POST", "/api/v1/reads", `{"provider":"fake","chapter":3,"library_manga":1}`, &out); code != http.StatusOK ||
		out.Recorded || out.Note == nil || gotIDs[metadata.IDCodeAnilist] != "21087" {
		t.Errorf("scanned %d %+v %v", code, out, gotIDs)
	}
	var e ErrorResponse
	if code := s.call(t, "POST", "/api/v1/reads", `{"provider":"fake","chapter":2}`, &e); code != http.StatusBadRequest {
		t.Errorf("no manga %d %+v", code, e)
	}
}

// readerLoader is a provider with one manga "m" of 2 chapters in volume 1, 3 pages each.
type readerLoader struct{}

func (readerLoader) String() string { return "reader" }
func (readerLoader) Info() libmangal.ProviderInfo {
	return libmangal.ProviderInfo{ID: "reader", Name: "reader", Version: "1.0.0"}
}
func (readerLoader) Load(context.Context) (libmangal.Provider, error) { return readerProvider{}, nil }

type readerProvider struct{}

type (
	rManga   struct{ meta metadata.Metadata }
	rVolume  struct{}
	rChapter struct{ n float32 }
	rPage    struct {
		c rChapter
		i int
	}
)

func (*rManga) String() string                       { return "M" }
func (*rManga) Info() mangadata.MangaInfo            { return mangadata.MangaInfo{ID: "m", Title: "M"} }
func (m *rManga) Metadata() metadata.Metadata        { return m.meta }
func (m *rManga) SetMetadata(meta metadata.Metadata) { m.meta = meta }
func (rVolume) String() string                       { return "1" }
func (rVolume) Info() mangadata.VolumeInfo           { return mangadata.VolumeInfo{Number: 1} }
func (rVolume) Manga() mangadata.Manga               { return &rManga{} }
func (c rChapter) String() string                    { return fmt.Sprint(c.n) }
func (c rChapter) Info() mangadata.ChapterInfo {
	return mangadata.ChapterInfo{Number: c.n, Title: fmt.Sprint(c.n)}
}
func (rChapter) Volume() mangadata.Volume           { return rVolume{} }
func (p rPage) String() string                      { return fmt.Sprint(p.i) }
func (rPage) Extension() string                     { return ".png" }
func (p rPage) Chapter() mangadata.Chapter          { return p.c }
func (readerProvider) String() string               { return "reader" }
func (readerProvider) Close() error                 { return nil }
func (readerProvider) Info() libmangal.ProviderInfo { return readerLoader{}.Info() }
func (readerProvider) SetLogger(*lmlogger.Logger)   {}
func (readerProvider) SearchMangas(context.Context, string) ([]mangadata.Manga, error) {
	return []mangadata.Manga{&rManga{}}, nil
}
func (readerProvider) MangaByID(_ context.Context, id string) (mangadata.Manga, bool, error) {
	return &rManga{}, id == "m", nil
}
func (readerProvider) MangaVolumes(context.Context, mangadata.Manga) ([]mangadata.Volume, error) {
	return []mangadata.Volume{rVolume{}}, nil
}
func (readerProvider) VolumeChapters(context.Context, mangadata.Volume) ([]mangadata.Chapter, error) {
	return []mangadata.Chapter{rChapter{1}, rChapter{2}}, nil
}
func (readerProvider) ChapterPages(_ context.Context, c mangadata.Chapter) ([]mangadata.Page, error) {
	return []mangadata.Page{rPage{c.(rChapter), 0}, rPage{c.(rChapter), 1}, rPage{c.(rChapter), 2}}, nil
}
func (readerProvider) GetPageImage(context.Context, mangadata.Page) ([]byte, error) {
	return pngData, nil
}
