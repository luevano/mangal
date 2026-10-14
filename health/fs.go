package health

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/spf13/afero"
)

// fsData is what the fs checks read from the download directory.
type fsData struct {
	// missing are the library mangas whose folder is gone
	missing map[int64]bool
	// markers are the library mangas' .libmangal.json, markerErrs the broken ones
	markers    map[int64]*libmangal.MangaDirInfo
	markerErrs map[int64]error
	// folders are the folders under the download directory that may be mangas
	folders []library.Folder
	// otherRoot is another existing directory with the missing mangas' folders
	otherRoot string
	trash     []library.TrashBatch
}

// renameData is what library rename would do, with the cached metadata (fetched with the
// network checks); noMetadata are the folders it couldn't tell.
type renameData struct {
	renames    []library.Rename
	noMetadata int
}

// loadFS reads the library mangas' folders and markers, the folders under the download
// directory and the trash.
func (c Checker) loadFS(ctx context.Context, s *snapshot) error {
	if c.FS == nil {
		return errors.New("no filesystem")
	}
	d := &fsData{missing: map[int64]bool{}, markers: map[int64]*libmangal.MangaDirInfo{}, markerErrs: map[int64]error{}}
	var missing []storage.Manga
	for i, m := range s.mangas {
		if err := ctx.Err(); err != nil {
			return err
		}
		if i%20 == 0 {
			c.progress(fmt.Sprintf("reading the folders: %d/%d", i, len(s.mangas)))
		}
		if m.Dir == "" {
			continue
		}
		exists, err := afero.DirExists(c.FS, m.Path())
		if err != nil {
			return err
		}
		if !exists {
			d.missing[m.ID] = true
			missing = append(missing, m)
			continue
		}
		info, err := libmangal.ReadMangaDirInfo(c.FS, m.Path())
		switch {
		case err == nil:
			d.markers[m.ID] = &info
		case !errors.Is(err, fs.ErrNotExist):
			d.markerErrs[m.ID] = err
		}
	}
	var err error
	if c.Root != "" {
		if d.folders, err = library.Folders(c.FS, c.Root, c.ProviderDirs); err != nil {
			return err
		}
		d.otherRoot = library.OtherRoot(c.FS, missing, c.Root, c.ProviderDirs)
		if d.trash, err = library.Trash(c.FS, c.Root); err != nil {
			return err
		}
	}
	s.fs = d
	return nil
}

// loadRenames plans library rename with the cached metadata, fetched (batched) when
// network.
func (c Checker) loadRenames(ctx context.Context, s *snapshot, network bool) error {
	if c.FS == nil || c.Root == "" || c.MangaName == nil || c.ProviderName == nil || c.Metadata == nil {
		return errors.New("not available")
	}
	d := &renameData{}
	n := library.Namer{
		FS: c.FS, Loaders: c.Loaders, ProviderName: c.ProviderName, MangaName: c.MangaName, Library: c.Library,
		Metadata: func(ctx context.Context, ids [][]metadata.ID) ([]metadata.Metadata, error) {
			return c.Metadata(ctx, ids, network)
		},
		NeedMetadata: true,
		OnNoMetadata: func(string) { d.noMetadata++ },
		OnProgress:   func(p library.Progress) { c.progress(p.String()) },
	}
	var err error
	if d.renames, err = n.Plan(ctx, c.Root, c.ProviderDirs); err != nil {
		return err
	}
	s.renames = d
	return nil
}

// mangaByPath is the library manga of the folder, false if none.
func (s *snapshot) mangaByPath(path string) (storage.Manga, bool) {
	for _, m := range s.mangas {
		if m.Dir != "" && m.Path() == path {
			return m, true
		}
	}
	return storage.Manga{}, false
}

