package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/adrg/xdg"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/web/api"
)

func TestMain(m *testing.M) {
	// the clients open mangal's databases: keep them away from the user's
	dir, err := os.MkdirTemp("", "mangal-web-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	os.Setenv("MANGAL_AUTH_KEYRING", "false")
	xdg.DataHome = filepath.Join(dir, "data")
	xdg.StateHome = filepath.Join(dir, "state")
	findClosest = func(context.Context, string) (*anilist.Manga, metadata.Match, bool, error) {
		return nil, metadata.Match{}, false, nil
	}

	code := m.Run()
	_ = client.CloseAll()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type fakeLoader struct {
	id       string
	searches *atomic.Int32
}

func (l fakeLoader) String() string { return l.id }
func (l fakeLoader) Info() libmangal.ProviderInfo {
	return libmangal.ProviderInfo{
		ID: l.id, Name: l.id, Version: "0.1.0",
		History: []libmangal.ProviderAlias{{ID: l.id + "-old", Name: "old"}},
	}
}

func (l fakeLoader) Load(context.Context) (libmangal.Provider, error) {
	return &fakeProvider{info: l.Info(), searches: l.searches}, nil
}

type fakeProvider struct {
	libmangal.Provider
	info     libmangal.ProviderInfo
	searches *atomic.Int32
}

func (p *fakeProvider) Info() libmangal.ProviderInfo { return p.info }
func (p *fakeProvider) String() string               { return p.info.Name }
func (p *fakeProvider) SetLogger(*logger.Logger)     {}
func (p *fakeProvider) Close() error                 { return nil }

func (p *fakeProvider) SearchMangas(_ context.Context, query string) ([]mangadata.Manga, error) {
	p.searches.Add(1)
	return []mangadata.Manga{&fakeManga{info: mangadata.MangaInfo{ID: "m1", Title: query + " 1"}}}, nil
}

func (p *fakeProvider) MangaVolumes(_ context.Context, manga mangadata.Manga) ([]mangadata.Volume, error) {
	return []mangadata.Volume{fakeVolume{manga: manga, number: 1}, fakeVolume{manga: manga, number: 2}}, nil
}

func (p *fakeProvider) VolumeChapters(_ context.Context, volume mangadata.Volume) ([]mangadata.Chapter, error) {
	n := volume.Info().Number
	return []mangadata.Chapter{
		fakeChapter{volume: volume, number: n*10 + 1},
		fakeChapter{volume: volume, number: n*10 + 2},
	}, nil
}

type fakeManga struct {
	info mangadata.MangaInfo
	meta metadata.Metadata
}

func (m *fakeManga) String() string                     { return m.info.Title }
func (m *fakeManga) Info() mangadata.MangaInfo          { return m.info }
func (m *fakeManga) Metadata() metadata.Metadata        { return m.meta }
func (m *fakeManga) SetMetadata(meta metadata.Metadata) { m.meta = meta }

type fakeVolume struct {
	manga  mangadata.Manga
	number float32
}

func (v fakeVolume) String() string             { return fmt.Sprint(v.number) }
func (v fakeVolume) Info() mangadata.VolumeInfo { return mangadata.VolumeInfo{Number: v.number} }
func (v fakeVolume) Manga() mangadata.Manga     { return v.manga }

type fakeChapter struct {
	volume mangadata.Volume
	number float32
}

func (c fakeChapter) String() string { return fmt.Sprint(c.number) }
func (c fakeChapter) Info() mangadata.ChapterInfo {
	return mangadata.ChapterInfo{Number: c.number, Title: fmt.Sprint("chapter ", c.number)}
}
func (c fakeChapter) Volume() mangadata.Volume { return c.volume }

func newTestServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var searches atomic.Int32
	handler, err := newHandler(&Server{
		loaders:    []libmangal.ProviderLoader{fakeLoader{id: t.Name(), searches: &searches}},
		images:     newImageCache(1 << 20),
		httpClient: http.DefaultClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, &searches
}

// get requests path with the query and decodes the JSON response into v, returning the status.
func get(t *testing.T, server *httptest.Server, path string, query url.Values, v any) int {
	t.Helper()
	resp, err := http.Get(server.URL + path + "?" + query.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("%s: decode: %s", path, err)
	}
	return resp.StatusCode
}

func TestSearchTwice(t *testing.T) {
	server, searches := newTestServer(t)

	for i := range 2 {
		var mangas []api.Manga
		status := get(t, server, "/api/searchMangas", url.Values{"provider": {t.Name()}, "query": {"q"}}, &mangas)
		if status != http.StatusOK {
			t.Fatalf("request %d: status %d", i, status)
		}
		if len(mangas) != 1 || mangas[0].Id != "m1" || mangas[0].Title != "q 1" {
			t.Fatalf("request %d: %+v", i, mangas)
		}
	}
	if searches.Load() != 2 {
		t.Errorf("%d searches", searches.Load())
	}

	// an old id of the provider
	var mangas []api.Manga
	if status := get(t, server, "/api/searchMangas", url.Values{"provider": {t.Name() + "-old"}, "query": {"q"}}, &mangas); status != http.StatusOK {
		t.Errorf("old id: status %d", status)
	}
}

func TestNotFound(t *testing.T) {
	server, _ := newTestServer(t)

	tests := []struct {
		path  string
		query url.Values
	}{
		{"/api/searchMangas", url.Values{"provider": {"missing"}, "query": {"q"}}},
		{"/api/provider", url.Values{"id": {"missing"}}},
		{"/api/mangaPage", url.Values{"provider": {t.Name()}, "query": {"q"}, "manga": {"missing"}}},
		{"/api/volumeChapters", url.Values{"provider": {t.Name()}, "query": {"q"}, "manga": {"m1"}, "volume": {"9"}}},
	}
	for _, tt := range tests {
		var apiErr api.Error
		if status := get(t, server, tt.path, tt.query, &apiErr); status != http.StatusNotFound || apiErr.Code != http.StatusNotFound || apiErr.Message == "" {
			t.Errorf("%s: status %d, %+v", tt.path, status, apiErr)
		}
	}

	var apiErr api.Error
	if status := get(t, server, "/api/searchMangas", url.Values{"provider": {t.Name()}}, &apiErr); status != http.StatusBadRequest {
		t.Errorf("missing query: status %d, %+v", status, apiErr)
	}
}

func TestMangaPage(t *testing.T) {
	server, _ := newTestServer(t)

	var page api.MangaPage
	status := get(t, server, "/api/mangaPage", url.Values{"provider": {t.Name()}, "query": {"q"}, "manga": {"m1"}}, &page)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if page.Manga.Id != "m1" || len(page.Volumes) != 2 {
		t.Fatalf("%+v", page)
	}
	for i, v := range page.Volumes {
		if v.Volume.Number != float32(i+1) || len(v.Chapters) != 2 || v.Chapters[0].Number != float32((i+1)*10+1) {
			t.Errorf("volume %d: %+v", i, v)
		}
	}

	var chapters []api.Chapter
	status = get(t, server, "/api/volumeChapters", url.Values{"provider": {t.Name()}, "query": {"q"}, "manga": {"m1"}, "volume": {"2"}}, &chapters)
	if status != http.StatusOK || len(chapters) != 2 || chapters[1].Number != 22 {
		t.Errorf("volume chapters: status %d, %+v", status, chapters)
	}
}

func pngImage(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.Black)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImage(t *testing.T) {
	data := pngImage(t)
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/a.webp":
			if r.Referer() != "https://site.example/manga" {
				http.Error(w, "no referer", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "image/webp")
			w.Write(data)
		case "/sniffed":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(data)
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>cloudflare</html>"))
		case "/big":
			w.Header().Set("Content-Type", "image/png")
			w.Write(make([]byte, maxImageSize+1))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	server, _ := newTestServer(t)

	getImage := func(path, referer string) (*http.Response, []byte) {
		t.Helper()
		q := url.Values{"url": {upstream.URL + path}}
		if referer != "" {
			q.Set("referer", referer)
		}
		resp, err := http.Get(server.URL + "/api/image?" + q.Encode())
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body bytes.Buffer
		body.ReadFrom(resp.Body)
		return resp, body.Bytes()
	}

	for range 2 {
		resp, body := getImage("/a.webp", "https://site.example/manga")
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/webp" || !bytes.Equal(body, data) {
			t.Fatalf("status %d, type %q, %d bytes", resp.StatusCode, resp.Header.Get("Content-Type"), len(body))
		}
	}
	if hits.Load() != 1 {
		t.Errorf("not cached: %d upstream requests", hits.Load())
	}

	if resp, _ := getImage("/sniffed", ""); resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("sniffed: status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	for _, path := range []string{"/html", "/missing", "/big"} {
		before := hits.Load()
		for range 2 {
			if resp, _ := getImage(path, ""); resp.StatusCode != http.StatusBadGateway {
				t.Errorf("%s: status %d", path, resp.StatusCode)
			}
		}
		if hits.Load()-before != 2 {
			t.Errorf("%s: error cached", path)
		}
	}

	for _, bad := range []string{"file:///etc/passwd", "gopher://x", "/relative", "http://"} {
		resp, err := http.Get(server.URL + "/api/image?" + url.Values{"url": {bad}}.Encode())
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d", bad, resp.StatusCode)
		}
	}
}

func TestImageCache(t *testing.T) {
	c := newImageCache(10)
	c.add("a", cachedImage{data: make([]byte, 4)})
	c.add("b", cachedImage{data: make([]byte, 4)})
	c.get("a")
	c.add("c", cachedImage{data: make([]byte, 4)})
	if _, ok := c.get("b"); ok {
		t.Error("b not evicted")
	}
	if _, ok := c.get("a"); !ok {
		t.Error("a evicted")
	}
	c.add("huge", cachedImage{data: make([]byte, 11)})
	if _, ok := c.get("huge"); ok || c.used != 8 {
		t.Errorf("huge cached, used %d", c.used)
	}
}

func TestCheckHost(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	loopback := checkHost(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, ok)
	for host, want := range map[string]int{
		"127.0.0.1:6969": http.StatusOK,
		"localhost:6969": http.StatusOK,
		"[::1]:6969":     http.StatusOK,
		"evil.example":   http.StatusMisdirectedRequest,
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = host
		w := httptest.NewRecorder()
		loopback.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%s: %d", host, w.Code)
		}
	}
}
