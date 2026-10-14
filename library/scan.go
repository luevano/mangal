package library

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// ScanResult is what a scan changed in the library.
type ScanResult struct {
	// Added, Moved and Removed are recorded chapter files.
	Added, Moved, Removed int
	// Relocated are manga folders found somewhere else, New are folders added to the library.
	Relocated, New int
	// Missing are library mangas whose folder wasn't found.
	Missing []storage.Manga
	// Unrecognized are files that look like chapters but have no chapter number.
	Unrecognized []string
	// Sources are the sources found for mangas without any (Library.BackfillSources).
	Sources storage.Backfill
}

func (r *ScanResult) add(o ScanResult) {
	r.Added += o.Added
	r.Moved += o.Moved
	r.Removed += o.Removed
	r.Relocated += o.Relocated
	r.New += o.New
	r.Missing = append(r.Missing, o.Missing...)
	r.Unrecognized = append(r.Unrecognized, o.Unrecognized...)
}

// Scanner reconciles the library with the files: the files are the truth.
type Scanner struct {
	FS      afero.Fs
	Library storage.Library
	// OnProgress, if set, is told where Scan is.
	OnProgress func(Progress)
}

// ScanManga reconciles a library manga with its folder. A missing folder is looked for
// under root (the download directory, and its provider directories when providerDirs),
// recorded files that are gone are looked for in the folder before being forgotten, and
// chapter files nobody recorded are added.
func (s Scanner) ScanManga(m storage.Manga, root string, providerDirs bool) (ScanResult, error) {
	return s.scanManga(m, root, providerDirs, &markedCache{}, s.OnProgress)
}

// markedCache keeps the marked folders under root for a whole scan, read when first needed.
type markedCache struct {
	folders []mangaFolder
	read    bool
}

func (s Scanner) marked(c *markedCache, root string, providerDirs bool) ([]mangaFolder, error) {
	if c.read {
		return c.folders, nil
	}
	folders, err := markedDirs(s.FS, root, providerDirs)
	if err != nil {
		return nil, err
	}
	c.folders, c.read = folders, true
	return folders, nil
}

// scanManga is ScanManga, its steps told to onProgress (nil within a whole scan, which
// counts the mangas).
func (s Scanner) scanManga(m storage.Manga, root string, providerDirs bool, marked *markedCache, onProgress func(Progress)) (ScanResult, error) {
	var res ScanResult
	folder := m.Path()
	exists, err := afero.DirExists(s.FS, folder)
	if err != nil {
		return res, err
	}
	if !exists {
		report(onProgress, Progress{Phase: "looking for its folder", Item: m.Title})
		found, ok, err := s.locate(m, root, providerDirs, marked)
		if err != nil {
			return res, err
		}
		if !ok {
			res.Missing = append(res.Missing, m)
			return res, nil
		}
		if err := s.Library.Moved(folder, found); err != nil {
			return res, err
		}
		res.Relocated++
		folder = found
		m.Parent, m.Dir = filepath.Dir(found), filepath.Base(found)
	}

	// without its own folder the chapters share it with other mangas, only check what's recorded
	var files []chapterFile
	if m.Dir != "" {
		report(onProgress, Progress{Phase: "reading the chapter files", Item: m.Title})
		if files, err = s.chapterFiles(folder); err != nil {
			return res, err
		}
	}
	recorded, err := s.Library.Downloads(m.ID)
	if err != nil {
		return res, err
	}
	known := map[string]bool{}
	for _, dl := range recorded {
		known[dl.Path] = true
	}

	for i, dl := range recorded {
		report(onProgress, Progress{Phase: "checking the recorded chapters", Done: i + 1, Total: len(recorded), Item: m.Title})
		if exists, err := afero.Exists(s.FS, dl.Path); err != nil {
			return res, err
		} else if exists {
			continue
		}
		if f, ok := movedTo(files, known, dl); ok {
			if err := s.Library.SetDownloadPath(dl.Path, f.path); err != nil {
				return res, err
			}
			known[f.path] = true
			res.Moved++
			continue
		}
		if err := s.Library.RemoveDownload(dl.Path); err != nil {
			return res, err
		}
		res.Removed++
	}

	provider, mangaID := s.sourceOf(m, folder, root, providerDirs)
	if slices.ContainsFunc(files, func(f chapterFile) bool { return !known[f.path] && f.hasNumber }) {
		report(onProgress, Progress{Phase: "adding the new chapters", Item: m.Title})
	}
	for _, f := range files {
		if known[f.path] {
			continue
		}
		if !f.hasNumber {
			res.Unrecognized = append(res.Unrecognized, f.path)
			continue
		}
		err := s.Library.RecordDownload(m.Parent, m.Dir, storage.Download{
			Path: f.path, Provider: provider, MangaID: mangaID, MangaTitle: m.Title,
			Chapter: f.number, ChapterTitle: f.title, ChapterURL: f.url,
			Format: f.format.String(), Pages: f.pages, Downloaded: f.modTime,
		})
		if err != nil {
			return res, err
		}
		res.Added++
	}
	return res, nil
}

