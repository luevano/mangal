package remote

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// ReadCache is where a remote frontend keeps the chapters it fetched to read: a private
// directory (0700), its oldest files removed once they're over Max bytes (0: no limit).
type ReadCache struct {
	Dir string
	Max int64
}

// chapterExts are the extensions of the chapter files the server sends, by content type.
var chapterExts = map[string]string{
	"application/pdf":               ".pdf",
	"application/vnd.comicbook+zip": ".cbz",
	"application/epub+zip":          ".epub",
	"application/zip":               ".zip",
	"application/x-tar":             ".tar",
	"application/gzip":              ".tar.gz",
}

var imageExt = regexp.MustCompile(`^\.[a-z0-9]{1,4}$`)

// decodeError is the server's JSON error of a body, nil if it isn't one.
func decodeError(body []byte) *apiv1.Error {
	var e apiv1.Error
	if json.Unmarshal(body, &e) != nil {
		return nil
	}
	return &e
}

// safeName is a file name from the server's (or a site's) text: letters, digits and a
// few marks, nothing that's a path or hidden.
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" -_.,()[]", r) {
			return r
		}
		return '_'
	}, s)
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80])
	}
	return cmp.Or(strings.Trim(s, ". "), "chapter")
}

// name is the cache's file for the key: base, a hash of the key (two keys, two files)
// and ext.
func name(key, base, ext string) string {
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%s-%x%s", safeName(base), sum[:4], ext)
}

// open is the cache's directory, made private.
func (rc ReadCache) open() (*os.Root, error) {
	if rc.Dir == "" {
		return nil, errors.New("no read cache directory")
	}
	if err := os.MkdirAll(rc.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("read cache: %w", err)
	}
	if err := os.Chmod(rc.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("read cache: %w", err)
	}
	root, err := os.OpenRoot(rc.Dir)
	if err != nil {
		return nil, fmt.Errorf("read cache: %w", err)
	}
	return root, nil
}

// cached is the file's path when it's in the cache already (it's then the newest).
func (rc ReadCache) cached(root *os.Root, file string) (string, bool) {
	info, err := root.Stat(file)
	// a directory is a chapter of plain images
	if err != nil || !info.IsDir() && (!info.Mode().IsRegular() || info.Size() == 0) {
		return "", false
	}
	now := time.Now()
	_ = root.Chtimes(file, now, now)
	return filepath.Join(rc.Dir, file), true
}

// write writes the file with fn (to a part file renamed once done), then prunes the
// cache keeping it.
func (rc ReadCache) write(root *os.Root, file string, fn func(io.Writer) error) (string, error) {
	part := file + ".part"
	f, err := root.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("read cache: %w", err)
	}
	err = fn(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = root.Rename(part, file)
	}
	if err != nil {
		_ = root.Remove(part)
		return "", err
	}
	rc.prune(root, file)
	return filepath.Join(rc.Dir, file), nil
}

// prune removes the oldest files until the cache fits in Max, keep stays.
func (rc ReadCache) prune(root *os.Root, keep string) {
	if rc.Max <= 0 {
		return
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return
	}
	type entry struct {
		info fs.FileInfo
		size int64
	}
	var files []entry
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		switch {
		case err != nil || strings.HasPrefix(e.Name(), ".part-"):
		case info.Mode().IsRegular():
			files = append(files, entry{info, info.Size()})
			total += info.Size()
		case info.IsDir():
			var size int64
			_ = fs.WalkDir(root.FS(), e.Name(), func(_ string, d fs.DirEntry, err error) error {
				if i, ierr := d.Info(); err == nil && ierr == nil && i.Mode().IsRegular() {
					size += i.Size()
				}
				return nil
			})
			files = append(files, entry{info, size})
			total += size
		}
	}
	slices.SortFunc(files, func(a, b entry) int { return a.info.ModTime().Compare(b.info.ModTime()) })
	for _, f := range files {
		if total <= rc.Max {
			return
		}
		if f.info.Name() == keep {
			continue
		}
		if root.RemoveAll(f.info.Name()) == nil {
			total -= f.size
		}
	}
}

