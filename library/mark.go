package library

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// MarkEvidence is what a marker was made from.
type MarkEvidence string

const (
	// MarkedFromSource: the library manga's (current) source, provider and manga id.
	MarkedFromSource MarkEvidence = "from the library source"
	// MarkedFromDir: the provider of the provider directory it's in, no manga id.
	MarkedFromDir MarkEvidence = "provider from its directory, no manga id"
	// MarkedFromIDs: only the title and metadata ids, no provider nor manga id.
	MarkedFromIDs MarkEvidence = "metadata ids only, no provider nor manga id"
)

// Marked is a manga folder that got a libmangal.MangaDirInfo.
type Marked struct {
	Manga    storage.Manga
	Info     libmangal.MangaDirInfo
	Evidence MarkEvidence
}

// MarkResult is what Mark did.
type MarkResult struct {
	Marked []Marked
	// Unknown are the folders without a marker nor anything to make one from (no source,
	// provider directory or metadata id), left unmarked.
	Unknown []storage.Manga
}

// MarkOptions say how Mark tells a folder's provider.
type MarkOptions struct {
	Loaders []libmangal.ProviderLoader
	// ProviderDirs is download.provider.create_dir, ProviderName the provider name
	// template: a folder's provider directory names its provider when it's the directory
	// (current or old name) of exactly one loaded provider.
	ProviderDirs bool
	ProviderName func(libmangal.ProviderInfo) string
	// OnMarked, if set, is told each folder marked as soon as it is.
	OnMarked func(Marked)
}

// Mark writes a libmangal.MangaDirInfo (as libmangal does on download) into the folders of
// the library mangas that have none, from what's known, most trusted first: the current
// source (provider and manga id, an old provider id as the current one); else the
// provider of its provider directory (no manga id: chapter URLs don't tell it); else only
// the title and metadata ids. A marker without a manga id never matches a download by
// provider (libmangal.MatchMangaDir needs the id), only by metadata id as the library does.
// Folders with nothing to go by are left alone, an existing marker is never replaced.
func (f Files) Mark(opts MarkOptions) (MarkResult, error) {
	var res MarkResult
	mangas, err := f.Library.Mangas()
	if err != nil {
		return res, err
	}
	for i, m := range mangas {
		report(f.OnProgress, Progress{Phase: "marking", Done: i + 1, Total: len(mangas), Item: m.Title})
		if m.Dir == "" {
			continue
		}
		if exists, err := afero.DirExists(f.FS, m.Path()); err != nil {
			return res, err
		} else if !exists {
			continue
		}
		if _, err := libmangal.ReadMangaDirInfo(f.FS, m.Path()); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return res, err
		}
		info, evidence := f.markerOf(m, opts)
		if evidence == "" {
			res.Unknown = append(res.Unknown, m)
			continue
		}
		if err := libmangal.WriteMangaDirInfo(f.FS, m.Path(), info, f.Modes.File); err != nil {
			return res, fmt.Errorf("mark %s: %w", m.Path(), err)
		}
		marked := Marked{Manga: m, Info: info, Evidence: evidence}
		res.Marked = append(res.Marked, marked)
		if opts.OnMarked != nil {
			opts.OnMarked(marked)
		}
	}
	return res, nil
}

// markerOf is the marker of the manga's folder and what it's from, no evidence when
// there's nothing to go by.
func (f Files) markerOf(m storage.Manga, opts MarkOptions) (libmangal.MangaDirInfo, MarkEvidence) {
	ids := slices.Clone(m.MetadataIDs)
	for _, tag := range idTag.FindAllStringSubmatch(m.Dir, -1) {
		code := metadata.IDCode(tag[1])
		id := metadata.ID{Raw: tag[2], Source: idSources[code], Code: code}
		if !slices.ContainsFunc(ids, func(o metadata.ID) bool { return o.Source == id.Source && o.Raw == id.Raw }) {
			ids = append(ids, id)
		}
	}
	info := libmangal.MangaDirInfo{Manga: mangadata.MangaInfo{Title: m.Title}, MetadataIDs: ids}
	if len(m.Sources) != 0 {
		src := m.Sources[0]
		info.Provider = src.Provider
		if loader, alias, ok := libmangal.FindLoader(opts.Loaders, src.Provider); ok && alias != nil {
			info.Provider = loader.Info().ID
		}
		info.Manga = mangadata.MangaInfo{Title: src.Title, ID: src.MangaID}
		return info, MarkedFromSource
	}
	if provider, ok := f.dirProvider(m, opts); ok {
		info.Provider = provider
		return info, MarkedFromDir
	}
	if len(ids) != 0 {
		return info, MarkedFromIDs
	}
	return info, ""
}

// dirProvider is the provider whose directory (current or old name) the manga's folder is
// in, false unless exactly one loaded provider has that name.
func (f Files) dirProvider(m storage.Manga, opts MarkOptions) (string, bool) {
	if !opts.ProviderDirs || opts.ProviderName == nil || filepath.Dir(m.Parent) != filepath.Clean(f.Root) {
		return "", false
	}
	name := filepath.Base(m.Parent)
	var found []string
	for _, loader := range opts.Loaders {
		info := loader.Info()
		names := []string{opts.ProviderName(info)}
		for _, alias := range info.History {
			old := info
			old.ID, old.Name, old.Website, old.History = alias.ID, alias.Name, alias.Website, nil
			if old.Name == "" {
				old.Name = info.Name
			}
			names = append(names, opts.ProviderName(old))
		}
		if slices.Contains(names, name) {
			found = append(found, info.ID)
		}
	}
	if len(found) != 1 {
		return "", false
	}
	return found[0], true
}
