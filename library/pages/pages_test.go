package pages

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"path/filepath"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/spf13/afero"
)

var files = map[string]string{"0002.png": "two", "0001.jpg": "one", "ComicInfo.xml": "<x/>", "0010.webp": "ten"}

// the pages in order, without the other files
var want = []string{"0001.jpg", "0002.png", "0010.webp"}

func writeZip(t *testing.T, fs afero.Fs, p string) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range files {
		f, _ := w.Create(name)
		f.Write([]byte(data))
	}
	w.Close()
	afero.WriteFile(fs, p, buf.Bytes(), 0o644)
}

func writeTar(t *testing.T, fs afero.Fs, p string, gz bool) {
	var buf bytes.Buffer
	var out io.Writer = &buf
	var g *gzip.Writer
	if gz {
		g = gzip.NewWriter(&buf)
		out = g
	}
	w := tar.NewWriter(out)
	for name, data := range files {
		w.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), Typeflag: tar.TypeReg})
		w.Write([]byte(data))
	}
	w.Close()
	if g != nil {
		g.Close()
	}
	afero.WriteFile(fs, p, buf.Bytes(), 0o644)
}

func TestOpen(t *testing.T) {
	fs := afero.NewMemMapFs()
	writeZip(t, fs, "/lib/m/[0001.0] a.cbz")
	writeZip(t, fs, "/lib/m/[0002.0] b.zip")
	writeTar(t, fs, "/lib/m/[0003.0] c.tar", false)
	writeTar(t, fs, "/lib/m/[0004.0] d.tar.gz", true)
	for name, data := range files {
		afero.WriteFile(fs, filepath.Join("/lib/m/[0005.0] e", name), []byte(data), 0o644)
	}
	afero.WriteFile(fs, "/lib/m/[0006.0] f.pdf", []byte("%PDF"), 0o644)

	for _, tc := range []struct {
		path   string
		format libmangal.Format
	}{
		{"/lib/m/[0001.0] a.cbz", libmangal.FormatCBZ},
		{"/lib/m/[0002.0] b.zip", libmangal.FormatZIP},
		{"/lib/m/[0003.0] c.tar", libmangal.FormatTAR},
		{"/lib/m/[0004.0] d.tar.gz", libmangal.FormatTARGZ},
		{"/lib/m/[0005.0] e", libmangal.FormatImages},
	} {
		c, err := Open(fs, tc.path)
		if err != nil {
			t.Fatalf("%s: %s", tc.path, err)
		}
		if c.Format != tc.format {
			t.Errorf("%s: format %s, want %s", tc.path, c.Format, tc.format)
		}
		if len(c.Pages) != len(want) {
			t.Fatalf("%s: %d pages, want %d", tc.path, len(c.Pages), len(want))
		}
		for i, name := range want {
			if c.Pages[i].Name != name {
				t.Errorf("%s: page %d is %s, want %s", tc.path, i, c.Pages[i].Name, name)
			}
			r, p, err := c.Read(i)
			if err != nil {
				t.Fatalf("%s: read %d: %s", tc.path, i, err)
			}
			data, _ := io.ReadAll(r)
			r.Close()
			if string(data) != files[name] || p.Size != int64(len(files[name])) {
				t.Errorf("%s: page %d is %q (%d), want %q", tc.path, i, data, p.Size, files[name])
			}
		}
		if _, _, err := c.Read(len(want)); err != ErrNotFound {
			t.Errorf("%s: read past the end: %v", tc.path, err)
		}
		var buf bytes.Buffer
		if err := c.WriteFile(&buf); err != nil || buf.Len() == 0 {
			t.Errorf("%s: write the file: %v", tc.path, err)
		}
	}

	c, err := Open(fs, "/lib/m/[0006.0] f.pdf")
	if err != nil || c.Format != libmangal.FormatPDF || len(c.Pages) != 0 {
		t.Fatalf("pdf: %v %v", c, err)
	}
	if _, _, err := c.Read(0); err != ErrNoPages {
		t.Errorf("pdf page: %v", err)
	}
	if c.ContentType() != "application/pdf" {
		t.Errorf("pdf type %s", c.ContentType())
	}

	// a folder downloads as a zip of its pages
	c, _ = Open(fs, "/lib/m/[0005.0] e")
	var buf bytes.Buffer
	c.WriteFile(&buf)
	r, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil || len(r.File) != len(want) || c.FileName() != "[0005.0] e.zip" {
		t.Errorf("folder zip: %v, %d files, %s", err, len(r.File), c.FileName())
	}

	if _, err := Open(fs, "/lib/m/missing.cbz"); err == nil {
		t.Error("a missing file opened")
	}
}
