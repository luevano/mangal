package web

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestIcon(t *testing.T) {
	png := pngImage(t)
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/icon.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(png)
		case "/favicon.ico":
			// no icon at the default place either
			http.NotFound(w, r)
		case "/page.html":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	dir := t.TempDir()
	s := &Server{httpClient: http.DefaultClient, iconDir: dir}
	ctx := t.Context()

	t.Run("stored then served from disk", func(t *testing.T) {
		hits.Store(0)
		for range 2 {
			data, contentType, err := s.Icon(ctx, upstream.URL+"/icon.png", upstream.URL)
			if err != nil || contentType != "image/png" || !bytes.Equal(data, png) {
				t.Fatalf("%q, %d bytes, %v", contentType, len(data), err)
			}
		}
		if hits.Load() != 1 {
			t.Errorf("%d requests", hits.Load())
		}
		if files, _ := filepath.Glob(filepath.Join(dir, "*.png")); len(files) != 1 {
			t.Errorf("files %v", files)
		}
		// another server (a restart) has it too
		other := &Server{httpClient: http.DefaultClient, iconDir: dir}
		if _, _, err := other.Icon(ctx, upstream.URL+"/icon.png", upstream.URL); err != nil || hits.Load() != 1 {
			t.Errorf("after a restart: %v, %d requests", err, hits.Load())
		}
	})

	t.Run("failure remembered", func(t *testing.T) {
		hits.Store(0)
		for range 2 {
			if _, _, err := s.Icon(ctx, "", upstream.URL+"/manga/"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("got %v", err)
			}
		}
		if hits.Load() != 1 {
			t.Errorf("%d requests", hits.Load())
		}
		// asked again once iconRetry passed
		failed, _ := filepath.Glob(filepath.Join(dir, "*.failed"))
		if len(failed) != 1 {
			t.Fatalf("files %v", failed)
		}
		old := time.Now().Add(-iconRetry - time.Minute)
		if err := os.Chtimes(failed[0], old, old); err != nil {
			t.Fatal(err)
		}
		s.Icon(ctx, "", upstream.URL+"/manga/")
		if hits.Load() != 2 {
			t.Errorf("not asked again: %d requests", hits.Load())
		}
	})

	t.Run("non-image refused", func(t *testing.T) {
		hits.Store(0)
		if _, _, err := s.Icon(ctx, upstream.URL+"/page.html", ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("got %v", err)
		}
		if files, _ := filepath.Glob(filepath.Join(dir, "*.html")); len(files) != 0 {
			t.Errorf("stored %v", files)
		}
	})
}
