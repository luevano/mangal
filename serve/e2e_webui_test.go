//go:build webui

package serve

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web"
	"github.com/spf13/afero"
)

// TestWebUI drives the built web UI (npm run build, then go test -tags webui) in a
// headless browser against a server on a fixture library: library → manga → reader
// (paged, a page turned, webtoon), then a merge of the duplicates through the plan dialog
// and its undo. It needs a Chromium (the system's or go-rod's downloaded one), else it's
// skipped; it never downloads one.
func TestWebUI(t *testing.T) {
	bin, ok := launcher.LookPath()
	if !ok {
		if b := launcher.NewBrowser(); b.Validate() == nil {
			bin, ok = b.BinPath(), true
		}
	}
	if !ok {
		t.Skip("no chromium")
	}
	s := newE2EServer(t)

	u := launcher.New().Bin(bin).Headless(true).Leakless(false).MustLaunch()
	b := rod.New().ControlURL(u).MustConnect()
	t.Cleanup(b.MustClose)
	page := b.MustPage("")
	page.MustSetViewport(1280, 900, 1, false)
	// each step gets its time
	step := func() *rod.Page { return page.Timeout(20 * time.Second) }

	step().MustNavigate(s.URL + "/#/library")
	step().MustElement("[data-test=library-table]")
	if n := len(step().MustElements("[data-test=library-table] tbody tr")); n != 2 {
		t.Fatalf("%d mangas listed", n)
	}
	step().MustElementR("[data-test=library-table] a", "Berserk").MustClick()
	if title := step().MustElement("[data-test=manga-title]").MustText(); title != "Berserk" {
		t.Fatalf("manga page %q", title)
	}

	// the reader: paged, a page turned with the arrow, then webtoon
	step().MustElement("[data-test=read-next]").MustClick()
	img := step().MustElement("[data-test=reader-image]")
	img.MustWaitLoad()
	if w := img.MustEval(`() => this.naturalWidth`).Int(); w != 8 {
		t.Errorf("page width %d", w)
	}
	if got := step().MustElement("[data-test=reader-page]").MustText(); got != "1 / 3" {
		t.Errorf("page %q", got)
	}
	page.Keyboard.MustType(input.ArrowRight)
	step().MustElementR("[data-test=reader-page]", "^2 / 3$")
	// double pages: the first alone, then 2-3; thumbnails; zoom
	page.Keyboard.MustType('d')
	step().MustElementR("[data-test=reader-page]", "^2-3 / 3$")
	if n := len(step().MustElements("[data-test=reader-paged] .reader-zoom img")); n != 2 {
		t.Errorf("%d pages in the spread", n)
	}
	page.Keyboard.MustType('t')
	if n := len(step().MustElements("[data-test=reader-thumbs] img")); n != 3 {
		t.Errorf("%d thumbnails", n)
	}
	page.Keyboard.MustType('+')
	step().MustElement("[data-test=reader-paged].zoomed")
	page.Keyboard.MustType('0')
	page.Keyboard.MustType('d')
	step().MustElementR("[data-test=reader-page]", "^2 / 3$")
	step().MustElement("[data-test=mode-webtoon]").MustClick()
	step().MustElement("[data-test=reader-webtoon]")
	if n := len(step().MustElements("[data-test=reader-webtoon] img")); n != 3 {
		t.Errorf("%d pages in the strip", n)
	}

	// the metadata search fills a link's id
	metadataProvider = func(metadata.IDSource) (metadataSearcher, error) {
		return fakeSearcher{results: []metadata.Metadata{
			&mangadata.Metadata{EnglishTitle: "Berserk: The Prototype", ProviderID: "1", ProviderIDCode: "al"},
			&mangadata.Metadata{EnglishTitle: "Berserk", DateStart: metadata.Date{Year: 1989}, ProviderID: "30002", ProviderIDCode: "al"},
		}}, nil
	}
	t.Cleanup(func() { metadataProvider = defaultMetadataProvider })
	step().MustNavigate(s.URL + "/#/library")
	step().MustElementR("[data-test=library-table] a", "^Berserk$").MustClick()
	step().MustElementR("button", "^Links$").MustClick()
	step().MustElement("[data-test=search-link-al]").MustClick()
	step().MustElementR("[data-test=metadata-result]", "Berserk \\(1989\\)").MustElement("[data-test=metadata-pick]").MustClick()
	if v := step().MustElementX(`//button[@data-test="search-link-al"]/preceding-sibling::input`).MustProperty("value").String(); v != "30002" {
		t.Errorf("picked id %q", v)
	}
	step().MustElementR(".modal-footer button", "Cancel").MustClick()

	// merge the duplicates through the plan dialog, then undo it
	step().MustNavigate(s.URL + "/#/library/duplicates")
	step().MustElement("[data-test=merge-into-first]").MustClick()
	step().MustElement("[data-test=plan-lines]")
	step().MustElement("[data-test=plan-apply]").MustClick()
	step().MustElement("[data-test=plan-done]")
	if mangas, _ := s.api.services(context.Background()); mangas != nil {
		list, _ := mangas.Library.Mangas(context.Background(), core.MangasOptions{})
		if len(list.Mangas) != 1 {
			t.Errorf("%d mangas after the merge", len(list.Mangas))
		}
	}
	step().MustElement("[data-test=plan-dialog] .btn-close").MustClick()
	step().MustNavigate(s.URL + "/#/library/ops")
	step().MustElement("[data-test=undo]").MustClick()
	step().MustElement("[data-test=plan-apply]").MustClick()
	step().MustElement("[data-test=plan-done]")
	c, _ := s.api.services(context.Background())
	if list, _ := c.Library.Mangas(context.Background(), core.MangasOptions{}); len(list.Mangas) != 2 {
		t.Errorf("%d mangas after the undo", len(list.Mangas))
	}
}