// L1: library mangas whose folder is gone, with the other download directory that has
// them when there's one.
func checkMissing(s *snapshot) []Finding {
	var out []Finding
	for _, m := range s.mangas {
		if !s.fs.missing[m.ID] {
			continue
		}
		f := Finding{Severity: Warning, Subject: mangaSubject(m), Message: "its folder is gone: " + s.rel(m.Path()),
			Fix: Fix{
				Commands: []string{"mangal library scan"},
				Note:     "finds folders that moved (by their .libmangal.json), then offers to forget the ones it didn't find",
				TUI:      tuiLibrary + ": scan, delete (forget it)",
				Action:   &Action{Kind: ActionForget, Manga: m.ID},
			}}
		if s.fs.otherRoot != "" {
			f.Details = []string{fmt.Sprintf("%s has the missing mangas' folders: is download.path (%s) right? the scan offers to switch", s.fs.otherRoot, s.root)}
		}
		out = append(out, f)
	}
	return out
}

// L2: library mangas' folders without a .libmangal.json, as one summary: what scan --mark
// would write into each (renames and downloads know a folder by it).
func checkUnmarked(s *snapshot) []Finding {
	var items []Finding
	counts := map[library.MarkEvidence]int{}
	var mark []int64
	none := 0
	files := library.Files{Root: s.root}
	for _, m := range s.mangas {
		if m.Dir == "" || s.fs.missing[m.ID] || s.fs.markers[m.ID] != nil || s.fs.markerErrs[m.ID] != nil {
			continue
		}
		info, evidence := files.PlanMark(m, s.mark)
		f := Finding{Check: "L2", Category: Library, Severity: Info, Subject: mangaSubject(m)}
		if evidence == "" {
			none++
			f.Message = "no .libmangal.json, nothing to make one from (no source, provider directory nor metadata id)"
			f.Fix = Fix{Commands: []string{fmt.Sprintf("mangal library bind %d", m.ID)},
				Note: "link it (or download a chapter of it from a provider), then mark it", TUI: tuiLibrary + ": links"}
		} else {
			counts[evidence]++
			mark = append(mark, m.ID)
			as := info.Provider
			if as == "" {
				as = "no provider"
			}
			if info.Manga.ID != "" {
				as += " " + info.Manga.ID
			}
			f.Message = fmt.Sprintf("no .libmangal.json: would mark it as %s (%s)", as, evidence)
			f.Fix = Fix{Commands: []string{fmt.Sprintf("mangal library mark %d --apply", m.ID)}, TUI: "health: apply fix",
				Action: &Action{Kind: ActionMark, Manga: m.ID}}
		}
		items = append(items, f)
	}
	if len(items) == 0 {
		return nil
	}
	f := Finding{Severity: Warning, Subject: Subject{Kind: "library", Name: "library"},
		Message: fmt.Sprintf("%s without a .libmangal.json: %d can be marked, %d with nothing to go by", plural(len(items), "folder"), len(mark), none),
		Details: []string{
			"library rename skips them, downloads and scans may not recognize them",
			fmt.Sprintf("would mark %d from the library source, %d provider from its directory, %d metadata ids only",
				counts[library.MarkedFromSource], counts[library.MarkedFromDir], counts[library.MarkedFromIDs]),
		},
		Items: items,
		Fix: Fix{
			Commands: []string{"mangal library scan --mark", "mangal library scan --mark --apply"},
			Note:     "lists what it would write, --apply writes them (undo: mangal library undo); mangal health --only L2 lists the folders",
			TUI:      "health: open the finding, apply fix",
		}}
	if len(mark) == 0 {
		f.Severity = Info
		f.Fix.Commands = nil
		f.Fix.Note = "link them (mangal library bind) or download from a provider; mangal health --only L2 lists them"
	} else {
		f.Fix.Action = &Action{Kind: ActionMark, Mangas: mark}
	}
	return []Finding{f}
}

