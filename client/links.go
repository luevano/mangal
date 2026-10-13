package client

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/template"
	mango "github.com/luevano/mangoprovider"
)

// LibraryLinker links library mangas to metadata sources, with the configured name
// template for the folder names the links imply.
func LibraryLinker() (library.Linker, error) {
	files, err := LibraryFiles()
	if err != nil {
		return library.Linker{}, err
	}
	loaders, err := manager.Loaders()
	if err != nil {
		return library.Linker{}, err
	}
	return library.Linker{
		Files:     files,
		Loaders:   loaders,
		MangaName: template.Manga,
		Metadata:  anilist.MetadataByIDs,
	}, nil
}

// MetadataProvider is the metadata source's provider without a provider's client (the
// library's links): AniList, Kitsu when enabled, MyAnimeList when configured.
func MetadataProvider(source metadata.IDSource) (*metadata.ProviderWithCache, error) {
	switch source {
	case metadata.IDSourceAnilist:
		return anilist.Anilist()
	case metadata.IDSourceKitsu:
		if !config.Metadata.Kitsu.Enabled.Get() {
			return nil, errors.New("kitsu isn't enabled (metadata.kitsu.enabled)")
		}
		return kitsu.Kitsu()
	case metadata.IDSourceMyAnimeList:
		if !myanimelist.Configured() {
			return nil, errors.New("myanimelist has no API client (mangal myanimelist client)")
		}
		return myanimelist.MyAnimeList()
	}
	return nil, fmt.Errorf("no metadata provider %v", source)
}

type metadataSet struct{}

func (metadataSet) GetMetadataProvider(source metadata.IDSource) (*metadata.ProviderWithCache, error) {
	return MetadataProvider(source)
}

// LinkMetadata is the metadata of the source's id, to check it before linking a library
// manga to it. MyAnimeList without an API client isn't checked: nil metadata, no error.
func LinkMetadata(ctx context.Context, code metadata.IDCode, id string) (metadata.Metadata, error) {
	if code == metadata.IDCodeMyAnimeList && !myanimelist.Configured() {
		if n, err := strconv.Atoi(id); err != nil || n <= 0 {
			return nil, fmt.Errorf("MyAnimeList id %q isn't a positive integer", id)
		}
		return nil, nil
	}
	return BoundMetadata(ctx, metadataSet{}, code, id)
}

// LibraryBackups finds the Kitsu and MyAnimeList links of library mangas linked to AniList:
// AniList's MyAnimeList ids (batched), the links of their MangaDex sources, Kitsu's
// mappings of the AniList ids (batched, when Kitsu is enabled).
func LibraryBackups() (library.BackupFinder, error) {
	files, err := LibraryFiles()
	if err != nil {
		return library.BackupFinder{}, err
	}
	finder := library.BackupFinder{Files: files, AniList: anilist.MetadataByIDsBatch, ProviderIDs: providerLinks}
	if config.Metadata.Kitsu.Enabled.Get() {
		finder.KitsuIDs = kitsuIDs
	}
	return finder, nil
}

// kitsuIDs maps AniList ids to Kitsu ids with Kitsu's mappings (cached).
func kitsuIDs(ctx context.Context, anilist []int) (map[int]int, error) {
	p, err := kitsu.Kitsu()
	if err != nil {
		return nil, err
	}
	return p.IDsFromContext(ctx, metadata.IDCodeAnilist, anilist)
}

// mangaDexID is the id of mangoprovider's MangaDex, its old ids are in its History.
const mangaDexID = mango.BundleID + "-mangadex"

// isMangaDex is true when the provider with the id (current or old) is MangaDex.
func isMangaDex(loaders []libmangal.ProviderLoader, id string) (libmangal.ProviderLoader, bool) {
	loader, _, ok := libmangal.FindLoader(loaders, id)
	return loader, ok && loader.Info().ID == mangaDexID
}

// providerLinks are the metadata ids a MangaDex manga links to, none for other providers
// (they have no such links) or a provider that isn't installed.
func providerLinks(ctx context.Context, src storage.Source) ([]metadata.ID, error) {
	loaders, err := manager.Loaders()
	if err != nil {
		return nil, err
	}
	loader, ok := isMangaDex(loaders, src.Provider)
	if !ok {
		return nil, nil
	}
	c, err := Acquire(ctx, loader)
	if err != nil {
		return nil, err
	}
	manga, found, err := c.MangaByIDContext(ctx, src.MangaID)
	if err != nil || !found {
		return nil, err
	}
	meta := manga.Metadata()
	if meta == nil {
		return nil, nil
	}
	return meta.ExtraIDs(), nil
}

// LibraryStatuses fills in the publication status of library mangas from AniList: the
// cached metadata, the rest fetched (batched) when asked.
func LibraryStatuses(lib storage.Library) library.StatusFinder {
	return library.StatusFinder{
		Library: lib,
		Cached: func(ids []int) (map[int]metadata.Metadata, error) {
			ani, err := anilist.Anilist()
			if err != nil {
				return nil, err
			}
			return ani.CachedByIDs(ids)
		},
		Fetch: func(ctx context.Context, ids []int) (map[int]metadata.Metadata, error) {
			ani, err := anilist.Anilist()
			if err != nil {
				return nil, err
			}
			_ = anilist.EnsureLogin(ctx)
			return ani.SearchByIDsContext(ctx, ids)
		},
	}
}

// UpdateLibraryStatuses fills in the statuses of the mangas (in place), fetching from
// AniList only when fetch.
func UpdateLibraryStatuses(ctx context.Context, lib storage.Library, mangas []storage.Manga, fetch bool, onProgress func(library.Progress)) error {
	bindings, err := storage.Bindings{DB: lib.DB}.List("", "")
	if err != nil {
		return err
	}
	finder := LibraryStatuses(lib)
	finder.OnProgress = onProgress
	return finder.Update(ctx, mangas, bindings, fetch)
}
