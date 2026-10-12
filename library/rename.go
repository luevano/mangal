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

	// Library tells the mangas of the folders when a provider directory's mangas move into
	// an existing one, may be zero (only the .libmangal.json files tell then).
	Library storage.Library
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
	if n.Metadata != nil && len(folders) != 0 {
		ids := make([][]metadata.ID, len(folders))
		for i, f := range folders {
			ids[i] = f.info.MetadataIDs
		}
		if metas, err = n.Metadata(ctx, ids); err != nil {
			return nil, fmt.Errorf("metadata: %w", err)
		}
		if len(metas) != len(folders) {
			return nil, fmt.Errorf("metadata: %d results for %d folders", len(metas), len(folders))
		}
	}
	var renames []Rename
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
		if name := n.MangaName(provider, &dirManga{info: f.info.Manga, meta: metas[i]}); name != filepath.Base(f.path) {
			renames = append(renames, Rename{From: f.path, To: filepath.Join(filepath.Dir(f.path), name)})
		}
	}
	for _, providerDir := range providerDirsSeen {
		if mixed[providerDir] || n.otherProviders(filepath.Base(providerDir), providers[providerDir].ID) {
			continue
		}
		if name := n.ProviderName(providers[providerDir]); name != filepath.Base(providerDir) {
			renames = append(renames, Rename{From: providerDir, To: filepath.Join(dir, name)})
		}
	}
	if providerDirs {
		// the directories of a provider's old names, also without marked mangas in them
		// (downloaded before the markers), as libmangal's downloads reuse them
		for _, loader := range n.Loaders {
			info := loader.Info()
			name := n.ProviderName(info)
			for _, alias := range info.History {
				old := info
				old.ID, old.Name, old.Website, old.History = alias.ID, alias.Name, alias.Website, nil
				if old.Name == "" {
					old.Name = info.Name
				}
				from := filepath.Join(dir, n.ProviderName(old))
				if filepath.Base(from) == name || slices.ContainsFunc(renames, func(r Rename) bool { return r.From == from }) {
					continue
				}
				if exists, err := afero.DirExists(n.FS, from); err != nil {
					return nil, err
				} else if exists {
					renames = append(renames, Rename{From: from, To: filepath.Join(dir, name)})
				}
			}
		}
	}
	if !providerDirs {
		return renames, nil
	}
	// a provider directory onto an existing one (not empty): its mangas move one by one
	var planned []Rename
	for _, r := range renames {
		if filepath.Dir(r.From) != dir || r.To == "" {
			planned = append(planned, r)
			continue
		}
		into, err := n.intoExisting(r, renames)
		if err != nil {
			return nil, err
		}
		if into == nil {
			planned = append(planned, r)
			continue
		}
		planned = append(planned, into...)
	}
	return planned, nil
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
		for _, alias := range info.History {
			old := info
			old.ID, old.Name, old.Website, old.History = alias.ID, alias.Name, alias.Website, nil
			if old.Name == "" {
				old.Name = info.Name
			}
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
		move := Rename{From: from, To: filepath.Join(r.To, name)}
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
	return append(out, Rename{From: r.From, RemoveEmpty: true}), nil
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
	ApplyEach(fsys, renames, func(i int, err error) { errs[i] = err })
	return errs
}

// ApplyEach is Apply telling each rename's error (nil when done) as soon as it's done.
func ApplyEach(fsys afero.Fs, renames []Rename, done func(i int, err error)) {
	for i, r := range renames {
		done(i, applyRename(fsys, r))
	}
}

func applyRename(fsys afero.Fs, r Rename) error {
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
		return fsys.Remove(r.From)
	}
	exists, err := afero.Exists(fsys, r.To)
	if err == nil && exists && !sameFile(fsys, r.From, r.To) {
		// an empty directory in the way (e.g. a provider's of its current name) goes
		if entries, readErr := afero.ReadDir(fsys, r.To); readErr == nil && len(entries) == 0 {
			if err = fsys.Remove(r.To); err == nil {
				exists = false
			}
		}
	}
	switch {
	case err != nil:
		return err
	case exists && sameFile(fsys, r.From, r.To):
		// only the case changes, on a case-insensitive filesystem
		tmp := r.From + ".mangal-rename"
		if err := fsys.Rename(r.From, tmp); err != nil {
			return err
		}
		return fsys.Rename(tmp, r.To)
	case exists:
		return fmt.Errorf("%s already exists", r.To)
	}
	// copied when it's on another drive (the copy goes when that fails)
	return moveFile(fsys, r.From, r.To, DefaultModes)
}

func sameFile(fsys afero.Fs, a, b string) bool {
	infoA, err := fsys.Stat(a)
	if err != nil {
		return false
	}
	infoB, err := fsys.Stat(b)
	return err == nil && os.SameFile(infoA, infoB)
}

// dirManga is a manga rebuilt from its directory info, for the name templates.
type dirManga struct {
	info mangadata.MangaInfo
	meta metadata.Metadata
}

func (m *dirManga) String() string                     { return m.info.Title }
func (m *dirManga) Info() mangadata.MangaInfo          { return m.info }
func (m *dirManga) Metadata() metadata.Metadata        { return m.meta }
func (m *dirManga) SetMetadata(meta metadata.Metadata) { m.meta = meta }
