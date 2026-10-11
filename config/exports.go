package config

import (
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
)

// DownloadOptions constructs the libmangal.DownloadOptions populated by the Config.
func DownloadOptions() libmangal.DownloadOptions {
	// start from the defaults in case of new additions and build on top of it.
	o := libmangal.DefaultDownloadOptions()
	o.Format = Download.Format.Get()
	o.Directory = Download.Path.Get()
	o.CreateProviderDir = Download.Provider.CreateDir.Get()
	o.CreateMangaDir = Download.Manga.CreateDir.Get()
	o.ReuseMangaDir = Download.Manga.ReuseDir.Get()
	o.CreateVolumeDir = Download.Volume.CreateDir.Get()
	o.SkipIfExists = Download.SkipIfExists.Get()
	o.Strict = Download.Metadata.Strict.Get()
	o.SearchMetadata = Download.Metadata.Search.Get()
	o.DownloadMangaCover = Download.Manga.Cover.Get()
	o.DownloadMangaBanner = Download.Manga.Banner.Get()
	o.WriteSeriesJSON = Download.Metadata.SeriesJSON.Get()
	o.SkipSeriesJSONIfOngoing = Download.Metadata.SkipSeriesJSONIfOngoing.Get()
	o.WriteComicInfoXML = Download.Metadata.ComicInfoXML.Get()
	o.ComicInfoXMLOptions = metadata.DefaultComicInfoOptions()
	o.ComicInfoXMLOptions.LanguageISO = Providers.Filter.Language.Get()
	o.ComicInfoXMLOptions.AddVolume = Download.Metadata.ComicInfoXMLVolume.Get()
	o.ImageTransformer = func(bytes []byte) ([]byte, error) {
		return bytes, nil
	}
	return o
}

func ReadOptions() libmangal.ReadOptions {
	o := libmangal.DefaultReadOptions()
	o.SaveAnilist = Read.History.Anilist.Get()
	// without the provider libmangal would fail the sync
	o.SaveKitsu = Read.History.Kitsu.Get() && Metadata.Kitsu.Enabled.Get()
	o.SaveMyAnimeList = Read.History.MyAnimeList.Get() && Metadata.MyAnimeList.Configured()
	return o
}
