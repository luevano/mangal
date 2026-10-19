package library

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/spf13/afero"
)

// ChapterTitles are the titles of the chapters by number: of several with a number (one
// per scanlation group), the first with a title that says more than its number
// ("Chapter 1"), else the first with one.
func ChapterTitles(chapters []mangadata.Chapter) map[float32]string {
	titles := map[float32]string{}
	for _, c := range chapters {
		info := c.Info()
		if t, ok := titles[info.Number]; info.Title != "" && (!ok || onlyNumber(t, info.Number) && !onlyNumber(info.Title, info.Number)) {
			titles[info.Number] = info.Title
		}
	}
	return titles
}

var numberTitle = regexp.MustCompile(`(?i)^(chapter|chap\.?|ch\.?|episode|ep\.?)?\s*#?\s*([0-9.]+)$`)

// onlyNumber is true for a title that's only the chapter's number ("Chapter 1", "Ch. 1").
func onlyNumber(title string, number float32) bool {
	m := numberTitle.FindStringSubmatch(strings.TrimSpace(title))
	if m == nil {
		return false
	}
	n, err := strconv.ParseFloat(m[2], 32)
	return err == nil && float32(n) == number
}

// Retitler gives the downloaded chapters of a library manga the titles another source has
// for their numbers: the files get the names the chapter name template gives them and,
// zips with a ComicInfo.xml, its Title.
type Retitler struct {
	Files
	Loaders []libmangal.ProviderLoader
	// ChapterName is the chapter name template.
	ChapterName func(libmangal.ProviderInfo, mangadata.Chapter) string
}

// Retitle is a downloaded chapter's new title.
type Retitle struct {
	Download storage.Download
	Title    string
	// To is the file's new path, Download.Path when its name stays.
	To string
	// ComicInfo is set when the file's ComicInfo.xml gets the title too.
	ComicInfo bool
	// Skip is why it isn't retitled.
	Skip string
}

// RetitlePlan is what retitling a library manga's chapters does.
type RetitlePlan struct {
	Manga    storage.Manga
	Retitles []Retitle
	// Unmatched are the downloads without a title for their number.
	Unmatched []storage.Download
}

// PlanRetitle plans giving the manga's downloads the titles (by chapter number): those
// whose title is another one.
func (r Retitler) PlanRetitle(m storage.Manga, titles map[float32]string) (RetitlePlan, error) {
	if r.ChapterName == nil {
		return RetitlePlan{}, errors.New("no chapter name template")
	}
	plan := RetitlePlan{Manga: m}
	downloads, err := r.Library.Downloads(m.ID)
	if err != nil {
		return plan, err
	}
	taken := map[string]bool{}
	for _, dl := range downloads {
		title := titles[dl.Chapter]
		switch {
		case title == "":
			plan.Unmatched = append(plan.Unmatched, dl)
			continue
		case title == dl.ChapterTitle:
			continue
		}
		rt := Retitle{Download: dl, Title: title, To: dl.Path}
		renamed := dl
		renamed.ChapterTitle = title
		if name := chapterFileName(r.ChapterName, r.Loaders, renamed); name != "" {
			rt.To = filepath.Join(filepath.Dir(dl.Path), name)
		}
		if exists, err := afero.Exists(r.FS, dl.Path); err != nil {
			return plan, err
		} else if !exists {
			rt.Skip = "the file is missing (mangal library scan)"
			plan.Retitles = append(plan.Retitles, rt)
			continue
		}
		if rt.To != dl.Path {
			switch exists, err := afero.Exists(r.FS, rt.To); {
			case err != nil:
				return plan, err
			case exists && !sameFile(r.FS, dl.Path, rt.To):
				rt.Skip = "a file of that name exists"
			case taken[rt.To]:
				rt.Skip = "another chapter gets that name"
			}
			taken[rt.To] = true
		}
		if rt.ComicInfo, err = hasComicInfo(r.FS, dl); err != nil {
			return plan, err
		}
		plan.Retitles = append(plan.Retitles, rt)
	}
	return plan, nil
}

