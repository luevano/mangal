// Package library manages the downloaded mangas.
package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// Rename moves a directory to the name it would get now.
type Rename struct {
	From, To string
	// Skip is why it can't be done (e.g. To exists), empty when it can.
	Skip string
	// RemoveEmpty removes From (To is empty) when it's empty by then: a provider directory
	// whose mangas moved into the one of its current name.
	RemoveEmpty bool
	// Chapter is a chapter file's rename (PlanChapters): its download's path follows.
	Chapter bool
	// NoMetadata is a manga folder with metadata ids whose metadata wasn't found: its name
	// would be the fallback one, without its year and id tags. It's skipped unless
	// Namer.Fallback.
	NoMetadata bool
	// ProviderDir is the provider directory a provider directory's rename is of: the
	// directory itself, or its manga folders moving into an existing one; empty for a
	// manga folder's own rename.
	ProviderDir string
}

// OldInfos are the provider's infos under its old names (History), as the provider
// name template sees them: an alias without a name keeps the current one.
func OldInfos(info libmangal.ProviderInfo) []libmangal.ProviderInfo {
	olds := make([]libmangal.ProviderInfo, len(info.History))
	for i, alias := range info.History {
		old := info
		old.ID, old.Name, old.Website, old.History = alias.ID, alias.Name, alias.Website, nil
		if old.Name == "" {
			old.Name = info.Name
		}
		olds[i] = old
	}
	return olds
}

// ErrNotEmpty is a directory Apply leaves as it isn't empty (Rename.RemoveEmpty).
var ErrNotEmpty = errors.New("not empty, left in place")

// Namer computes the names manga and provider directories would get now.
type Namer struct {
	FS      afero.Fs
	Loaders []libmangal.ProviderLoader

	// ProviderName and MangaName are the name templates.
	ProviderName func(libmangal.ProviderInfo) string
	MangaName    func(libmangal.ProviderInfo, mangadata.Manga) string

	// Metadata looks up the metadata of several mangas at once (batched), each by one of
	// its ids: a result per manga, nil when it has none. Nil looks nothing up.
	Metadata func(ctx context.Context, ids [][]metadata.ID) ([]metadata.Metadata, error)

	// OnProgress, if set, is told where Plan is.
	OnProgress func(Progress)

	// Library tells the mangas of the folders when a provider directory's mangas move into
	// an existing one, and their links (bindings and own ids), which win over the
	// .libmangal.json's ids; may be zero (only the .libmangal.json files tell then).
	Library storage.Library

	// OnDisagree, if set, is told of the folders whose .libmangal.json or name has other ids
	// than their links (the links are used), e.g. "AniList 74347 (binding, library) used,
	// disagree: folder tag 1".
	OnDisagree func(folder string, disagreements []string)

	// ChapterName is the chapter name template, for PlanChapters.
	ChapterName func(libmangal.ProviderInfo, mangadata.Chapter) string

	// NeedMetadata leaves out the manga folders Metadata has nothing for (e.g. only the
	// cached metadata is asked), telling OnNoMetadata: their names aren't worked out.
	NeedMetadata bool
	OnNoMetadata func(folder string)
	// Fallback renames the manga folders with metadata ids whose metadata wasn't found to
	// the fallback name too (it drops their year and id tags); they're skipped otherwise.
	Fallback bool
}

