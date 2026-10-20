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

	"github.com/labstack/echo/v5"
)

const (
	svgIcon    = `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"><rect width="1" height="1" style="fill:red"/></svg>`
	scriptIcon = `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(document.domain)</script><image href="https://tracker.example/x.png"/></svg>`
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
		// asked again once iconRetry passed, not before
		failed, _ := filepath.Glob(filepath.Join(dir, "*.failed"))
		if len(failed) != 1 {
			t.Fatalf("files %v", failed)
		}
		old := time.Now().Add(-iconRetry["failed"] + time.Minute)
		if err := os.Chtimes(failed[0], old, old); err != nil {
			t.Fatal(err)
		}
		s.Icon(ctx, "", upstream.URL+"/manga/")
		if hits.Load() != 1 {
			t.Errorf("asked again: %d requests", hits.Load())
		}
		old = time.Now().Add(-iconRetry["failed"] - time.Minute)
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

func TestIconSVG(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/icon.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write([]byte(svgIcon))
		case "/script.svg":
			// a wrong type: the content decides
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte(scriptIcon))
		case "/fake.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write([]byte("<html><svg></svg></html>"))
		case "/broken.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect></svg>`))
		case "/text":
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write([]byte("just text"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	dir := t.TempDir()
	s := &Server{httpClient: http.DefaultClient, iconDir: dir}

	// served as the API does (its headers)
	e := echo.New()
	e.HTTPErrorHandler = APIError
	e.Use(ImageHeaders)
	e.GET("/api/icon", func(c *echo.Context) error {
		data, contentType, err := s.Icon(c.Request().Context(), c.QueryParam("url"), "")
		if err != nil {
			return err
		}
		return c.Blob(http.StatusOK, contentType, data)
	})
	server := httptest.NewServer(e)
	defer server.Close()

	for _, name := range []string{"icon.svg", "script.svg"} {
		t.Run(name, func(t *testing.T) {
			resp, err := http.Get(server.URL + "/api/icon?url=" + upstream.URL + "/" + name)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			h := resp.Header
			if resp.StatusCode != http.StatusOK || h.Get("Content-Type") != "image/svg+xml" {
				t.Fatalf("%s %q", resp.Status, h.Get("Content-Type"))
			}
			if csp := h.Get("Content-Security-Policy"); csp != "default-src 'none'; style-src 'unsafe-inline'; sandbox" {
				t.Errorf("csp %q", csp)
			}
			if h.Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("nosniff %q", h.Get("X-Content-Type-Options"))
			}
		})
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*.svg")); len(files) != 2 {
		t.Errorf("files %v", files)
	}
	// from disk after a restart
	other := &Server{httpClient: http.DefaultClient, iconDir: dir}
	upstream.Close()
	if data, contentType, err := other.Icon(t.Context(), upstream.URL+"/icon.svg", ""); err != nil || contentType != "image/svg+xml" || string(data) != svgIcon {
		t.Errorf("%q, %q, %v", contentType, data, err)
	}
}

func TestIconType(t *testing.T) {
	for _, c := range []struct {
		data string
		ok   bool
	}{
		{svgIcon, true},
		{scriptIcon, true},
		{"<html><svg></svg></html>", false},
		{`<svg xmlns="http://www.w3.org/2000/svg"><rect></svg>`, false},
		{"<svg></svg>", false}, // not in the SVG namespace
		{`<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///etc/passwd">]><svg xmlns="http://www.w3.org/2000/svg">&x;</svg>`, false},
		{"just text", false},
		{"", false},
	} {
		contentType, err := iconType("image/svg+xml", []byte(c.data))
		if (err == nil) != c.ok || (c.ok && contentType != "image/svg+xml") {
			t.Errorf("%.40q: %q, %v", c.data, contentType, err)
		}
		if !c.ok && !errors.Is(err, ErrUpstream) {
			t.Errorf("%.40q: %v", c.data, err)
		}
	}
	// the image proxy still refuses SVG
	if _, err := imageType("image/svg+xml", []byte(svgIcon)); err == nil {
		t.Error("image proxy took an SVG")
	}
}

func TestIconRetry(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/down.png":
			http.Error(w, "down", http.StatusServiceUnavailable)
		case "/text":
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write([]byte("just text"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	for _, c := range []struct{ name, url, ext string }{
		{"no icon", upstream.URL + "/icon.png", "failed"},
		{"not an image", upstream.URL + "/text", "failed"},
		{"5xx", upstream.URL + "/down.png", "unreachable"},
		// nothing listens there
		{"no answer", "http://127.0.0.1:1/icon.png", "unreachable"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			s := &Server{httpClient: http.DefaultClient, iconDir: dir}
			if _, _, err := s.Icon(t.Context(), c.url, ""); !errors.Is(err, ErrNotFound) {
				t.Fatalf("got %v", err)
			}
			files, _ := filepath.Glob(filepath.Join(dir, "*"))
			if len(files) != 1 || filepath.Ext(files[0]) != "."+c.ext {
				t.Fatalf("files %v", files)
			}
			// an mtime still old after a call: not asked again
			for _, step := range []struct {
				ago   time.Duration
				asked bool
			}{{iconRetry[c.ext] - time.Minute, false}, {iconRetry[c.ext] + time.Minute, true}} {
				old := time.Now().Add(-step.ago)
				if err := os.Chtimes(files[0], old, old); err != nil {
					t.Fatal(err)
				}
				s.Icon(t.Context(), c.url, "")
				fi, err := os.Stat(files[0])
				if err != nil {
					t.Fatal(err)
				}
				if asked := fi.ModTime().After(old.Add(time.Second)); asked != step.asked {
					t.Errorf("%s ago: asked %v", step.ago, asked)
				}
			}
		})
	}
}
