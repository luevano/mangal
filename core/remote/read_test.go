package remote

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadCache: the fetched chapters stay in the private cache whatever the server's
// names, a second read is the cached file, the oldest go past the limit.
func TestReadCache(t *testing.T) {
	fetched := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/library/mangas/1/file":
			fetched++
			w.Header().Set("Content-Type", "application/vnd.comicbook+zip")
			w.Header().Set("Content-Disposition", `attachment; filename="../../evil.sh"`)
			_, _ = w.Write([]byte(strings.Repeat("x", 600)))
		case "/api/v1/providers/p/pages":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"format":"ONLINE","pages":[{"index":0,"name":"0001.png"},{"index":1,"name":"../x"}]}`))
		case "/api/v1/providers/p/pages/0", "/api/v1/providers/p/pages/1":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("png"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := New(Options{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "remote")
	rc := ReadCache{Dir: dir, Max: 1000}
	ctx := t.Context()
	got, err := c.ChapterFile(ctx, rc, 1, "../../../Berserk/Vol 1/Ch 1.cbz")
	if err != nil || filepath.Dir(got) != dir || !strings.HasPrefix(filepath.Base(got), "Ch 1-") || !strings.HasSuffix(got, ".cbz") {
		t.Fatalf("file %q %v", got, err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("cache dir %v %v", info.Mode(), err)
	}
	if again, err := c.ChapterFile(ctx, rc, 1, "../../../Berserk/Vol 1/Ch 1.cbz"); err != nil || again != got || fetched != 1 {
		t.Errorf("again %q %v (fetched %d)", again, err, fetched)
	}
	online, err := c.OnlineChapter(ctx, rc, "p", "m", 1, 2, "../Berserk 2")
	if err != nil || filepath.Dir(online) != dir {
		t.Fatalf("online %q %v", online, err)
	}
	z, err := zip.OpenReader(online)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if len(z.File) != 2 || z.File[0].Name != "0001.png" || z.File[1].Name != "0002.jpg" {
		t.Errorf("pages %v", z.File)
	}
	// a second chapter file is over the limit: the oldest goes
	if _, err := c.ChapterFile(ctx, rc, 1, "Ch 2.cbz"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(got); !os.IsNotExist(err) {
		t.Errorf("the oldest is still there: %v", err)
	}
	if got := safeName("..\\../a/b"); strings.ContainsAny(got, `/\`) || strings.HasPrefix(got, ".") {
		t.Errorf("safe name %q", got)
	}
}
