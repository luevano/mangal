// Package pages reads the pages of a downloaded chapter straight from its file (CBZ, ZIP,
// TAR, TAR.GZ or a folder of images), without extracting it, for the web reader.
package pages

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/spf13/afero"
)

// ErrNotFound is a page that isn't there.
var ErrNotFound = errors.New("no such page")

// ErrNoPages is a chapter whose pages can't be read one by one (a PDF).
var ErrNoPages = errors.New("a pdf has no pages to read one by one")

// Page is a page of a chapter file.
type Page struct {
	// Name is its name in the archive or folder.
	Name string
	// Size in bytes (uncompressed).
	Size int64

	// where a plain tar has its data
	offset int64
}

// Type is the page's content type, from its name.
func (p Page) Type() string {
	t := mime.TypeByExtension(strings.ToLower(path.Ext(p.Name)))
	if t == "" {
		return "application/octet-stream"
	}
	return t
}

// Chapter is a chapter's file and its pages, in reading order (by name, as libmangal
// numbers them).
type Chapter struct {
	FS     afero.Fs
	Path   string
	Format libmangal.Format
	Pages  []Page
}

// images are the extensions of the pages (raster images browsers show).
var images = []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif", ".bmp", ".jxl"}

func isImage(name string) bool {
	return slices.Contains(images, strings.ToLower(path.Ext(name)))
}

// FormatOf is the format of the chapter at p: a folder is images, a file by its extension.
func FormatOf(fsys afero.Fs, p string) (libmangal.Format, error) {
	info, err := fsys.Stat(p)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return libmangal.FormatImages, nil
	}
	if f, ok := libmangal.FormatOf(p); ok {
		return f, nil
	}
	return 0, fmt.Errorf("%s: not a chapter file (%s)", filepath.Base(p), strings.Join(libmangal.FormatStrings(), ", "))
}

// Open lists the pages of the chapter at p. A PDF has none (ErrNoPages): it's read whole.
func Open(fsys afero.Fs, p string) (*Chapter, error) {
	format, err := FormatOf(fsys, p)
	if err != nil {
		return nil, err
	}
	c := &Chapter{FS: fsys, Path: p, Format: format}
	switch format {
	case libmangal.FormatPDF:
		return c, nil
	case libmangal.FormatImages:
		entries, err := afero.ReadDir(fsys, p)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() && isImage(e.Name()) {
				c.Pages = append(c.Pages, Page{Name: e.Name(), Size: e.Size()})
			}
		}
	case libmangal.FormatCBZ, libmangal.FormatZIP:
		err = c.withZip(func(r *zip.Reader) error {
			for _, f := range r.File {
				if !f.FileInfo().IsDir() && isImage(f.Name) {
					c.Pages = append(c.Pages, Page{Name: f.Name, Size: int64(f.UncompressedSize64)})
				}
			}
			return nil
		})
	case libmangal.FormatTAR, libmangal.FormatTARGZ:
		err = c.walkTar(func(h *tar.Header, offset int64, _ io.Reader) (bool, error) {
			c.Pages = append(c.Pages, Page{Name: h.Name, Size: h.Size, offset: offset})
			return false, nil
		})
	default:
		return nil, fmt.Errorf("format %s has no pages", format)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(p), err)
	}
	slices.SortStableFunc(c.Pages, func(a, b Page) int { return strings.Compare(a.Name, b.Name) })
	return c, nil
}

