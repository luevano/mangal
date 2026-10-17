package serve

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/provider/install"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/web/apiv1"
	"github.com/spf13/afero"
)

type mirrorLoader struct{ readerLoader }

func (mirrorLoader) Info() libmangal.ProviderInfo {
	return libmangal.ProviderInfo{ID: "mirrored", Name: "Mirrored", Version: "1.0.0", Website: "https://a.example/",
		Mirrors: []string{"https://b.example/"}}
}

// TestV1ProviderMirror: a mirror of the provider is set (the write scope), anything else
// refused, the website goes back to the default.
func TestV1ProviderMirror(t *testing.T) {
	s := newTestServer(t, "")
	s.api.Core = func(m watch.Manager) (*core.Core, error) {
		return core.New(context.Background(), core.Env{
			Storage: storage.Storage{Data: s.src.library.DB},
			Loaders: func() ([]libmangal.ProviderLoader, error) { return []libmangal.ProviderLoader{mirrorLoader{}}, nil },
			Watches: func(context.Context) (watch.Manager, error) { return m, nil },
		})
	}
	saved := maps.Clone(config.Providers.Mirrors.Get())
	t.Cleanup(func() { config.Providers.Mirrors.Set(saved) })
	var forgot []string
	setMirror = func(id, mirror string) error {
		m := maps.Clone(config.Providers.Mirrors.Get())
		if mirror == "" {
			delete(m, id)
		} else {
			m[id] = mirror
		}
		return config.Providers.Mirrors.Set(m)
	}
	forgetMirror = func(id string) error { forgot = append(forgot, id); return nil }
	t.Cleanup(func() { setMirror, forgetMirror = config.SetMirror, defaultForgetMirror })

	var p apiv1.Provider
	if code := s.call(t, "PUT", "/api/v1/providers/mirrored/mirror", `{"mirror":"https://b.example/"}`, &p); code != http.StatusOK ||
		deref(p.Preferred) != "https://b.example/" || len(deref(p.Mirrors)) != 1 || len(forgot) != 1 {
		t.Fatalf("set %d %+v %v", code, p, forgot)
	}
	var e ErrorResponse
	if code := s.call(t, "PUT", "/api/v1/providers/mirrored/mirror", `{"mirror":"https://evil.example/"}`, &e); code != http.StatusBadRequest {
		t.Errorf("not a mirror: %d %+v", code, e)
	}
	if code := s.call(t, "PUT", "/api/v1/providers/mirrored/mirror", `{"mirror":"https://a.example/"}`, &p); code != http.StatusOK ||
		deref(p.Preferred) != "https://a.example/" || config.Providers.Mirrors.Get()["mirrored"] != "" {
		t.Errorf("website %d %+v", code, p)
	}
	if code := s.call(t, "PUT", "/api/v1/providers/nope/mirror", `{"mirror":""}`, &e); code != http.StatusNotFound {
		t.Errorf("unknown provider: %d", code)
	}
	// no config file: refused with how to make one, not a 500
	setMirror = func(string, string) error {
		return fmt.Errorf("write config key %q: %w", "providers.mirrors", config.ErrNoFile)
	}
	if code := s.call(t, "PUT", "/api/v1/providers/mirrored/mirror", `{"mirror":"https://b.example/"}`, &e); code != http.StatusConflict ||
		!strings.Contains(e.Error, "mangal config write") {
		t.Errorf("no config file: %d %+v", code, e)
	}
}

// TestV1ProvidersUpdate: each provider directory's result, the failures said.
func TestV1ProvidersUpdate(t *testing.T) {
	s := newTestServer(t, "")
	updateProviders = func(context.Context) ([]install.UpdateResult, error) {
		return []install.UpdateResult{{Dir: "saturno", Status: install.StatusUpdated}, {Dir: "local", Status: install.StatusSkipped},
			{Dir: "broken", Status: install.StatusFailed, Err: errors.New("non-fast-forward")}}, nil
	}
	t.Cleanup(func() { updateProviders = defaultUpdateProviders })
	var out apiv1.ProvidersUpdate
	if code := s.call(t, "POST", "/api/v1/providers/update", "", &out); code != http.StatusOK || len(out.Providers) != 3 ||
		out.Providers[0].Status != "updated" || deref(out.Providers[2].Error) != "non-fast-forward" {
		t.Fatalf("update %d %+v", code, out)
	}
}

// TestV1ProviderScopes: mirrors and updates need the write scope.
func TestV1ProviderScopes(t *testing.T) {
	s := newTestServer(t, "")
	s.api.Auth.Tokens = func() ([]cache.ServeTokenEntry, error) {
		return []cache.ServeTokenEntry{{Name: "reader", Hash: HashToken("r3ad"), Scopes: []string{ScopeRead}}}, nil
	}
	s.key = "r3ad"
	for _, c := range []struct{ method, path, body string }{
		{"POST", "/api/v1/providers/update", ""},
		{"PUT", "/api/v1/providers/x/mirror", `{"mirror":""}`},
	} {
		if code := s.call(t, c.method, c.path, c.body, nil); code != http.StatusForbidden {
			t.Errorf("%s %s without write: %d", c.method, c.path, code)
		}
	}
}

type fakeSearcher struct{ results []lmmeta.Metadata }

func (f fakeSearcher) SearchByIDContext(_ context.Context, id int) (lmmeta.Metadata, bool, error) {
	for _, m := range f.results {
		if m.ID().Value() == id {
			return m, true, nil
		}
	}
	return nil, false, nil
}