// Plan lists the renames of the manga directories under dir (found by their
// libmangal.MangaDirInfo), and of the provider directories when providerDirs is set.
// Manga directories come before the provider directory they're in.
func (n Namer) Plan(ctx context.Context, dir string, providerDirs bool) ([]Rename, error) {
	folders, err := markedDirs(n.FS, dir, providerDirs)
	if err != nil {
		return nil, err
	}
	metas := make([]metadata.Metadata, len(folders))
	ids := make([][]metadata.ID, len(folders))
	if n.Metadata != nil && len(folders) != 0 {
		for i, f := range folders {
			if ids[i], err = n.linkedIDs(f); err != nil {
				return nil, err
			}
		}
		report(n.OnProgress, Progress{Phase: "looking up metadata"})
		if metas, err = n.Metadata(ctx, ids); err != nil {
			return nil, fmt.Errorf("metadata: %w", err)
		}
		if len(metas) != len(folders) {
			return nil, fmt.Errorf("metadata: %d results for %d folders", len(metas), len(folders))
		}
	}
	var renames []Rename
	// manga folders that would get the same name: the first one does
	planned := map[string]string{}
	// the provider of each provider directory: the one of its mangas with a known
	// provider, none when they're of several (e.g. a manga moved there by hand)
	var providerDirsSeen []string
	providers := map[string]libmangal.ProviderInfo{}
	mixed := map[string]bool{}
	for i, f := range folders {
		provider := libmangal.ProviderInfo{ID: f.info.Provider, Name: f.info.Provider}
		if loader, _, ok := libmangal.FindLoader(n.Loaders, f.info.Provider); ok {
			provider = loader.Info()
			seen, ok := providers[f.providerDir]
			switch {
			case f.providerDir == "":
			case !ok:
				providers[f.providerDir] = provider
				providerDirsSeen = append(providerDirsSeen, f.providerDir)
			case seen.ID != provider.ID:
				mixed[f.providerDir] = true
			}
		}
		if n.NeedMetadata && n.Metadata != nil && metas[i] == nil {
			if n.OnNoMetadata != nil {
				n.OnNoMetadata(f.path)
			}
			continue
		}
		if name := n.MangaName(provider, &dirManga{info: f.info.Manga, meta: metas[i]}); name != filepath.Base(f.path) {
			r := Rename{From: f.path, To: filepath.Join(filepath.Dir(f.path), name)}
			r.NoMetadata = n.Metadata != nil && metas[i] == nil && len(ids[i]) != 0
			switch {
			case r.NoMetadata && !n.Fallback:
				r.Skip = "no metadata found for " + formatIDs(ids[i])
			case planned[r.To] != "":
				r.Skip = fmt.Sprintf("%s would get that name too (%s), the same manga? mangal library merge", filepath.Base(planned[r.To]), filepath.Base(r.To))
			default:
				if r.Skip, err = n.taken(r); err != nil {
					return nil, err
				}
			}
			if r.Skip == "" {
				planned[r.To] = r.From
			}
			renames = append(renames, r)
		}
	}
	for _, providerDir := range providerDirsSeen {
		if mixed[providerDir] || n.otherProviders(filepath.Base(providerDir), providers[providerDir].ID) {
			continue
		}
		if name := n.ProviderName(providers[providerDir]); name != filepath.Base(providerDir) {
			renames = append(renames, Rename{From: providerDir, To: filepath.Join(dir, name), ProviderDir: providerDir})
		}
	}
	if providerDirs {
		// the directories of a provider's old names, also without marked mangas in them
		// (downloaded before the markers), as libmangal's downloads reuse them
		for _, loader := range n.Loaders {
			info := loader.Info()
			name := n.ProviderName(info)
			for _, old := range OldInfos(info) {
				from := filepath.Join(dir, n.ProviderName(old))
				if filepath.Base(from) == name || slices.ContainsFunc(renames, func(r Rename) bool { return r.From == from }) {
					continue
				}
				if exists, err := afero.DirExists(n.FS, from); err != nil {
					return nil, err
				} else if exists {
					renames = append(renames, Rename{From: from, To: filepath.Join(dir, name), ProviderDir: from})
				}
			}
		}
	}
	if !providerDirs {
		return renames, nil
	}
	// a provider directory onto an existing one (not empty): its mangas move one by one
	var out []Rename
	for _, r := range renames {
		if filepath.Dir(r.From) != dir || r.To == "" {
			out = append(out, r)
			continue
		}
		into, err := n.intoExisting(r, renames)
		if err != nil {
			return nil, err
		}
		if into == nil {
			out = append(out, r)
			continue
		}
		out = append(out, into...)
	}
	return out, nil
}

