// Package library manages the downloaded mangas.
package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/spf13/afero"
)

// Rename moves a directory to the name it would get now.
type Rename struct {
	From, To string
	// Skip is why it can't be done (e.g. To exists), empty when it can.
	Skip string
}

// Namer computes the names manga and provider directories would get now.
type Namer struct {
	FS      afero.Fs
	Loaders []libmangal.ProviderLoader

	// ProviderName and MangaName are the name templates.
	ProviderName func(libmangal.ProviderInfo) string
	MangaName    func(libmangal.ProviderInfo, mangadata.Manga) string

	// Metadata looks up the manga's metadata by one of its ids, nil if there's none.
	Metadata func(ctx context.Context, ids []metadata.ID) (metadata.Metadata, error)
}

// Plan lists the renames of the manga directories under dir (found by their
// libmangal.MangaDirInfo), and of the provider directories when providerDirs is set.
// Manga directories come before the provider directory they're in.
func (n Namer) Plan(ctx context.Context, dir string, providerDirs bool) ([]Rename, error) {
	folders, err := markedDirs(n.FS, dir, providerDirs)
	if err != nil {
		return nil, err
	}
	var renames []Rename
	// the provider of each provider directory: its first manga with a known provider
	var providerDirsSeen []string
	providers := map[string]libmangal.ProviderInfo{}
	for _, f := range folders {
		provider := libmangal.ProviderInfo{ID: f.info.Provider, Name: f.info.Provider}
		if loader, _, ok := libmangal.FindLoader(n.Loaders, f.info.Provider); ok {
			provider = loader.Info()
			if _, seen := providers[f.providerDir]; f.providerDir != "" && !seen {
				providers[f.providerDir] = provider
				providerDirsSeen = append(providerDirsSeen, f.providerDir)
			}
		}
		meta, err := n.Metadata(ctx, f.info.MetadataIDs)
		if err != nil {
			return nil, fmt.Errorf("metadata of %q: %w", f.path, err)
		}
		if name := n.MangaName(provider, &dirManga{info: f.info.Manga, meta: meta}); name != filepath.Base(f.path) {
			renames = append(renames, Rename{From: f.path, To: filepath.Join(filepath.Dir(f.path), name)})
		}
	}
	for _, providerDir := range providerDirsSeen {
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
	for i, r := range renames {
		// a provider directory: moving onto an existing one would mean merging them
		if !providerDirs || filepath.Dir(r.From) != dir {
			continue
		}
		if exists, err := afero.Exists(n.FS, r.To); err != nil {
			return nil, err
		} else if exists && !sameFile(n.FS, r.From, r.To) {
			renames[i].Skip = fmt.Sprintf("%s already exists: move its mangas there (mangal library move) or merge them, directories aren't merged", r.To)
		}
	}
	return renames, nil
}

// Apply does the renames in order, skipping those whose target exists.
// It returns an error per rename (nil when done).
func Apply(fsys afero.Fs, renames []Rename) []error {
	errs := make([]error, len(renames))
	for i, r := range renames {
		exists, err := afero.Exists(fsys, r.To)
		switch {
		case r.Skip != "":
			errs[i] = errors.New(r.Skip)
		case err != nil:
			errs[i] = err
		case exists && sameFile(fsys, r.From, r.To):
			// only the case changes, on a case-insensitive filesystem
			tmp := r.From + ".mangal-rename"
			if errs[i] = fsys.Rename(r.From, tmp); errs[i] == nil {
				errs[i] = fsys.Rename(tmp, r.To)
			}
		case exists:
			errs[i] = fmt.Errorf("%s already exists", r.To)
		default:
			errs[i] = fsys.Rename(r.From, r.To)
		}
	}
	return errs
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