// L9: folders under the download directory that aren't in the library (scan adds them).
func checkUntracked(s *snapshot) []Finding {
	var items []Finding
	for _, folder := range s.fs.folders {
		if _, ok := s.mangaByPath(folder.Path); ok {
			continue
		}
		if folder.Marker == nil && folder.MarkerErr == nil && !hasChapters(s, folder.Path) {
			continue
		}
		items = append(items, Finding{Check: "L9", Category: Library, Severity: Info, Subject: Subject{Kind: "folder", Name: s.rel(folder.Path)},
			Message: "not in the library", Fix: Fix{Commands: []string{"mangal library scan"}, Action: &Action{Kind: ActionScan}}})
	}
	if len(items) == 0 {
		return nil
	}
	f := Finding{Severity: Warning, Subject: Subject{Kind: "library", Name: "library"},
		Message: fmt.Sprintf("%s aren't in the library", plural(len(items), "manga folder")), Items: items,
		Fix: Fix{Commands: []string{"mangal library scan"}, Note: "adds them (and finds moved folders)", TUI: tuiLibrary + ": scan",
			Action: &Action{Kind: ActionScan}}}
	if len(items) == 1 {
		f.Message = "1 manga folder isn't in the library: " + items[0].Subject.Name
	}
	for i, item := range items {
		if i == 10 {
			f.Details = append(f.Details, fmt.Sprintf("... %d more (mangal health --only L9)", len(items)-10))
			break
		}
		f.Details = append(f.Details, item.Subject.Name)
	}
	return []Finding{f}
}

// hasChapters is true when the folder has a chapter file or folder (a cheap look, as
// scan would adopt it).
func hasChapters(s *snapshot, dir string) bool {
	entries, err := afero.ReadDir(s.fsys, dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			return true
		}
		for _, ext := range []string{".cbz", ".zip", ".pdf", ".tar", ".tar.gz"} {
			if strings.HasSuffix(name, ext) {
				return true
			}
		}
	}
	return false
}

// L4: provider directories library rename would rename: a provider's old name (downloads
// reuse it), or a name that isn't the template's.
func checkProviderDirs(s *snapshot) []Finding {
	byDir := map[string][]library.Rename{}
	var dirs []string
	for _, r := range s.renames.renames {
		if r.ProviderDir == "" {
			continue
		}
		if _, ok := byDir[r.ProviderDir]; !ok {
			dirs = append(dirs, r.ProviderDir)
		}
		byDir[r.ProviderDir] = append(byDir[r.ProviderDir], r)
	}
	var out []Finding
	for _, dir := range dirs {
		renames := byDir[dir]
		name := filepath.Base(dir)
		f := Finding{Severity: Warning, Subject: Subject{Kind: "directory", Name: name},
			Fix: Fix{
				Commands: []string{"mangal library rename"},
				Note:     "lists every rename, --apply renames (undo: mangal library undo)",
				TUI:      "health: apply fix, or enter (the rename screen: pick the renames)",
				Action:   &Action{Kind: ActionRename, Paths: []string{dir}},
			}}
		to := ""
		for _, r := range renames {
			if r.From == dir && r.To != "" {
				to = filepath.Base(r.To)
			}
		}
		if p, ok := s.oldDirOf(name); ok {
			f.Message = fmt.Sprintf("%s is %s's old directory name, downloads reuse it", name, p)
		} else {
			f.Message = fmt.Sprintf("%s isn't the name the provider template gives", name)
		}
		if to != "" {
			f.Message += ": rename it to " + to
		} else {
			f.Message += fmt.Sprintf(": its %s move into the current one", plural(len(renames)-1, "manga folder"))
		}
		for i, r := range renames {
			if i == 8 {
				f.Details = append(f.Details, fmt.Sprintf("... %d more (mangal library rename)", len(renames)-8))
				break
			}
			f.Details = append(f.Details, renameLine(s, r))
		}
		out = append(out, f)
	}
	return out
}

// oldDirOf is the provider (name) whose old name the directory has.
func (s *snapshot) oldDirOf(name string) (string, bool) {
	if s.providerName == nil {
		return "", false
	}
	for _, l := range s.loaders {
		info := l.Info()
		for _, old := range library.OldInfos(info) {
			if s.providerName(old) == name && s.providerName(info) != name {
				return info.Name, true
			}
		}
	}
	return "", false
}

