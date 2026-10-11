package library

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
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
}

// ScanManga reconciles a library manga with its folder. A missing folder is looked for
// under root (the download directory, and its provider directories when providerDirs),
// recorded files that are gone are looked for in the folder before being forgotten, and
// chapter files nobody recorded are added.
func (s Scanner) ScanManga(m storage.Manga, root string, providerDirs bool) (ScanResult, error) {
	var res ScanResult
	folder := m.Path()
	exists, err := afero.DirExists(s.FS, folder)
	if err != nil {
		return res, err
	}
	if !exists {
		found, ok, err := s.locate(m, root, providerDirs)
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

	for _, dl := range recorded {
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

	info, _ := libmangal.ReadMangaDirInfo(s.FS, folder)
	for _, f := range files {
		if known[f.path] {
			continue
		}
		if !f.hasNumber {
			res.Unrecognized = append(res.Unrecognized, f.path)
			continue
		}
		err := s.Library.RecordDownload(m.Parent, m.Dir, storage.Download{
			Path: f.path, Provider: info.Provider, MangaID: info.Manga.ID, MangaTitle: m.Title,
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
// folders with a .libmangal.json that aren't in it yet.
func (s Scanner) Scan(root string, providerDirs bool) (ScanResult, error) {
	var res ScanResult
	mangas, err := s.Library.Mangas()
	if err != nil {
		return res, err
	}
	for _, m := range mangas {
		r, err := s.ScanManga(m, root, providerDirs)
		if err != nil {
			return res, err
		}
		res.add(r)
	}

	folders, err := s.markedFolders(root, providerDirs)
	if err != nil {
		return res, err
	}
	for _, folder := range folders {
		parent, dir := filepath.Dir(folder.path), filepath.Base(folder.path)
		if _, ok, err := s.Library.MangaByFolder(parent, dir); err != nil || ok {
			if err != nil {
				return res, err
			}
			continue
		}
		if err := s.Library.RememberMangaDir(parent, dir, folder.info); err != nil {
			return res, err
		}
		m, _, err := s.Library.MangaByFolder(parent, dir)
		if err != nil {
			return res, err
		}
		r, err := s.ScanManga(m, root, providerDirs)
		if err != nil {
			return res, err
		}
		res.add(r)
		res.New++
	}
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

type markedFolder struct {
	path string
	info libmangal.MangaDirInfo
}

// markedFolders are the folders with a .libmangal.json under root (and its provider directories).
func (s Scanner) markedFolders(root string, providerDirs bool) ([]markedFolder, error) {
	parents := []string{root}
	if providerDirs {
		entries, err := afero.ReadDir(s.FS, root)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				parents = append(parents, filepath.Join(root, e.Name()))
			}
		}
	}
	var folders []markedFolder
	for _, parent := range parents {
		entries, err := afero.ReadDir(s.FS, parent)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			path := filepath.Join(parent, e.Name())
			info, err := libmangal.ReadMangaDirInfo(s.FS, path)
			if err != nil {
				continue
			}
			folders = append(folders, markedFolder{path: path, info: info})
		}
	}
	return folders, nil
}

// locate finds the folder of a library manga under root by its .libmangal.json: same
// source, or else a shared metadata id (not another library manga's folder).
func (s Scanner) locate(m storage.Manga, root string, providerDirs bool) (string, bool, error) {
	folders, err := s.markedFolders(root, providerDirs)
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

// readZip counts the pages and reads the chapter from its ComicInfo.xml, if any.
func (s Scanner) readZip(f *chapterFile) {
	file, err := s.FS.Open(f.path)
	if err != nil {
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return
	}
	r, err := zip.NewReader(file.(io.ReaderAt), stat.Size())
	if err != nil {
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
			continue
		}
		var info struct {
			Title  string `xml:"Title"`
			Number string `xml:"Number"`
			Web    string `xml:"Web"`
		}
		err = xml.NewDecoder(rc).Decode(&info)
		_ = rc.Close()
		if err != nil {
			continue
		}
		if n, err := strconv.ParseFloat(strings.TrimSpace(info.Number), 32); err == nil {
			f.number, f.hasNumber = float32(n), true
		}
		f.title, f.url = info.Title, info.Web
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