func (f fakeSearcher) SearchContext(context.Context, string) ([]lmmeta.Metadata, error) {
	return f.results, nil
}

func (f fakeSearcher) FindClosestContext(_ context.Context, q lmmeta.Query) (lmmeta.Match, bool, error) {
	return lmmeta.Match{Metadata: f.results[1], Score: lmmeta.Score(q, f.results[1])}, true, nil
}

// TestV1MetadataSearch: the closest match first, then the other results scored; an id
// query gets that manga; the sources list says which can be searched.
func TestV1MetadataSearch(t *testing.T) {
	s := newTestServer(t, "")
	opm := &mangadata.Metadata{EnglishTitle: "One-Punch Man", DateStart: lmmeta.Date{Year: 2012}, PublicationFormat: "MANGA",
		PublicationStatus: "RELEASING", CoverImage: "https://img/opm.jpg", ProviderID: "74347", ProviderIDCode: "al", AuthorList: []string{"ONE"}}
	other := &mangadata.Metadata{EnglishTitle: "Mob Psycho 100", DateStart: lmmeta.Date{Year: 2012}, ProviderID: "86123", ProviderIDCode: "al"}
	metadataProvider = func(source lmmeta.IDSource) (metadataSearcher, error) {
		if source != lmmeta.IDSourceAnilist {
			return nil, errors.New("kitsu isn't enabled (metadata.kitsu.enabled)")
		}
		return fakeSearcher{results: []lmmeta.Metadata{other, opm}}, nil
	}
	t.Cleanup(func() { metadataProvider = defaultMetadataProvider })

	var results []apiv1.MetadataResult
	if code := s.call(t, "GET", "/api/v1/metadata/anilist/search?q="+url.QueryEscape("one punch man"), "", &results); code != http.StatusOK || len(results) != 2 {
		t.Fatalf("search %d %+v", code, results)
	}
	if r := results[0]; r.Id != "74347" || r.Bad || deref(r.Year) != 2012 || deref(r.Format) != "MANGA" || deref(r.Status) != "releasing" ||
		deref(r.Cover) == "" || r.Url != "https://anilist.co/manga/74347" || len(deref(r.Authors)) != 1 {
		t.Errorf("closest %+v", r)
	}
	if !results[1].Bad {
		t.Errorf("a bad match isn't said: %+v", results[1])
	}
	if code := s.call(t, "GET", "/api/v1/metadata/al/search?q=id:86123", "", &results); code != http.StatusOK || len(results) != 1 || results[0].Title != "Mob Psycho 100" {
		t.Errorf("by id %d %+v", code, results)
	}
	var e ErrorResponse
	if code := s.call(t, "GET", "/api/v1/metadata/kitsu/search?q=x", "", &e); code != http.StatusUnprocessableEntity || !strings.Contains(e.Error, "kitsu") {
		t.Errorf("disabled source %d %+v", code, e)
	}
	if code := s.call(t, "GET", "/api/v1/metadata/nope/search?q=x", "", &e); code != http.StatusBadRequest {
		t.Errorf("unknown source %d", code)
	}
	var sources []apiv1.MetadataSource
	if code := s.call(t, "GET", "/api/v1/metadata/sources", "", &sources); code != http.StatusOK || len(sources) != 3 ||
		!sources[0].Enabled || sources[1].Enabled || sources[1].Reason == nil {
		t.Errorf("sources %d %+v", code, sources)
	}
}

func pngOf(t *testing.T, w, h int) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestV1ReaderExtras: the pages have their sizes, the manga downloads as one zip.
func TestV1ReaderExtras(t *testing.T) {
	s, fsys := newReaderServer(t)
	afero.WriteFile(fsys, "/lib/One-Punch Man/2/0002.png", pngOf(t, 1600, 1200), 0o644)
	var pages apiv1.ChapterPages
	if code := s.call(t, "GET", "/api/v1/library/mangas/1/pages?path="+url.QueryEscape("/lib/One-Punch Man/2"), "", &pages); code != http.StatusOK || len(pages.Pages) != 2 {
		t.Fatalf("pages %d %+v", code, pages)
	}
	if p := pages.Pages[1]; deref(p.Width) != 1600 || deref(p.Height) != 1200 || pages.Pages[0].Width != nil {
		t.Errorf("sizes %+v %+v", pages.Pages[0], p)
	}

	// a download whose file is gone is left out
	if err := s.src.library.RecordDownload("/lib", "One-Punch Man", storage.Download{Path: "/lib/One-Punch Man/9.cbz", Chapter: 9, Format: "CBZ",
		Provider: "fake", MangaID: "opm", MangaTitle: "One-Punch Man"}); err != nil {
		t.Fatal(err)
	}
	code, h, body := s.get(t, "/api/v1/library/mangas/1/zip")
	if code != http.StatusOK || h.Get("Content-Type") != "application/zip" || !strings.Contains(h.Get("Content-Disposition"), "One-Punch Man.zip") {
		t.Fatalf("zip %d %v %s", code, h, body)
	}
	r, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	if strings.Join(names, ",") != "1.cbz,2.zip,3.pdf" {
		t.Errorf("zip files %v", names)
	}
	if code, _, _ := s.get(t, "/api/v1/library/mangas/99/zip"); code != http.StatusNotFound {
		t.Errorf("unknown manga %d", code)
	}
}
