package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestProvider: a server's provider browsed through its endpoints, the chapters listed
// once for the volumes and their chapters.
func TestProvider(t *testing.T) {
	listed := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch r.URL.Path {
		case "/api/v1/providers":
			_, _ = w.Write([]byte(`[{"id":"p","name":"P","version":"1"}]`))
		case "/api/v1/providers/p/search":
			_, _ = w.Write([]byte(`[{"id":"m1","title":"One"}]`))
		case "/api/v1/providers/p/chapters":
			listed++
			_, _ = w.Write([]byte(`[{"volume":1,"number":1,"title":"a","downloaded":false,"reads":0},{"volume":1,"number":2,"title":"b","downloaded":false,"reads":0},{"volume":2,"number":3,"title":"c","downloaded":false,"reads":0}]`))
		case "/api/v1/providers/p/pages":
			if q.Get("manga_id") != "m1" || q.Get("chapter") != "3" || q.Get("volume") != "2" {
				t.Errorf("pages %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"format": "ONLINE", "pages": []map[string]any{{"index": 0, "name": "0001.JPG"}}})
		case "/api/v1/providers/p/pages/0":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("img"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := New(Options{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	loaders, err := c.Loaders(ctx)
	if err != nil || len(loaders) != 1 || loaders[0].Info().ID != "p" {
		t.Fatalf("loaders %v %v", loaders, err)
	}
	p, _ := loaders[0].Load(ctx)
	mangas, err := p.SearchMangas(ctx, "one")
	if err != nil || len(mangas) != 1 || mangas[0].Info().ID != "m1" {
		t.Fatalf("search %v %v", mangas, err)
	}
	vols, err := p.MangaVolumes(ctx, mangas[0])
	if err != nil || len(vols) != 2 {
		t.Fatalf("volumes %v %v", vols, err)
	}
	chapters, err := p.VolumeChapters(ctx, vols[1])
	if err != nil || len(chapters) != 1 || chapters[0].Info().Number != 3 || listed != 1 {
		t.Fatalf("chapters %v %v (listed %d)", chapters, err, listed)
	}
	pages, err := p.ChapterPages(ctx, chapters[0])
	if err != nil || len(pages) != 1 || pages[0].Extension() != ".jpg" {
		t.Fatalf("pages %v %v", pages, err)
	}
	if img, err := p.GetPageImage(ctx, pages[0]); err != nil || string(img) != "img" {
		t.Errorf("image %q %v", img, err)
	}
}