// ChapterFile is the library manga's chapter file (its path as the server lists it)
// fetched into the cache, its path there.
func (c *Client) ChapterFile(ctx context.Context, rc ReadCache, manga int64, file string) (string, error) {
	root, err := rc.open()
	if err != nil {
		return "", err
	}
	defer root.Close()
	key := fmt.Sprintf("%s\x00library\x00%d\x00%s", c.URL, manga, file)
	base := path.Base(strings.ReplaceAll(file, `\`, "/"))
	base = strings.TrimSuffix(strings.TrimSuffix(base, ".gz"), path.Ext(strings.TrimSuffix(base, ".gz")))
	for _, ext := range chapterExts {
		if p, ok := rc.cached(root, name(key, base, ext)); ok {
			return p, nil
		}
	}
	r, err := c.API.GetChapterFile(ctx, manga, &apiv1.GetChapterFileParams{Path: file})
	if err != nil {
		return "", fmt.Errorf("mangal serve: %w", err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		return "", errorOf(r.StatusCode, body, decodeError(body))
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	ext, ok := chapterExts[ct]
	if !ok {
		return "", fmt.Errorf("mangal serve: a chapter file this mangal doesn't open (%q)", ct)
	}
	out := name(key, base, ext)
	return rc.write(root, out, func(w io.Writer) error {
		_, err := io.Copy(w, r.Body)
		return err
	})
}

// OnlineChapter is the provider's chapter, its pages fetched through the server, written
// in the format (read.format) by libmangal on this machine into the cache: its path there.
func (c *Client) OnlineChapter(ctx context.Context, rc ReadCache, provider, mangaID string, vol, number float32, title string, format libmangal.Format) (string, error) {
	root, err := rc.open()
	if err != nil {
		return "", err
	}
	defer root.Close()
	key := fmt.Sprintf("%s\x00online\x00%s\x00%s\x00%g\x00%g", c.URL, provider, mangaID, vol, number)
	out := name(key, title, format.Extension())
	if p, ok := rc.cached(root, out); ok {
		return p, nil
	}
	lc, err := libmangal.NewClientContext(ctx, Loader{c: c, info: libmangal.ProviderInfo{ID: provider, Name: provider, Version: "0.0.0"}},
		libmangal.DefaultClientOptions())
	if err != nil {
		return "", err
	}
	defer lc.Close()
	tmp, err := os.MkdirTemp(rc.Dir, ".part-")
	if err != nil {
		return "", fmt.Errorf("read cache: %w", err)
	}
	defer os.RemoveAll(tmp)
	options := libmangal.DefaultDownloadOptions()
	options.Format, options.Directory = format, tmp
	options.CreateMangaDir, options.ReuseMangaDir, options.Strict, options.SkipIfExists, options.SearchMetadata = false, false, false, false, false
	ch := &chapter{volume: &volume{manga: &manga{info: mangadata.MangaInfo{ID: mangaID, Title: title}}, info: mangadata.VolumeInfo{Number: vol}},
		info: mangadata.ChapterInfo{Title: title, Number: number}}
	got, err := lc.DownloadChapterContext(ctx, ch, options)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rc.Dir, filepath.Join(got.Directory, got.Filename))
	if err != nil {
		return "", fmt.Errorf("read cache: %w", err)
	}
	if err := root.Rename(rel, out); err != nil {
		return "", fmt.Errorf("read cache: %w", err)
	}
	rc.prune(root, out)
	return filepath.Join(rc.Dir, out), nil
}

// RecordRead records the read in the server's history of the token's user, the server
// syncs the trackers it has logins for.
func (c *Client) RecordRead(ctx context.Context, read storage.Read) error {
	body := apiv1.ReadRequest{Provider: opt(read.Provider), MangaId: opt(read.MangaID), MangaTitle: opt(read.MangaTitle),
		Volume: opt(read.Volume), Chapter: read.Chapter, ChapterTitle: opt(read.ChapterTitle), ChapterUrl: opt(read.ChapterURL),
		LibraryManga: opt(read.LibraryManga)}
	r, err := c.API.RecordReadWithResponse(ctx, body)
	if err != nil {
		return fmt.Errorf("mangal serve: %w", err)
	}
	_, err = answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	return err
}
