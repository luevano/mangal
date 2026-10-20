package core

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/luevano/libmangal"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// Library reads the library: its mangas, downloads, bindings, trash, journal and read
// history. Scans reconcile the library's rows with the files (never touching them), so
// they're here and not Changes.
type Library interface {
	// Root is the download directory.
	Root(ctx context.Context) (string, error)
	Mangas(ctx context.Context, opts MangasOptions) (MangaList, error)
	// Manga is the library manga with the id, a *NotFoundError when there's none.
	Manga(ctx context.Context, id int64) (storage.Manga, error)
	// MangaBySource is the library manga the provider's manga was last downloaded into.
	MangaBySource(ctx context.Context, provider, mangaID string) (storage.Manga, bool, error)
	// MangaFor is the library manga the provider's manga (with its metadata ids) is
	// downloaded into: the one it was downloaded into, else a folder sharing an id.
	MangaFor(ctx context.Context, provider libmangal.ProviderInfo, mangaID string, ids []lmmeta.ID) (storage.Manga, bool, error)
	// Downloads are the library manga's downloaded chapters.
	Downloads(ctx context.Context, manga int64) ([]storage.Download, error)
	// ProviderDownloads are the downloads of a provider and manga (empty for any), by
	// manga and chapter.
	ProviderDownloads(ctx context.Context, provider, mangaID string) ([]storage.Download, error)
	ChapterFiles(ctx context.Context, req ChapterFilesReq) (ChapterFiles, error)
	// Bindings are the metadata bindings of a provider and manga (empty for any).
	Bindings(ctx context.Context, provider, mangaID string) ([]storage.Binding, error)
	// MangaBindings are the bindings of the library manga's sources, then its own ids.
	MangaBindings(ctx context.Context, id int64) ([]storage.Binding, error)
	// Links are the library manga's metadata links: each source's ids from its sources'
	// bindings, its own ids and its folder.
	Links(ctx context.Context, id int64) (library.LinkInfo, error)
	// LinkPick is the ids of the source the library manga's link check left to choose
	// from (a dismissed pick too), false when there's none.
	LinkPick(ctx context.Context, id int64, source lmmeta.IDCode) (storage.LinkPick, bool, error)
	// Trash is what's in the trash, oldest first.
	Trash(ctx context.Context) ([]library.TrashBatch, error)
	// Ops are the journal's operations, newest first (limit 0: all).
	Ops(ctx context.Context, limit int) ([]storage.Op, error)
	// History is the read chapters (of the provider, or its manga), last read first.
	History(ctx context.Context, provider, mangaID string) ([]storage.Read, error)
	// Duplicates are the library mangas sharing a metadata id, the one to keep suggested.
	Duplicates(ctx context.Context) ([]library.Duplicate, error)
	Scan(ctx context.Context, opts ScanOptions) (ScanReport, error)
	ScanManga(ctx context.Context, id int64) (MangaScan, error)
	CheckIDs(ctx context.Context) ([]library.IDCheck, error)
	// SourceURL is the web page of the library manga's source: the recorded one, else
	// (or with refresh) looked up on its provider and recorded.
	SourceURL(ctx context.Context, manga int64, provider, mangaID string, refresh bool) (string, error)
	// LookupSourceURLs looks the urls of the library's sources up and records them.
	LookupSourceURLs(ctx context.Context, opts SourceURLsOptions) (SourceURLsReport, error)
}

// MangasOptions are what Library.Mangas does besides listing.
type MangasOptions struct {
	// Statuses updates the publication statuses first, from the cached AniList metadata
	// (Fetch looks the unknown and old ones up).
	Statuses, Fetch bool `json:",omitempty"`
	// Missing lists the mangas whose folder is gone.
	Missing bool `json:",omitempty"`
}

// MangaList is the library's mangas.
type MangaList struct {
	Mangas []storage.Manga `json:"mangas"`
	// Missing are the ids of the mangas whose folder is gone (MangasOptions.Missing).
	Missing []int64 `json:"missing,omitempty"`
	// StatusError is why the statuses couldn't be updated, the list is there anyway.
	StatusError string `json:"status_error,omitempty"`
}

// IsMissing is true when the manga's folder is gone.
func (l MangaList) IsMissing(id int64) bool { return slices.Contains(l.Missing, id) }

// ChapterFilesReq is a provider's manga whose chapters are looked for in the library and
// the read history.
type ChapterFilesReq struct {
	Provider    libmangal.ProviderInfo `json:"provider"`
	MangaID     string                 `json:"manga_id"`
	MetadataIDs []lmmeta.ID            `json:"metadata_ids,omitempty"`
	// Scan checks the library manga's folder against the files first.
	Scan bool `json:"scan,omitempty"`
}

// ChapterFiles are where a provider's manga's chapters are.
type ChapterFiles struct {
	// Manga is its library manga, zero when Found is false.
	Manga storage.Manga `json:"manga"`
	Found bool          `json:"found"`
	// Downloads are the library manga's chapters: the library is the one answer to "is
	// it downloaded", whichever provider got it.
	Downloads []storage.Download `json:"downloads,omitempty"`
	// Read are the chapter numbers read (under the provider's current or old ids, or of
	// the library manga from any provider).
	Read []float32 `json:"read,omitempty"`
	// Errors are what couldn't be looked up, the rest is there anyway.
	Errors []string `json:"errors,omitempty"`
}

// MangaScan is a library manga reconciled with its folder.
type MangaScan struct {
	Result library.ScanResult `json:"result"`
	// Manga is the manga after the scan (a folder found elsewhere moves it).
	Manga storage.Manga `json:"manga"`
}

