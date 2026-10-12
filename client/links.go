package client

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/kitsu"
	"github.com/luevano/mangal/v5/client/myanimelist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/template"
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
// AniList's MyAnimeList ids (batched), the links of their MangaDex sources.
func LibraryBackups() (library.BackupFinder, error) {
	files, err := LibraryFiles()
	if err != nil {
		return library.BackupFinder{}, err
	}
	return library.BackupFinder{Files: files, AniList: anilist.MetadataByIDsBatch, ProviderIDs: providerLinks}, nil
}

// providerLinks are the metadata ids a MangaDex manga links to, none for other providers
// (they have no such links) or a provider that isn't installed.
func providerLinks(ctx context.Context, src storage.Source) ([]metadata.ID, error) {
	if !strings.Contains(src.Provider, "mangadex") {
		return nil, nil
	}
	c, _, err := AcquireByID(ctx, src.Provider)
	if errors.Is(err, ErrProviderNotFound) {
		return nil, nil
	}
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