// newE2EServer serves the API and the UI over a library of two folders of Berserk (the
// same AniList id), each with a chapter of 3 pages.
func newE2EServer(t *testing.T) *testServer {
	s := newTestServer(t, "")
	root := t.TempDir()
	var pageData bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 8, 12))
	img.Set(1, 1, color.White)
	png.Encode(&pageData, img)
	for i, dir := range []string{"Berserk", "Berserk (1989)"} {
		mangaDir := filepath.Join(root, dir)
		if err := os.MkdirAll(mangaDir, 0o755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(mangaDir, fmt.Sprintf("[000%d.0] Chapter %d.cbz", i+1, i+1))
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		for n := 1; n <= 3; n++ {
			w, _ := zw.Create(fmt.Sprintf("%04d.png", n))
			io.Copy(w, bytes.NewReader(pageData.Bytes()))
		}
		zw.Close()
		if err := os.WriteFile(file, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := s.src.library.RecordDownload(root, dir, storage.Download{Path: file, Provider: "fake", MangaID: fmt.Sprint("b", i),
			MangaTitle: "Berserk", Chapter: float32(i + 1), ChapterTitle: fmt.Sprint("Chapter ", i+1), Format: "CBZ", Pages: 3}); err != nil {
			t.Fatal(err)
		}
		if err := s.src.library.RememberMangaDir(root, dir, libmangal.MangaDirInfo{MetadataIDs: []metadata.ID{{Code: metadata.IDCodeAnilist, Raw: "30002"}}}); err != nil {
			t.Fatal(err)
		}
	}
	backups := t.TempDir()
	s.api.Core = func(m watch.Manager) (*core.Core, error) {
		return core.New(context.Background(), core.Env{
			Storage:    storage.Storage{Data: s.src.library.DB},
			FS:         afero.NewOsFs(),
			Root:       func() string { return root },
			Modes:      func() library.Modes { return library.DefaultModes },
			BackupsDir: backups,
			Loaders:    func() ([]libmangal.ProviderLoader, error) { return nil, nil },
			Watches:    func(context.Context) (watch.Manager, error) { return m, nil },
			Metadata:   func(context.Context, []metadata.ID) (metadata.Metadata, error) { return nil, nil },
		})
	}
	browser, err := web.NewBrowser(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	s.api.Browser = browser
	srv := httptest.NewUnstartedServer(nil)
	app, err := s.api.NewApp(AppOptions{Web: true, Guard: web.GuardOptions{Addr: srv.Listener.Addr()}})
	if err != nil {
		t.Fatal(err)
	}
	srv.Config.Handler = app
	srv.Start()
	t.Cleanup(srv.Close)
	s.Server = srv
	return s
}