// taken is why a manga folder can't get its name: another folder has it (another library
// manga's: merge them), empty when it can.
func (n Namer) taken(r Rename) (string, error) {
	exists, err := afero.Exists(n.FS, r.To)
	if err != nil || !exists || sameFile(n.FS, r.From, r.To) {
		return "", err
	}
	if n.Library.DB != nil {
		other, ok, err := n.Library.MangaByFolder(filepath.Dir(r.To), filepath.Base(r.To))
		if err != nil {
			return "", err
		}
		this, thisOK, err := n.Library.MangaByFolder(filepath.Dir(r.From), filepath.Base(r.From))
		if err != nil {
			return "", err
		}
		if ok && thisOK {
			return fmt.Sprintf("%s is library manga %d %q's folder, the same manga? mangal library merge %d %d",
				filepath.Base(r.To), other.ID, other.Title, other.ID, this.ID), nil
		}
	}
	return filepath.Base(r.To) + " exists", nil
}

// linkedIDs are the folder's metadata ids with its library manga's links (its bindings, its
// own ids) in place of the .libmangal.json's, telling OnDisagree what differs.
func (n Namer) linkedIDs(f mangaFolder) ([]metadata.ID, error) {
	if n.Library.DB == nil {
		return f.info.MetadataIDs, nil
	}
	m, ok, err := n.Library.MangaByFolder(filepath.Dir(f.path), filepath.Base(f.path))
	if err != nil || !ok {
		return f.info.MetadataIDs, err
	}
	info, err := Files{FS: n.FS, Library: n.Library}.LinkInfo(m)
	if err != nil {
		return nil, err
	}
	links := map[metadata.IDCode]string{}
	var disagree []string
	for _, s := range info.Sources {
		if !s.Linked() {
			continue
		}
		links[s.Code] = s.Current()
		if d := s.Disagreements(); len(d) != 0 {
			disagree = append(disagree, fmt.Sprintf("%s %s used, disagree: %s", sourceName(s.Code), s.Current(), strings.Join(d, ", ")))
		}
	}
	if len(disagree) != 0 && n.OnDisagree != nil {
		n.OnDisagree(f.path, disagree)
	}
	return storage.ReplaceIDs(f.info.MetadataIDs, links), nil
}

// otherProviders is a directory name of a loaded provider other than id (its current or an
// old one): mangas of id in it don't make it id's directory.
func (n Namer) otherProviders(name, id string) bool {
	for _, loader := range n.Loaders {
		info := loader.Info()
		if info.ID == id {
			continue
		}
		if n.ProviderName(info) == name {
			return true
		}
		for _, old := range OldInfos(info) {
			if n.ProviderName(old) == name {
				return true
			}
		}
	}
	return false
}

// folderID is what tells a manga folder's manga: its library manga (0 if none), its
// sources (current provider ids) and metadata ids.
type folderID struct {
	manga   storage.Manga
	sources [][2]string
	ids     []metadata.ID
}

func (f folderID) known() bool { return f.manga.ID != 0 || len(f.sources) != 0 }

// same is the same manga: a shared source or metadata id.
func (f folderID) same(other folderID) bool {
	for _, src := range f.sources {
		if slices.Contains(other.sources, src) {
			return true
		}
	}
	for _, id := range f.ids {
		if slices.ContainsFunc(other.ids, func(o metadata.ID) bool { return o.Source == id.Source && o.Raw == id.Raw }) {
			return true
		}
	}
	return false
}