// ToDo are the retitles not skipped.
func (p RetitlePlan) ToDo() []Retitle {
	return slices.DeleteFunc(slices.Clone(p.Retitles), func(r Retitle) bool { return r.Skip != "" })
}

// Lines describe the plan.
func (p RetitlePlan) Lines() []string {
	var lines []string
	for _, r := range p.Retitles {
		lines = append(lines, r.Line())
		if r.To != r.Download.Path {
			lines = append(lines, fmt.Sprintf("  %s -> %s", filepath.Base(r.Download.Path), filepath.Base(r.To)))
		}
	}
	if len(p.Unmatched) != 0 {
		var numbers []string
		for _, dl := range p.Unmatched {
			n := stringutil.FormatFloat32(dl.Chapter)
			if !slices.Contains(numbers, n) {
				numbers = append(numbers, n)
			}
		}
		lines = append(lines, fmt.Sprintf("no title for chapters %s", strings.Join(numbers, ", ")))
	}
	return lines
}

// Line describes the retitle.
func (r Retitle) Line() string {
	line := fmt.Sprintf("%s (%s): %q -> %q", stringutil.FormatFloat32(r.Download.Chapter), r.Download.Format, r.Download.ChapterTitle, r.Title)
	switch {
	case r.Skip != "":
		line += ", skipped: " + r.Skip
	case r.ComicInfo:
		line += ", ComicInfo.xml too"
	}
	return line
}

// ApplyRetitleContext retitles the planned chapters, each one whole or not at all: a zip
// with a ComicInfo.xml is written anew, the original goes to the trash. onDone (may be
// nil) is told each retitle's index in plan.ToDo() and its error; it stops when ctx is
// done.
func (r Retitler) ApplyRetitleContext(ctx context.Context, plan RetitlePlan, onDone func(i int, err error)) error {
	todo := plan.ToDo()
	batch := newTrashBatch()
	for i, rt := range todo {
		if err := ctx.Err(); err != nil {
			return err
		}
		report(r.OnProgress, Progress{Phase: "retitling", Done: i + 1, Total: len(todo), Item: rt.Title})
		err := r.retitle(ctx, batch, rt)
		if onDone != nil {
			onDone(i, err)
		}
	}
	return nil
}

// retitle retitles one chapter, moving back what it moved when it fails.
func (r Retitler) retitle(ctx context.Context, batch string, rt Retitle) error {
	from := rt.Download.Path
	var undo []func() error
	fail := func(err error) error {
		for _, u := range slices.Backward(undo) {
			if uErr := u(); uErr != nil {
				return fmt.Errorf("%w (and moving it back: %w)", err, uErr)
			}
		}
		return err
	}
	mv := func(a, b string) error {
		// only the case changes, on a case-insensitive filesystem
		if sameFile(r.FS, a, b) {
			return renameCase(r.FS, r.Journal, a, b)
		}
		return moveFile(ctx, r.FS, r.Journal, a, b, r.Modes)
	}
	move := func(a, b string) error {
		if err := mv(a, b); err != nil {
			return err
		}
		undo = append(undo, func() error { return mv(b, a) })
		return nil
	}
	if rt.ComicInfo {
		// written in the trash: undoing the retitle moves it back there
		staged, err := freeName(r.FS, inTrash(r.Root, filepath.Join(batch, "retitled"), rt.To), false, nil)
		if err != nil {
			return err
		}
		if err := mkdirAll(r.FS, r.Journal, filepath.Dir(staged), r.Modes.Dir); err != nil {
			return err
		}
		if err := writeRetitledZip(r.FS, from, staged, rt.Title, r.Modes.File); err != nil {
			return errors.Join(err, removeIfExists(r.FS, staged))
		}
		trashed, err := freeName(r.FS, inTrash(r.Root, batch, from), false, nil)
		if err != nil {
			return errors.Join(err, r.FS.Remove(staged))
		}
		if err := move(from, trashed); err != nil {
			return errors.Join(err, r.FS.Remove(staged))
		}
		if err := move(staged, rt.To); err != nil {
			return fail(errors.Join(err, r.FS.Remove(staged)))
		}
	} else if rt.To != from {
		if err := move(from, rt.To); err != nil {
			return err
		}
	}
	if err := r.Library.Retitle(from, rt.To, rt.Title); err != nil {
		return fail(err)
	}
	return nil
}

