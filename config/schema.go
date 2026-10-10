package config

import (
	"io/fs"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/theme/icon"
)

type config struct {
	Icons        *entry[string, icon.Type]
	Cache        configCache
	CLI          configCLI
	Read         configRead
	Download     configDownload
	TUI          configTUI
	Providers    configProviders
	Library      configLibrary
	Notification configNotification
}

type configCLI struct {
	ColoredHelp *entry[bool, bool]
}

type configRead struct {
	Format         *entry[string, libmangal.Format]
	History        configReadHistory
	DownloadOnRead *entry[bool, bool]
}

type configReadHistory struct {
	Anilist *entry[bool, bool]
	Local   *entry[bool, bool]
}

type configDownload struct {
	Format       *entry[string, libmangal.Format]
	Path         *entry[string, string]
	UserAgent    *entry[string, string]
	ModeDir      *entry[int64, fs.FileMode]
	ModeFile     *entry[int64, fs.FileMode]
	ModeDB       *entry[int64, fs.FileMode]
	SkipIfExists *entry[bool, bool]
	Provider     configDownloadProvider
	Manga        configDownloadManga
	Volume       configDownloadVolume
	Chapter      configDownloadChapter
	Metadata     configDownloadMetadata
}

type configDownloadProvider struct {
	CreateDir    *entry[bool, bool]
	NameTemplate *entry[string, string]
}

type configDownloadManga struct {
	CreateDir            *entry[bool, bool]
	ReuseDir             *entry[bool, bool]
	Cover                *entry[bool, bool]
	Banner               *entry[bool, bool]
	NameTemplate         *entry[string, string]
	NameTemplateFallback *entry[string, string]
}

type configDownloadVolume struct {
	CreateDir    *entry[bool, bool]
	NameTemplate *entry[string, string]
}

type configDownloadChapter struct {
	NameTemplate *entry[string, string]
}

type configDownloadMetadata struct {
	Strict                  *entry[bool, bool]
	Search                  *entry[bool, bool]
	ComicInfoXML            *entry[bool, bool]
	SeriesJSON              *entry[bool, bool]
	SkipSeriesJSONIfOngoing *entry[bool, bool]
}

type configTUI struct {
	SkipHome           *entry[bool, bool]
	ShowBreadcrumbs    *entry[bool, bool]
	ExpandAllVolumes   *entry[bool, bool]
	ExpandSingleVolume *entry[bool, bool]
	Chapter            configTUIChapter
}

type configTUIChapter struct {
	VolumeNumberFormat *entry[string, string]
	ShowVolumeNumber   *entry[bool, bool]
	NumberFormat       *entry[string, string]
	ShowNumber         *entry[bool, bool]
	ShowDate           *entry[bool, bool]
	ShowGroup          *entry[bool, bool]
}

type configProviders struct {
	Path        *entry[string, string]
	Parallelism *entry[int64, uint8]
	Headless    configProvidersHeadless
	Filter      configProvidersFilter
	MangaDex    configProvidersMangaDex
	MangaPlus   configProvidersMangaPlus
}

type configCache struct {
	Path *entry[string, string]
	TTL  *entry[string, string]
}

type configProvidersHeadless struct {
	UseFlaresolverr *entry[bool, bool]
	FlaresolverrURL *entry[string, string]
}

type configLibrary struct {
	Path *entry[string, string]
}

type configProvidersFilter struct {
	NSFW                    *entry[bool, bool]
	Language                *entry[string, string]
	AvoidDuplicateChapters  *entry[bool, bool]
	ShowUnavailableChapters *entry[bool, bool]
}

type configProvidersMangaPlus struct {
	Quality         *entry[string, string]
	OSVersion       *entry[string, string]
	AppVersion      *entry[string, string]
	AndroidID       *entry[string, string]
	Viewer          *entry[string, string]
	RequestInterval *entry[string, string]
	FreeSecret      *entry[string, string]
	SecretFile      *entry[string, string]
}

type configProvidersMangaDex struct {
	DataSaver *entry[bool, bool]
}

type configNotification struct {
	IncludeExisting  *entry[bool, bool]
	IncludeDirectory *entry[bool, bool]
	Discord          configNotificationDiscord
}

type configNotificationDiscord struct {
	Username   *entry[string, string]
	WebhookURL *entry[string, string]
}
