package pages

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/spf13/afero"
)

var files = map[string]string{"0002.png": "two", "0001.jpg": "one", "ComicInfo.xml": "<x/>", "0010.webp": "ten", "p2.png": "p2", "p10.png": "p10"}

// the pages in order, without the other files
var want = []string{"0001.jpg", "0002.png", "0010.webp", "p2.png", "p10.png"}

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
	writeZip(t, fs, "/lib/m/[0007.0] g.epub")

	for _, tc := range []struct {
		path   string
		format libmangal.Format
	}{
		{"/lib/m/[0001.0] a.cbz", libmangal.FormatCBZ},
		{"/lib/m/[0002.0] b.zip", libmangal.FormatZIP},
		{"/lib/m/[0003.0] c.tar", libmangal.FormatTAR},
		{"/lib/m/[0004.0] d.tar.gz", libmangal.FormatTARGZ},
		{"/lib/m/[0005.0] e", libmangal.FormatImages},
		{"/lib/m/[0007.0] g.epub", libmangal.FormatEPUB},
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

// TestEpubSpine: an epub's pages are its spine's, whatever their names; what isn't shown
// (a thumbnail) isn't a page; without a usable spine they go by name.
func TestEpubSpine(t *testing.T) {
	write := func(p string, files map[string]string) afero.Fs {
		fs := afero.NewMemMapFs()
		var buf bytes.Buffer
		w := zip.NewWriter(&buf)
		for name, data := range files {
			f, _ := w.Create(name)
			f.Write([]byte(data))
		}
		w.Close()
		afero.WriteFile(fs, p, buf.Bytes(), 0o644)
		return fs
	}
	fs := write("/c.epub", map[string]string{
		"META-INF/container.xml": `<container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`,
		"OEBPS/content.opf": `<package><manifest>
			<item id="a" href="text/one.xhtml"/><item id="b" href="text/two.xhtml"/><item id="c" href="img/z%20cover.png"/>
			<item id="thumb" href="img/thumb.png"/></manifest>
			<spine><itemref idref="c"/><itemref idref="b"/><itemref idref="a"/><itemref idref="nope"/></spine></package>`,
		"OEBPS/text/one.xhtml": `<html><body><img src="../img/a2.png"/></body></html>`,
		"OEBPS/text/two.xhtml": `<html><body><svg><image xlink:href="../img/b1.png#x"/></svg><img src="http://site/x.png"/><img src="../img/z%20cover.png"/></body></html>`,
		"OEBPS/img/a2.png":     "a", "OEBPS/img/b1.png": "b", "OEBPS/img/z cover.png": "c", "OEBPS/img/thumb.png": "t",
	})
	c, err := Open(fs, "/c.epub")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range c.Pages {
		got = append(got, p.Name)
	}
	if want := []string{"OEBPS/img/z cover.png", "OEBPS/img/b1.png", "OEBPS/img/a2.png"}; !slices.Equal(got, want) {
		t.Errorf("pages %q, want %q", got, want)
	}

	fs = write("/d.epub", map[string]string{"META-INF/container.xml": "not xml", "p10.png": "x", "p2.png": "y"})
	if c, err = Open(fs, "/d.epub"); err != nil || len(c.Pages) != 2 || c.Pages[0].Name != "p2.png" {
		t.Errorf("no spine: %+v %v", c, err)
	}
}

func TestPageFormat(t *testing.T) {
	writeZip := func(p string, names ...string) {
		t.Helper()
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		w := zip.NewWriter(f)
		for _, name := range names {
			e, err := w.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if name == "META-INF/container.xml" {
				_, err = e.Write([]byte(`<container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`))
			} else if name == "OEBPS/content.opf" {
				_, err = e.Write([]byte(`<package><manifest><item id="n" href="nav.xhtml" media-type="application/xhtml+xml"/><item id="l" href="l.svg" media-type="image/svg+xml"/><item id="a" href="images/a" media-type="image/webp"/></manifest></package>`))
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	cases := []struct{ name, want string }{}
	add := func(name, want string, names ...string) {
		writeZip(filepath.Join(dir, name), names...)
		cases = append(cases, struct{ name, want string }{name, want})
	}
	add("webp.cbz", "webp", "0001.webp", "0002.WEBP", "ComicInfo.xml")
	add("jpeg.zip", "jpeg", "0001.jpg", "0002.jpeg")
	add("mixed.cbz", MixedFormat, "0001.webp", "0002.png")
	add("none.cbz", "", "ComicInfo.xml")
	// the manifest's types, not the names: its files could say otherwise
	add("book.epub", "webp", "META-INF/container.xml", "OEBPS/content.opf", "OEBPS/images/a.png")
	add("foreign.epub", "png", "OEBPS/images/a.png")

	if err := os.WriteFile(filepath.Join(dir, "doc.pdf"), []byte("%PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct{ name, want string }{"doc.pdf", ""})
	folder := filepath.Join(dir, "folder")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "0001.png"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct{ name, want string }{"folder", "png"})

	for _, c := range cases {
		got, err := PageFormat(afero.NewOsFs(), filepath.Join(dir, c.name))
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v, want %q", c.name, got, err, c.want)
		}
	}
	if _, err := PageFormat(afero.NewOsFs(), filepath.Join(dir, "doc.pdf.nope")); err == nil {
		t.Error("a missing file: no error")
	}
}