// folderID is the manga of the folder parent/name, by the library and its .libmangal.json.
func (n Namer) folderID(parent, name string) (folderID, error) {
	var id folderID
	add := func(provider, mangaID string) {
		if loader, _, ok := libmangal.FindLoader(n.Loaders, provider); ok {
			provider = loader.Info().ID
		}
		if provider != "" && mangaID != "" && !slices.Contains(id.sources, [2]string{provider, mangaID}) {
			id.sources = append(id.sources, [2]string{provider, mangaID})
		}
	}
	if n.Library.DB != nil {
		m, ok, err := n.Library.MangaByFolder(parent, name)
		if err != nil {
			return id, err
		}
		if ok {
			id.manga = m
			id.ids = append(id.ids, m.MetadataIDs...)
			for _, src := range m.Sources {
				add(src.Provider, src.MangaID)
			}
		}
	}
	if info, err := libmangal.ReadMangaDirInfo(n.FS, filepath.Join(parent, name)); err == nil {
		add(info.Provider, info.Manga.ID)
		id.ids = append(id.ids, info.MetadataIDs...)
	}
	return id, nil
}

// intoExisting moves the provider directory r.From's manga folders into r.To one by one
// when r.To has something in it (nil when it's missing or empty: r renames it all, an
// empty r.To is removed first). A manga r.To has already (same source or metadata id) or a
// folder name taken there is skipped, folders of no known manga stay, r.From goes once
// empty. Manga renames planned inside r.From (renames) happen first.
func (n Namer) intoExisting(r Rename, renames []Rename) ([]Rename, error) {
	targets, err := afero.ReadDir(n.FS, r.To)
	if errors.Is(err, fs.ErrNotExist) || err == nil && len(targets) == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if sameFile(n.FS, r.From, r.To) {
		return nil, nil
	}
	var inTarget []folderID
	for _, e := range targets {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		id, err := n.folderID(r.To, e.Name())
		if err != nil {
			return nil, err
		}
		if id.known() {
			inTarget = append(inTarget, id)
		}
	}
	entries, err := afero.ReadDir(n.FS, r.From)
	if err != nil {
		return nil, err
	}
	var out []Rename
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		from, name := filepath.Join(r.From, e.Name()), e.Name()
		if i := slices.IndexFunc(renames, func(p Rename) bool { return p.From == from }); i >= 0 && renames[i].Skip == "" {
			from, name = renames[i].To, filepath.Base(renames[i].To)
		}
		id, err := n.folderID(r.From, e.Name())
		if err != nil {
			return nil, err
		}
		move := Rename{From: from, To: filepath.Join(r.To, name), ProviderDir: r.From}
		switch {
		case !id.known():
			move.To, move.Skip = "", "no known manga (no .libmangal.json, not in the library), left in place"
		default:
			if i := slices.IndexFunc(inTarget, id.same); i >= 0 {
				move.Skip = mergeHint(inTarget[i], id, r.To)
				break
			}
			if exists, err := afero.Exists(n.FS, move.To); err != nil {
				return nil, err
			} else if exists {
				move.Skip = fmt.Sprintf("%s is another manga's folder: rename one of them", filepath.Join(filepath.Base(r.To), name))
			}
		}
		out = append(out, move)
	}
	return append(out, Rename{From: r.From, RemoveEmpty: true, ProviderDir: r.From}), nil
}

// mergeHint is the skip of a manga the target directory has already.
func mergeHint(keep, other folderID, dir string) string {
	where := fmt.Sprintf("%s has it already", filepath.Base(dir))
	if keep.manga.ID != 0 {
		where = fmt.Sprintf("%s has it already (library manga %d %q)", filepath.Base(dir), keep.manga.ID, keep.manga.Title)
	}
	switch {
	case keep.manga.ID != 0 && other.manga.ID != 0:
		return fmt.Sprintf("%s: mangal library merge %d %d", where, keep.manga.ID, other.manga.ID)
	case keep.manga.ID != 0:
		return fmt.Sprintf("%s: mangal library scan, then mangal library merge %d <its id>", where, keep.manga.ID)
	}
	return where + ": mangal library scan, then mangal library merge"
}