type localLibrary struct {
	core *Core
}

func (l *localLibrary) lib() storage.Library { return l.core.env.Storage.Library() }

func (l *localLibrary) Root(context.Context) (string, error) { return l.core.env.Root(), nil }

func (l *localLibrary) Mangas(ctx context.Context, opts MangasOptions) (MangaList, error) {
	mangas, err := l.lib().Mangas()
	if err != nil {
		return MangaList{}, err
	}
	list := MangaList{Mangas: mangas}
	if opts.Statuses {
		if err := client.UpdateLibraryStatuses(ctx, l.lib(), mangas, opts.Fetch, progressOf(ctx)); err != nil {
			list.StatusError = err.Error()
		}
	}
	if opts.Missing {
		for _, m := range mangas {
			exists, err := afero.DirExists(l.core.env.FS, m.Path())
			if err != nil {
				return list, err
			}
			if !exists {
				list.Missing = append(list.Missing, m.ID)
			}
		}
	}
	return list, nil
}

func (l *localLibrary) Manga(_ context.Context, id int64) (storage.Manga, error) {
	return l.core.manga(id)
}

func (l *localLibrary) MangaBySource(_ context.Context, provider, mangaID string) (storage.Manga, bool, error) {
	return l.lib().MangaBySource(provider, mangaID)
}

func (l *localLibrary) MangaFor(_ context.Context, provider libmangal.ProviderInfo, mangaID string, ids []lmmeta.ID) (storage.Manga, bool, error) {
	match, err := l.lib().Match(provider, mangaID, ids, l.core.env.ProviderDirs())
	return match.Manga, match.Linked, err
}

func (l *localLibrary) Downloads(_ context.Context, manga int64) ([]storage.Download, error) {
	return l.lib().Downloads(manga)
}

func (l *localLibrary) ProviderDownloads(_ context.Context, provider, mangaID string) ([]storage.Download, error) {
	return l.lib().ListDownloads(provider, mangaID)
}

func (l *localLibrary) ChapterFiles(ctx context.Context, req ChapterFilesReq) (ChapterFiles, error) {
	var out ChapterFiles
	var err error
	out.Manga, out.Found, err = l.MangaFor(ctx, req.Provider, req.MangaID, req.MetadataIDs)
	if err != nil {
		out.Errors = append(out.Errors, fmt.Sprintf("library manga: %s", err))
	}
	if out.Found {
		if req.Scan {
			scanner := library.Scanner{FS: l.core.env.FS, Library: l.lib(), OnProgress: progressOf(ctx)}
			if _, err := scanner.ScanManga(out.Manga, l.core.env.Root(), l.core.env.ProviderDirs()); err != nil {
				out.Errors = append(out.Errors, fmt.Sprintf("library: %s", err))
			}
		}
		if out.Downloads, err = l.lib().Downloads(out.Manga.ID); err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("library: %s", err))
		}
	}
	ids := []string{req.Provider.ID}
	for _, alias := range req.Provider.History {
		ids = append(ids, alias.ID)
	}
	read, err := l.core.env.Storage.History().ReadChapters(ids, req.MangaID, out.Manga.ID)
	if err != nil {
		out.Errors = append(out.Errors, fmt.Sprintf("read history: %s", err))
	}
	out.Read = slices.Sorted(maps.Keys(read))
	return out, nil
}

func (l *localLibrary) Bindings(_ context.Context, provider, mangaID string) ([]storage.Binding, error) {
	return l.core.env.Storage.Bindings().List(provider, mangaID)
}

func (l *localLibrary) MangaBindings(_ context.Context, id int64) ([]storage.Binding, error) {
	return l.core.env.Storage.Bindings().OfLibraryManga(id)
}

func (l *localLibrary) Links(ctx context.Context, id int64) (library.LinkInfo, error) {
	m, err := l.core.manga(id)
	if err != nil {
		return library.LinkInfo{}, err
	}
	linker, err := l.core.linker(ctx)
	if err != nil {
		return library.LinkInfo{}, err
	}
	return linker.LinkInfo(m)
}

func (l *localLibrary) LinkPick(_ context.Context, id int64, source lmmeta.IDCode) (storage.LinkPick, bool, error) {
	return l.lib().LinkPick(id, source)
}

func (l *localLibrary) Trash(context.Context) ([]library.TrashBatch, error) {
	return library.Trash(l.core.env.FS, l.core.env.Root())
}

func (l *localLibrary) Ops(_ context.Context, limit int) ([]storage.Op, error) {
	return l.core.env.Storage.Journal().Ops(limit)
}

func (l *localLibrary) History(_ context.Context, provider, mangaID string) ([]storage.Read, error) {
	return l.core.env.Storage.History().List(provider, mangaID)
}

// ScanManga reconciles the library manga with its folder (a missing one is looked for
// under the download directory), like a scan does for every manga: no operation.
func (l *localLibrary) ScanManga(ctx context.Context, id int64) (MangaScan, error) {
	m, err := l.core.manga(id)
	if err != nil {
		return MangaScan{}, err
	}
	files, err := l.core.existingFiles()
	if err != nil {
		return MangaScan{}, err
	}
	scanner := library.Scanner{FS: l.core.env.FS, Library: files.Library, OnProgress: progressOf(ctx)}
	res, err := scanner.ScanManga(m, files.Root, l.core.env.ProviderDirs())
	if err != nil {
		return MangaScan{}, err
	}
	if after, ok, err := files.Library.MangaByID(id); err == nil && ok {
		m = after
	}
	return MangaScan{Result: res, Manga: m}, nil
}
