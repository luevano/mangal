// Package library manages the downloaded mangas.
package library

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/spf13/afero"
)

// Rename moves a directory to the name it would get now.
type Rename struct {
	From, To string
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
	if !providerDirs {
		renames, _, err := n.planMangas(ctx, dir)
		return renames, err
	}

	entries, err := afero.ReadDir(n.FS, dir)
	if err != nil {
		return nil, err
	}
	var renames []Rename
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		providerDir := filepath.Join(dir, entry.Name())
		mangas, provider, err := n.planMangas(ctx, providerDir)
		if err != nil {
			return nil, err
		}
		renames = append(renames, mangas...)
		if provider == nil {
			continue
		}
		if name := n.ProviderName(*provider); name != entry.Name() {
			renames = append(renames, Rename{From: providerDir, To: filepath.Join(dir, name)})
		}
	}
	return renames, nil
}

// planMangas plans the manga directories in parent, and returns the provider of the
// first one with a known provider.
func (n Namer) planMangas(ctx context.Context, parent string) ([]Rename, *libmangal.ProviderInfo, error) {
	entries, err := afero.ReadDir(n.FS, parent)
	if err != nil {
		return nil, nil, err
	}
	var renames []Rename
	var parentProvider *libmangal.ProviderInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mangaDir := filepath.Join(parent, entry.Name())
		info, err := libmangal.ReadMangaDirInfo(n.FS, mangaDir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}

		provider := libmangal.ProviderInfo{ID: info.Provider, Name: info.Provider}
		if loader, _, ok := libmangal.FindLoader(n.Loaders, info.Provider); ok {
			provider = loader.Info()
			if parentProvider == nil {
				parentProvider = &provider
			}
		}
		meta, err := n.Metadata(ctx, info.MetadataIDs)
		if err != nil {
			return nil, nil, fmt.Errorf("metadata of %q: %w", mangaDir, err)
		}

		if name := n.MangaName(provider, &dirManga{info: info.Manga, meta: meta}); name != entry.Name() {
			renames = append(renames, Rename{From: mangaDir, To: filepath.Join(parent, name)})
		}
	}
	return renames, parentProvider, nil
}

// Apply does the renames in order, skipping those whose target exists.
// It returns an error per rename (nil when done).
func Apply(fsys afero.Fs, renames []Rename) []error {
	errs := make([]error, len(renames))
	for i, r := range renames {
		exists, err := afero.Exists(fsys, r.To)
		switch {
		case err != nil:
			errs[i] = err
		case exists:
			errs[i] = fmt.Errorf("%s already exists", r.To)
		default:
			errs[i] = fsys.Rename(r.From, r.To)
		}
	}
	return errs
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