// Apply does the renames in order, skipping those whose target exists.
// It returns an error per rename (nil when done).
func Apply(fsys afero.Fs, renames []Rename) []error {
	errs := make([]error, len(renames))
	_ = ApplyEach(fsys, nil, renames, nil, func(i int, err error) error {
		errs[i] = err
		return nil
	})
	return errs
}

// ApplyEach is Apply recording the changes in j (may be nil), telling where it is
// (onProgress, may be nil) and each rename's error (nil when done) as soon as it's done.
// It stops at the first error done returns.
func ApplyEach(fsys afero.Fs, j Journal, renames []Rename, onProgress func(Progress), done func(i int, err error) error) error {
	return ApplyEachContext(context.Background(), fsys, DefaultModes, j, renames, onProgress, done)
}

// ApplyEachContext is ApplyEach, stopped between renames when ctx is done, the folders
// it makes with modes.Dir.
func ApplyEachContext(ctx context.Context, fsys afero.Fs, modes Modes, j Journal, renames []Rename, onProgress func(Progress), done func(i int, err error) error) error {
	for i, r := range renames {
		if err := ctx.Err(); err != nil {
			return err
		}
		report(onProgress, Progress{Phase: "renaming", Done: i + 1, Total: len(renames), Item: filepath.Base(r.From)})
		if err := done(i, applyRename(ctx, fsys, j, r, modes)); err != nil {
			return err
		}
	}
	return nil
}

// ApplyRenames is ApplyEach moving the library's rows with each rename done (a folder's
// mangas and downloads, a chapter file's download): done is told each rename's error
// (nil when done), a library error stops it.
func ApplyRenames(fsys afero.Fs, lib storage.Library, j Journal, renames []Rename, onProgress func(Progress), done func(i int, err error)) error {
	return ApplyRenamesContext(context.Background(), fsys, DefaultModes, lib, j, renames, onProgress, done)
}

// ApplyRenamesContext is ApplyRenames, stopped between renames when ctx is done, the
// folders it makes with modes.Dir.
func ApplyRenamesContext(ctx context.Context, fsys afero.Fs, modes Modes, lib storage.Library, j Journal, renames []Rename, onProgress func(Progress), done func(i int, err error)) error {
	return ApplyEachContext(ctx, fsys, modes, j, renames, onProgress, func(i int, err error) error {
		r := renames[i]
		if err == nil && r.Skip == "" && !r.RemoveEmpty {
			move := lib.Moved
			if r.Chapter {
				move = lib.SetDownloadPath
			}
			if err := move(r.From, r.To); err != nil {
				return err
			}
		}
		if done != nil {
			done(i, err)
		}
		return nil
	})
}

func applyRename(ctx context.Context, fsys afero.Fs, j Journal, r Rename, modes Modes) error {
	if r.Skip != "" {
		return errors.New(r.Skip)
	}
	if r.RemoveEmpty {
		entries, err := afero.ReadDir(fsys, r.From)
		switch {
		case err != nil:
			return err
		case len(entries) != 0:
			return fmt.Errorf("%s: %w", r.From, ErrNotEmpty)
		}
		return removeDir(fsys, j, r.From)
	}
	exists, err := afero.Exists(fsys, r.To)
	if err != nil {
		return err
	}
	if exists && !sameFile(fsys, r.From, r.To) {
		// an empty directory in the way (e.g. a provider's of its current name) goes
		if entries, err := afero.ReadDir(fsys, r.To); err == nil && len(entries) == 0 {
			if err := removeDir(fsys, j, r.To); err != nil {
				return err
			}
		}
	}
	return moveDir(ctx, fsys, j, r.From, r.To, modes)
}

func sameFile(fsys afero.Fs, a, b string) bool {
	infoA, err := fsys.Stat(a)
	if err != nil {
		return false
	}
	infoB, err := fsys.Stat(b)
	return err == nil && os.SameFile(infoA, infoB)
}