// Scan reconciles the whole library with the files under root, and adds the manga
// folders that aren't in it yet: with a .libmangal.json, or else any folder with chapters
// (under the provider directories when providerDirs), guessed by guessInfo.
func (s Scanner) Scan(root string, providerDirs bool) (ScanResult, error) {
	return s.ScanContext(context.Background(), root, providerDirs)
}

// ScanContext is Scan, stopped when ctx is done.
func (s Scanner) ScanContext(ctx context.Context, root string, providerDirs bool) (ScanResult, error) {
	var res ScanResult
	mangas, err := s.Library.Mangas()
	if err != nil {
		return res, err
	}
	marked := &markedCache{}
	for i, m := range mangas {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		report(s.OnProgress, Progress{Phase: "scanning library", Done: i + 1, Total: len(mangas), Item: m.Title})
		r, err := s.scanManga(m, root, providerDirs, marked, nil)
		if err != nil {
			return res, err
		}
		res.add(r)
	}

	report(s.OnProgress, Progress{Phase: "looking for new manga folders"})
	folders, err := walkMangaDirs(s.FS, root, providerDirs)
	if err != nil {
		return res, err
	}
	for i, folder := range folders {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		parent, dir := filepath.Dir(folder.path), filepath.Base(folder.path)
		report(s.OnProgress, Progress{Phase: "looking for new manga folders", Done: i + 1, Total: len(folders), Item: dir})
		if _, ok, err := s.Library.MangaByFolder(parent, dir); err != nil || ok {
			if err != nil {
				return res, err
			}
			continue
		}
		info := folder.info
		if !folder.marked {
			if s.holdsMangas(folder.path) {
				continue
			}
			files, err := s.chapterFiles(folder.path)
			if err != nil {
				return res, err
			}
			if len(files) == 0 {
				continue
			}
			info = s.guessInfo(folder.path, files)
		}
		if err := s.Library.RememberMangaDir(parent, dir, info); err != nil {
			return res, err
		}
		m, _, err := s.Library.MangaByFolder(parent, dir)
		if err != nil {
			return res, err
		}
		r, err := s.scanManga(m, root, providerDirs, marked, nil)
		if err != nil {
			return res, err
		}
		res.add(r)
		res.New++
	}
	sources, err := s.Library.BackfillSources()
	if err != nil {
		return res, err
	}
	res.Sources = sources
	return res, nil
}

// OtherRoot is an existing directory (other than root) with the missing mangas' folders,
// e.g. the previous download path, empty if none.
func OtherRoot(fsys afero.Fs, missing []storage.Manga, root string, providerDirs bool) string {
	for _, m := range missing {
		dir := m.Parent
		if providerDirs {
			dir = filepath.Dir(dir)
		}
		if dir == filepath.Clean(root) {
			continue
		}
		if exists, _ := afero.DirExists(fsys, dir); exists {
			return dir
		}
	}
	return ""
}

// movedTo is an unrecorded file that's the recorded download moved: same name, or the
// same chapter number and format.
func movedTo(files []chapterFile, known map[string]bool, dl storage.Download) (chapterFile, bool) {
	for _, f := range files {
		if !known[f.path] && filepath.Base(f.path) == filepath.Base(dl.Path) {
			return f, true
		}
	}
	for _, f := range files {
		if !known[f.path] && f.hasNumber && f.number == dl.Chapter && f.format.String() == dl.Format {
			return f, true
		}
	}
	return chapterFile{}, false
}

// locate finds the folder of a library manga under root by its .libmangal.json: same
// source, or else a shared metadata id (not another library manga's folder).
func (s Scanner) locate(m storage.Manga, root string, providerDirs bool, marked *markedCache) (string, bool, error) {
	folders, err := s.marked(marked, root, providerDirs)
	if err != nil {
		return "", false, err
	}
	for _, f := range folders {
		for _, src := range m.Sources {
			if f.info.Provider == src.Provider && f.info.Manga.ID == src.MangaID {
				return f.path, true, nil
			}
		}
	}
	for _, f := range folders {
		if _, taken, err := s.Library.MangaByFolder(filepath.Dir(f.path), filepath.Base(f.path)); err != nil || taken {
			if err != nil {
				return "", false, err
			}
			continue
		}
		for _, id := range f.info.MetadataIDs {
			if slices.ContainsFunc(m.MetadataIDs, func(other metadata.ID) bool { return other.Source == id.Source && other.Raw == id.Raw }) {
				return f.path, true, nil
			}
		}
	}
	return "", false, nil
}

