package library

import (
	"cmp"
	"context"
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
	// Broken are the folders whose marker can't be read (invalid JSON), left as they are.
	Broken []storage.Manga
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
	// DryRun only tells what would be marked, nothing is written.
	DryRun bool
}

// Mark writes a libmangal.MangaDirInfo (as libmangal does on download) into the folders of
// the library mangas that have none, from what's known, most trusted first: the current
// source (provider and manga id, an old provider id as the current one); else the
// provider of its provider directory (no manga id: chapter URLs don't tell it); else only
// the title and metadata ids. A marker without a manga id never matches a download by
// provider (libmangal.MatchMangaDir needs the id), only by metadata id as the library does.
// Folders with nothing to go by are left alone, an existing marker is never replaced.
func (f Files) Mark(opts MarkOptions) (MarkResult, error) {
	return f.MarkContext(context.Background(), opts)
}

// MarkContext is Mark, stopped when ctx is done.
func (f Files) MarkContext(ctx context.Context, opts MarkOptions) (MarkResult, error) {
	var res MarkResult
	mangas, err := f.Library.Mangas()
	if err != nil {
		return res, err
	}
	for i, m := range mangas {
		if err := ctx.Err(); err != nil {
			return res, err
		}
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
			res.Broken = append(res.Broken, m)
			continue
		}
		info, evidence := f.markerOf(m, opts)
		if evidence == "" {
			res.Unknown = append(res.Unknown, m)
			continue
		}
		if !opts.DryRun {
			if err := writeDirInfo(f.FS, f.Journal, m.Path(), info, f.Modes.File); err != nil {
				return res, fmt.Errorf("mark %s: %w", m.Path(), err)
			}
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
		for _, old := range OldInfos(info) {
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

// PlanMark is the marker Mark would write into the manga's folder and what it's from, no
// evidence when there's nothing to go by. It reads nothing.
func (f Files) PlanMark(m storage.Manga, opts MarkOptions) (libmangal.MangaDirInfo, MarkEvidence) {
	return f.markerOf(m, opts)
}

// MarkerDisagreement says how the folder's marker disagrees with the library manga's
// sources (old provider ids count as their current one), empty when it doesn't: its
// provider and manga id aren't one of them, or it has no manga id while the manga has a
// source of its provider (missingID). A source that isn't the current one is fine: the
// next download rewrites the marker.
func MarkerDisagreement(m storage.Manga, marker libmangal.MangaDirInfo, loaders []libmangal.ProviderLoader) (what string, missingID bool) {
	if len(m.Sources) == 0 {
		return "", false
	}
	current := func(provider string) string {
		if l, _, ok := libmangal.FindLoader(loaders, provider); ok {
			return l.Info().ID
		}
		return provider
	}
	provider := current(marker.Provider)
	cur := m.Sources[0]
	for _, src := range m.Sources {
		if current(src.Provider) != provider {
			continue
		}
		if marker.Manga.ID == "" {
			return fmt.Sprintf("has no manga id, the library source is %s %s", cur.Provider, cur.MangaID), true
		}
		if src.MangaID == marker.Manga.ID {
			return "", false
		}
	}
	if marker.Provider == "" {
		if marker.Manga.ID == "" {
			return fmt.Sprintf("has no provider, the library source is %s %s", cur.Provider, cur.MangaID), true
		}
	}
	return fmt.Sprintf("says %s %s, not a source of the library manga (it's on %s %s)", cmp.Or(marker.Provider, "no provider"), marker.Manga.ID, cur.Provider, cur.MangaID), false
}

// MarkManga is Mark for one library manga: it writes the folder's marker from what's
// known, replacing one only when it's broken or disagrees with the library
// (MarkerDisagreement; the ids it had for other sources are kept). False when there's
// nothing to do: a marker that agrees, no folder, nothing to go by.
func (f Files) MarkManga(m storage.Manga, opts MarkOptions) (Marked, bool, error) {
	if m.Dir == "" {
		return Marked{}, false, nil
	}
	if exists, err := afero.DirExists(f.FS, m.Path()); err != nil || !exists {
		return Marked{}, false, err
	}
	old, err := libmangal.ReadMangaDirInfo(f.FS, m.Path())
	valid := err == nil
	if valid {
		if what, _ := MarkerDisagreement(m, old, opts.Loaders); what == "" {
			return Marked{}, false, nil
		}
	}
	info, evidence := f.markerOf(m, opts)
	if evidence == "" {
		return Marked{}, false, nil
	}
	if valid {
		for _, id := range old.MetadataIDs {
			if !slices.ContainsFunc(info.MetadataIDs, func(o metadata.ID) bool { return o.Code == id.Code }) {
				info.MetadataIDs = append(info.MetadataIDs, id)
			}
		}
	}
	marked := Marked{Manga: m, Info: info, Evidence: evidence}
	if opts.DryRun {
		return marked, true, nil
	}
	if err := writeDirInfo(f.FS, f.Journal, m.Path(), info, f.Modes.File); err != nil {
		return Marked{}, false, fmt.Errorf("mark %s: %w", m.Path(), err)
	}
	return marked, true, nil
}