// PlanChapters lists the renames of the mangas' chapter files to the names the chapter
// name template gives them (ChapterName), in the same folder. They go before the folders'
// renames (Plan), whose paths they use. A name taken (by a file or another chapter) is
// skipped, as a chapter without a recorded title: its name may say more than the
// template would.
func (n Namer) PlanChapters(mangas []storage.Manga) ([]Rename, error) {
	if n.ChapterName == nil {
		return nil, errors.New("no chapter name template")
	}
	var renames []Rename
	taken := map[string]bool{}
	for i, m := range mangas {
		report(n.OnProgress, Progress{Phase: "planning chapter names", Done: i + 1, Total: len(mangas), Item: m.Title})
		downloads, err := n.Library.Downloads(m.ID)
		if err != nil {
			return nil, err
		}
		for _, dl := range downloads {
			r := Rename{From: dl.Path, Chapter: true}
			if exists, err := afero.Exists(n.FS, dl.Path); err != nil {
				return nil, err
			} else if !exists {
				continue
			}
			if dl.ChapterTitle == "" {
				r.Skip = "no chapter title recorded"
				renames = append(renames, r)
				continue
			}
			name := chapterFileName(n.ChapterName, n.Loaders, dl)
			r.To = filepath.Join(filepath.Dir(dl.Path), name)
			if name == "" || r.To == dl.Path {
				continue
			}
			switch exists, err := afero.Exists(n.FS, r.To); {
			case err != nil:
				return nil, err
			case exists && !sameFile(n.FS, r.From, r.To):
				r.Skip = "a file of that name exists"
			case taken[r.To]:
				r.Skip = "another chapter gets that name"
			}
			taken[r.To] = true
			renames = append(renames, r)
		}
	}
	return renames, nil
}

// chapterFileName is the file name the chapter name template gives the download, with
// its format's extension.
func chapterFileName(template func(libmangal.ProviderInfo, mangadata.Chapter) string, loaders []libmangal.ProviderLoader, dl storage.Download) string {
	provider := libmangal.ProviderInfo{ID: dl.Provider, Name: dl.Provider}
	if loader, _, ok := libmangal.FindLoader(loaders, dl.Provider); ok {
		provider = loader.Info()
	}
	name := template(provider, &dirChapter{
		info:   mangadata.ChapterInfo{Title: dl.ChapterTitle, URL: dl.ChapterURL, Number: dl.Chapter},
		volume: &dirVolume{info: mangadata.VolumeInfo{Number: dl.Volume}, manga: &dirManga{info: mangadata.MangaInfo{ID: dl.MangaID, Title: dl.MangaTitle}}},
	})
	if format, err := libmangal.FormatString(dl.Format); err == nil {
		name += format.Extension()
	}
	return name
}

type dirVolume struct {
	info  mangadata.VolumeInfo
	manga *dirManga
}

func (v *dirVolume) String() string             { return fmt.Sprintf("Vol. %g", v.info.Number) }
func (v *dirVolume) Info() mangadata.VolumeInfo { return v.info }
func (v *dirVolume) Manga() mangadata.Manga     { return v.manga }

type dirChapter struct {
	info   mangadata.ChapterInfo
	volume *dirVolume
}

func (c *dirChapter) String() string              { return c.info.Title }
func (c *dirChapter) Info() mangadata.ChapterInfo { return c.info }
func (c *dirChapter) Volume() mangadata.Volume    { return c.volume }

// dirManga is a manga rebuilt from its directory info, for the name templates.
type dirManga struct {
	info mangadata.MangaInfo
	meta metadata.Metadata
}

func (m *dirManga) String() string                     { return m.info.Title }
func (m *dirManga) Info() mangadata.MangaInfo          { return m.info }
func (m *dirManga) Metadata() metadata.Metadata        { return m.meta }
func (m *dirManga) SetMetadata(meta metadata.Metadata) { m.meta = meta }

// formatIDs are metadata ids as "al 30002, kt 8".
func formatIDs(ids []metadata.ID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = string(id.Code) + " " + id.Raw
	}
	return strings.Join(parts, ", ")
}