type chapterFile struct {
	path      string
	format    libmangal.Format
	hasNumber bool
	number    float32
	title     string
	series    string
	url       string
	pages     int
	modTime   time.Time
}

var (
	imageExts = []string{".jpg", ".jpeg", ".png", ".webp", ".gif", ".avif"}
	// the default chapter name template is "[%06.1f] title"
	bracketNumber = regexp.MustCompile(`\[(\d+(?:\.\d+)?)\]`)
	chapterNumber = regexp.MustCompile(`(?i)\bch(?:apter|\.)?\s*(\d+(?:\.\d+)?)`)
	notChapters   = []string{metadata.FilenameSeriesJSON, metadata.FilenameCoverJPG, metadata.FilenameBannerJPG}
)

// chapterFiles finds the chapter files of a manga folder (and its volume folders).
func (s Scanner) chapterFiles(folder string) ([]chapterFile, error) {
	var files []chapterFile
	err := s.walkChapters(folder, 0, &files)
	return files, err
}

func (s Scanner) walkChapters(dir string, depth int, files *[]chapterFile) error {
	entries, err := afero.ReadDir(s.FS, dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".part") || slices.Contains(notChapters, name) {
			continue
		}
		if e.IsDir() {
			images, err := s.countImages(path)
			if err != nil {
				return err
			}
			if images > 0 {
				f := chapterFile{path: path, format: libmangal.FormatImages, pages: images, modTime: e.ModTime()}
				f.number, f.hasNumber = numberFromName(name)
				*files = append(*files, f)
			} else if depth == 0 {
				// a volume folder
				if err := s.walkChapters(path, depth+1, files); err != nil {
					return err
				}
			}
			continue
		}
		format, ok := formatOf(name)
		if !ok {
			continue
		}
		f := chapterFile{path: path, format: format, modTime: e.ModTime()}
		if format == libmangal.FormatCBZ || format == libmangal.FormatZIP {
			s.readZip(&f)
		}
		if !f.hasNumber {
			f.number, f.hasNumber = numberFromName(name)
		}
		*files = append(*files, f)
	}
	return nil
}

func (s Scanner) countImages(dir string) (int, error) {
	entries, err := afero.ReadDir(s.FS, dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && slices.Contains(imageExts, strings.ToLower(filepath.Ext(e.Name()))) {
			n++
		}
	}
	return n, nil
}

// readZip counts the pages and reads the chapter from its ComicInfo.xml, if any. A file
// it can't read is logged, the chapter's number then comes from its name.
func (s Scanner) readZip(f *chapterFile) {
	warn := func(format string, a ...any) {
		log.Warn("scan %s: %s", f.path, fmt.Sprintf(format, a...))
	}
	file, err := s.FS.Open(f.path)
	if err != nil {
		warn("%s", err)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		warn("%s", err)
		return
	}
	readerAt, ok := file.(io.ReaderAt)
	if !ok {
		warn("can't be read as a zip")
		return
	}
	r, err := zip.NewReader(readerAt, stat.Size())
	if err != nil {
		warn("%s", err)
		return
	}
	for _, entry := range r.File {
		if slices.Contains(imageExts, strings.ToLower(filepath.Ext(entry.Name))) {
			f.pages++
			continue
		}
		if entry.Name != metadata.FilenameComicInfoXML {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			warn("%s: %s", entry.Name, err)
			continue
		}
		var info struct {
			Title  string `xml:"Title"`
			Series string `xml:"Series"`
			Number string `xml:"Number"`
			Web    string `xml:"Web"`
		}
		err = xml.NewDecoder(rc).Decode(&info)
		_ = rc.Close()
		if err != nil {
			warn("%s: %s", entry.Name, err)
			continue
		}
		if n, err := strconv.ParseFloat(strings.TrimSpace(info.Number), 32); err == nil {
			f.number, f.hasNumber = float32(n), true
		}
		f.title, f.series, f.url = info.Title, info.Series, info.Web
	}
}

func numberFromName(name string) (float32, bool) {
	for _, re := range []*regexp.Regexp{bracketNumber, chapterNumber} {
		if m := re.FindStringSubmatch(name); m != nil {
			if n, err := strconv.ParseFloat(m[1], 32); err == nil {
				return float32(n), true
			}
		}
	}
	return 0, false
}

func formatOf(name string) (libmangal.Format, bool) {
	lower := strings.ToLower(name)
	for _, format := range []libmangal.Format{libmangal.FormatTARGZ, libmangal.FormatCBZ, libmangal.FormatPDF, libmangal.FormatZIP, libmangal.FormatTAR} {
		if strings.HasSuffix(lower, format.Extension()) {
			return format, true
		}
	}
	return 0, false
}
