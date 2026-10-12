package library

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// Marked is a manga folder that got a libmangal.MangaDirInfo.
type Marked struct {
	Manga storage.Manga
	Info  libmangal.MangaDirInfo
}

// MarkResult is what Mark did.
type MarkResult struct {
	Marked []Marked
	// Unknown are the folders without a marker nor a known source, left unmarked.
	Unknown []storage.Manga
}

// Mark writes a libmangal.MangaDirInfo (as libmangal does on download) into the folders of
// the library mangas that have none: their current source (an old provider id as the
// loader's current one), its title and manga id, and the manga's metadata ids. Folders
// without a source (adopted from guesses) are left alone, an existing marker is never
// replaced.
func (f Files) Mark(loaders []libmangal.ProviderLoader) (MarkResult, error) {
	var res MarkResult
	mangas, err := f.Library.Mangas()
	if err != nil {
		return res, err
	}
	for _, m := range mangas {
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
		if len(m.Sources) == 0 {
			res.Unknown = append(res.Unknown, m)
			continue
		}
		src := m.Sources[0]
		provider := src.Provider
		if loader, alias, ok := libmangal.FindLoader(loaders, provider); ok && alias != nil {
			provider = loader.Info().ID
		}
		info := libmangal.MangaDirInfo{
			Provider:    provider,
			Manga:       mangadata.MangaInfo{Title: src.Title, ID: src.MangaID},
			MetadataIDs: m.MetadataIDs,
		}
		if err := libmangal.WriteMangaDirInfo(f.FS, m.Path(), info, f.Modes.File); err != nil {
			return res, fmt.Errorf("mark %s: %w", m.Path(), err)
		}
		res.Marked = append(res.Marked, Marked{Manga: m, Info: info})
	}
	return res, nil
}