func (c *Chapter) withZip(fn func(*zip.Reader) error) error {
	f, err := c.FS.Open(c.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	r, err := zip.NewReader(f, info.Size())
	if err != nil {
		return err
	}
	return fn(r)
}

// countingReader counts what was read, the offset of a plain tar's entries.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// walkTar calls fn with every image entry (its data's offset in a plain tar) until it
// says it's done.
func (c *Chapter) walkTar(fn func(h *tar.Header, offset int64, data io.Reader) (done bool, err error)) error {
	f, err := c.FS.Open(c.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	var r io.Reader = f
	if c.Format == libmangal.FormatTARGZ {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		r = gz
	}
	counter := &countingReader{r: r}
	tr := tar.NewReader(counter)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg || !isImage(h.Name) {
			continue
		}
		done, err := fn(h, counter.n, tr)
		if err != nil || done {
			return err
		}
	}
}

// Read is the i-th page (0-based): its content, type and size. The caller closes it.
func (c *Chapter) Read(i int) (io.ReadCloser, Page, error) {
	if c.Format == libmangal.FormatPDF {
		return nil, Page{}, ErrNoPages
	}
	if i < 0 || i >= len(c.Pages) {
		return nil, Page{}, ErrNotFound
	}
	p := c.Pages[i]
	switch c.Format {
	case libmangal.FormatImages:
		f, err := c.FS.Open(filepath.Join(c.Path, p.Name))
		return f, p, err
	case libmangal.FormatCBZ, libmangal.FormatZIP:
		f, err := c.FS.Open(c.Path)
		if err != nil {
			return nil, p, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, p, err
		}
		r, err := zip.NewReader(f, info.Size())
		if err != nil {
			f.Close()
			return nil, p, err
		}
		// by name, not r.Open: an entry's name may not be a valid fs path (./0001.jpg)
		i := slices.IndexFunc(r.File, func(zf *zip.File) bool { return zf.Name == p.Name })
		if i < 0 {
			f.Close()
			return nil, p, ErrNotFound
		}
		entry, err := r.File[i].Open()
		if err != nil {
			f.Close()
			return nil, p, err
		}
		return readClosers{entry, f}, p, nil
	case libmangal.FormatTAR:
		f, err := c.FS.Open(c.Path)
		if err != nil {
			return nil, p, err
		}
		return readClosers{io.NewSectionReader(f, p.offset, p.Size), f}, p, nil
	case libmangal.FormatTARGZ:
		// no random access: read up to it (a chapter is a few MB)
		var data []byte
		err := c.walkTar(func(h *tar.Header, _ int64, r io.Reader) (bool, error) {
			if h.Name != p.Name {
				return false, nil
			}
			var err error
			data, err = io.ReadAll(r)
			return true, err
		})
		if err == nil && data == nil {
			err = ErrNotFound
		}
		if err != nil {
			return nil, p, err
		}
		return io.NopCloser(bytes.NewReader(data)), p, nil
	}
	return nil, p, fmt.Errorf("format %s has no pages", c.Format)
}

// ContentType is the type of the chapter's file as a download (a folder is sent as a zip).
func (c *Chapter) ContentType() string {
	switch c.Format {
	case libmangal.FormatPDF:
		return "application/pdf"
	case libmangal.FormatCBZ:
		return "application/vnd.comicbook+zip"
	case libmangal.FormatImages, libmangal.FormatZIP:
		return "application/zip"
	case libmangal.FormatTAR:
		return "application/x-tar"
	case libmangal.FormatTARGZ:
		return "application/gzip"
	}
	return "application/octet-stream"
}

// FileName is the name of the chapter's file as a download.
func (c *Chapter) FileName() string {
	name := filepath.Base(c.Path)
	if c.Format == libmangal.FormatImages {
		name += ".zip"
	}
	return name
}

// WriteFile writes the chapter's file to w: the file itself, a folder as a zip (stored,
// the images are compressed already).
func (c *Chapter) WriteFile(w io.Writer) error {
	if c.Format != libmangal.FormatImages {
		f, err := c.FS.Open(c.Path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	}
	zw := zip.NewWriter(w)
	for _, p := range c.Pages {
		if err := func() error {
			f, err := c.FS.Open(filepath.Join(c.Path, p.Name))
			if err != nil {
				return err
			}
			defer f.Close()
			out, err := zw.CreateHeader(&zip.FileHeader{Name: p.Name, Method: zip.Store})
			if err != nil {
				return err
			}
			_, err = io.Copy(out, f)
			return err
		}(); err != nil {
			return err
		}
	}
	return zw.Close()
}

type readClosers struct {
	io.Reader
	closer io.Closer
}

func (r readClosers) Close() error {
	var err error
	if c, ok := r.Reader.(io.Closer); ok {
		err = c.Close()
	}
	return errors.Join(err, r.closer.Close())
}