func removeIfExists(fsys afero.Fs, path string) error {
	if err := fsys.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// hasComicInfo is true when the download is a zip with a ComicInfo.xml.
func hasComicInfo(fsys afero.Fs, dl storage.Download) (bool, error) {
	if dl.Format != libmangal.FormatCBZ.String() && dl.Format != libmangal.FormatZIP.String() {
		return false, nil
	}
	f, err := fsys.Open(dl.Path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	zr, err := zip.NewReader(f, info.Size())
	if err != nil {
		// not a zip after all: only renamed
		return false, nil
	}
	return slices.ContainsFunc(zr.File, func(f *zip.File) bool { return f.Name == metadata.FilenameComicInfoXML }), nil
}

// writeRetitledZip writes the zip from to the new file to, its ComicInfo.xml's Title set
// to title: the other entries are copied as they are, not recompressed.
func writeRetitledZip(fsys afero.Fs, from, to, title string, mode os.FileMode) error {
	in, err := fsys.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(in, info.Size())
	if err != nil {
		return fmt.Errorf("read %s: %w", from, err)
	}
	out, err := fsys.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	err = copyRetitled(zr, zw, title)
	if err == nil {
		err = zw.Close()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", to, err)
	}
	return nil
}

func copyRetitled(zr *zip.Reader, zw *zip.Writer, title string) error {
	for _, f := range zr.File {
		if f.Name != metadata.FilenameComicInfoXML {
			if err := zw.Copy(f); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		if data, err = setComicInfoTitle(data, title); err != nil {
			return err
		}
		fh := f.FileHeader
		fh.CRC32, fh.CompressedSize64, fh.UncompressedSize64 = 0, 0, 0
		w, err := zw.CreateHeader(&fh)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return zw.SetComment(zr.Comment)
}

// setComicInfoTitle is the ComicInfo.xml with its Title set, the rest byte for byte (a
// read and Marshal would drop what libmangal doesn't know or write).
func setComicInfoTitle(data []byte, title string) ([]byte, error) {
	var esc bytes.Buffer
	if err := xml.EscapeText(&esc, []byte(title)); err != nil {
		return nil, err
	}
	elem := "<Title>" + esc.String() + "</Title>"
	d := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	rootEnd := int64(-1)
	for {
		start := d.InputOffset()
		tok, err := d.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", metadata.FilenameComicInfoXML, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				rootEnd = d.InputOffset()
			}
			if depth != 2 || t.Name.Local != "Title" {
				continue
			}
			for depth > 1 {
				tok, err := d.RawToken()
				if err != nil {
					return nil, fmt.Errorf("read %s: %w", metadata.FilenameComicInfoXML, err)
				}
				switch tok.(type) {
				case xml.StartElement:
					depth++
				case xml.EndElement:
					depth--
				}
			}
			return slices.Concat(data[:start], []byte(elem), data[d.InputOffset():]), nil
		case xml.EndElement:
			depth--
		}
	}
	if rootEnd < 0 {
		return nil, fmt.Errorf("%s has no root element", metadata.FilenameComicInfoXML)
	}
	// Title is the schema's first element, indented as libmangal writes it
	return slices.Concat(data[:rootEnd], []byte("\n  "+elem), data[rootEnd:]), nil
}