func renameLine(s *snapshot, r library.Rename) string {
	switch {
	case r.Skip != "":
		return fmt.Sprintf("skip %s: %s", s.rel(r.From), r.Skip)
	case r.RemoveEmpty:
		return fmt.Sprintf("remove %s once empty", s.rel(r.From))
	}
	return fmt.Sprintf("%s -> %s", s.rel(r.From), s.rel(r.To))
}

// L5: manga folders whose name isn't the one the template gives (from the cached
// metadata, fetched with the network checks), as one summary.
func checkFolderNames(s *snapshot) []Finding {
	var items []Finding
	var paths []string
	for _, r := range s.renames.renames {
		if r.ProviderDir != "" || r.Chapter {
			continue
		}
		subject := Subject{Kind: "folder", Name: s.rel(r.From)}
		if m, ok := s.mangaByPath(r.From); ok {
			subject = mangaSubject(m)
		}
		f := Finding{Check: "L5", Category: Library, Severity: Info, Subject: subject,
			Message: fmt.Sprintf("folder %s -> %s", s.rel(r.From), filepath.Base(r.To))}
		if r.Skip != "" {
			f.Message += " (can't: " + r.Skip + ")"
		} else {
			paths = append(paths, r.From)
			f.Fix = Fix{Commands: []string{"mangal library rename"}, Note: "renames every folder off the template, --apply", TUI: "health: apply fix, or enter (the rename screen: pick the renames)",
				Action: &Action{Kind: ActionRename, Paths: []string{r.From}}}
		}
		items = append(items, f)
	}
	var notChecked string
	if n := s.renames.noMetadata; n != 0 {
		notChecked = fmt.Sprintf("%s without cached AniList metadata weren't checked (mangal health --network looks them up)", plural(n, "folder"))
		if s.network {
			notChecked = fmt.Sprintf("%s without AniList metadata (no AniList id, or not found) weren't checked", plural(n, "folder"))
		}
	}
	if len(items) == 0 {
		if notChecked == "" {
			return nil
		}
		f := Finding{Severity: Info, Subject: Subject{Kind: "library", Name: "library"}, Part: "unchecked", Message: notChecked}
		if !s.network {
			f.Fix.Commands = []string{"mangal health --network --only L5"}
		} else {
			f.Fix = Fix{Commands: []string{"mangal health --only M1"}, Note: "the ones without an AniList link"}
		}
		return []Finding{f}
	}
	f := Finding{Severity: Warning, Subject: Subject{Kind: "library", Name: "library"},
		Message: fmt.Sprintf("%s aren't named by the manga template", plural(len(items), "folder")), Items: items,
		Fix: Fix{
			Commands: []string{"mangal library rename"},
			Note:     "lists every rename, --apply renames (undo: mangal library undo); mangal health --only L5 lists them",
			TUI:      "health: open the finding, apply fix; library: alt+r (the rename screen)",
		}}
	if len(items) == 1 {
		f.Message = "1 folder isn't named by the manga template: " + items[0].Message
	}
	if notChecked != "" {
		f.Details = []string{notChecked}
	}
	if len(paths) != 0 {
		f.Fix.Action = &Action{Kind: ActionRename, Paths: paths}
	}
	return []Finding{f}
}

// L6: what's in the trash.
func checkTrash(s *snapshot) []Finding {
	files, size := 0, int64(0)
	for _, b := range s.fs.trash {
		files += b.Files
		size += b.Size
	}
	if files == 0 {
		return nil
	}
	return []Finding{{Severity: Info, Subject: Subject{Kind: "library", Name: "trash"},
		Message: fmt.Sprintf("%s (%s) in the trash, from %s", plural(files, "file"), stringutil.HumanSize(size), plural(len(s.fs.trash), "batch")),
		Fix: Fix{
			Commands: []string{"mangal library trash", "mangal library trash empty"},
			Note:     "lists the batches; emptying deletes them for good (no undo)",
			TUI:      tuiLibrary + ": trash, empty",
			Action:   &Action{Kind: ActionEmptyTrash},
		}}}
}

// M4: broken .libmangal.json files (unreadable, invalid JSON): scans and renames take the
// folder for unmarked.
func checkBrokenMarkers(s *snapshot) []Finding {
	var out []Finding
	for _, m := range s.mangas {
		err := s.fs.markerErrs[m.ID]
		if err == nil {
			continue
		}
		out = append(out, Finding{Severity: Warning, Subject: mangaSubject(m),
			Message: "broken .libmangal.json: " + markerError(err),
			Fix: Fix{
				Commands: []string{fmt.Sprintf("mangal library mark %d", m.ID)},
				Note:     "lists the .libmangal.json it would write in its place from the library, --apply writes it",
				TUI:      "health: apply fix",
				Action:   &Action{Kind: ActionMark, Manga: m.ID},
			}})
	}
	for _, folder := range s.fs.folders {
		if folder.MarkerErr == nil {
			continue
		}
		if _, ok := s.mangaByPath(folder.Path); ok {
			continue
		}
		path := filepath.Join(folder.Path, libmangal.FilenameMangaDirInfo)
		out = append(out, Finding{Severity: Warning, Subject: Subject{Kind: "folder", Name: s.rel(folder.Path)},
			Message: "broken .libmangal.json: " + markerError(folder.MarkerErr),
			Fix: Fix{
				Commands: []string{"rm " + quote(path), "mangal library scan --mark"},
				Note:     "not in the library: delete it, the scan adds the folder, --mark --apply marks it",
			}})
	}
	return out
}

// markerError is the error without the path (the subject says it).
func markerError(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, libmangal.FilenameMangaDirInfo+": "); i >= 0 {
		return msg[i+len(libmangal.FilenameMangaDirInfo)+2:]
	}
	return msg
}

// markerFindings are M3's: .libmangal.json files whose provider manga isn't one of the
// library manga's sources.
func markerFindings(s *snapshot) []Finding {
	if s.fs == nil {
		return nil
	}
	var out []Finding
	for _, m := range s.mangas {
		marker := s.fs.markers[m.ID]
		if marker == nil {
			continue
		}
		what, missingID := library.MarkerDisagreement(m, *marker, s.loaders)
		if what == "" {
			continue
		}
		f := Finding{Severity: Warning, Subject: mangaSubject(m), Part: "marker", Message: "its .libmangal.json " + what,
			Fix: Fix{
				Commands: []string{fmt.Sprintf("mangal library mark %d", m.ID)},
				Note:     "lists the .libmangal.json it would write from the library source (its other ids kept), --apply writes it",
				TUI:      "health: apply fix",
				Action:   &Action{Kind: ActionMark, Manga: m.ID},
			}}
		if missingID {
			f.Severity = Info
			f.Fix.Note += "; the next download from it writes it too"
		}
		out = append(out, f)
	}
	return out
}

// L8: library mangas without a chapter (no download recorded) whose folder is there.
func checkEmpty(s *snapshot) []Finding {
	var out []Finding
	for _, m := range s.mangas {
		if m.Chapters != 0 || s.fs != nil && s.fs.missing[m.ID] {
			continue
		}
		where := "no folder of its own"
		if m.Dir != "" {
			where = s.rel(m.Path())
		}
		out = append(out, Finding{Severity: Info, Subject: mangaSubject(m), Message: "no chapters recorded (" + where + ")",
			Fix: Fix{
				Commands: []string{"mangal library scan"},
				Note:     "adds the chapter files in its folder; if there are none, forget it (d in the TUI library)",
				TUI:      tuiLibrary + ": scan, delete",
				Action:   &Action{Kind: ActionForget, Manga: m.ID},
			}})
	}
	return out
}
