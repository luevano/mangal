package config

import (
	"io/fs"

	"github.com/luevano/libmangal"
)

type configAuth struct {
	Keyring *entry[bool, bool]
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
	Anilist     *entry[bool, bool]
	Local       *entry[bool, bool]
	Kitsu       *entry[bool, bool]
	MyAnimeList *entry[bool, bool]
}

type configMetadata struct {
	Anilist     configMetadataAnilist
	Kitsu       configMetadataKitsu
	MyAnimeList configMetadataMyAnimeList
}

type configMetadataAnilist struct {
	TagRankMin *entry[int64, int]
}

type configMetadataMyAnimeList struct {
	Enabled *entry[bool, bool]
}

type configMetadataKitsu struct {
	Enabled *entry[bool, bool]
}

type configDownload struct {
	Format       *entry[string, libmangal.Format]
	Path         *entry[string, string]
	UserAgent    *entry[string, string]
	ModeDir      *entry[int64, fs.FileMode]
	ModeFile     *entry[int64, fs.FileMode]
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
	ComicInfoXMLVolume      *entry[bool, bool]
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
	Mirrors     *entry[map[string]any, map[string]string]
	Private     configProvidersPrivate
	Filter      configProvidersFilter
	MangaDex    configProvidersMangaDex
	MangaPlus   configProvidersMangaPlus
}

type configCache struct {
	Path      *entry[string, string]
	TTL       *entry[string, string]
	MirrorTTL *entry[string, string]
}

type configProvidersHeadless struct {
	UseFlaresolverr *entry[bool, bool]
	FlaresolverrURL *entry[string, string]
}

type configProvidersPrivate struct {
	Allow     *entry[bool, bool]
	Allowlist *entry[string, []string]
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
	DataSaver     *entry[bool, bool]
	ExcludeGroups *entry[string, []string]
	PreferGroups  *entry[string, []string]
}

type configNotification struct {
	TUI              *entry[bool, bool]
	IncludeExisting  *entry[bool, bool]
	IncludeDirectory *entry[bool, bool]
	Discord          configNotificationDiscord
}

type configNotificationDiscord struct {
	Username   *entry[string, string]
	WebhookURL *entry[string, string]
}

type configData struct {
	Path    *entry[string, string]
	Backups *entry[int64, int]
}

type configWatch struct {
	Schedule          *entry[string, string]
	Jitter            *entry[string, string]
	GapSchedule       *entry[string, string]
	Gaps              *entry[bool, bool]
	ParallelProviders *entry[int64, int]
	Backoff           *entry[int64, int]
	Notify            *entry[string, string]
}

type configLog struct {
	Rotate       *entry[string, string]
	MaxSize      *entry[string, int64]
	KeepDays     *entry[int64, int]
	MaxFiles     *entry[int64, int]
	MaxTotalSize *entry[string, int64]
	Compress     *entry[bool, bool]
}

type configWeb struct {
	Host *entry[string, string]
	Port *entry[int64, int]
}

type configServe struct {
	Host  *entry[string, string]
	Port  *entry[int64, int]
	Token *entry[string, string]
	// TokenOnLoopback requires the token on loopback too.
	TokenOnLoopback *entry[bool, bool]
	Web             *entry[bool, bool]
	// Auth is token or none.
	Auth *entry[string, string]
	// LoopbackScopes are the scopes of the requests from the same machine without a
	// token.
	LoopbackScopes *entry[string, []string]
	// AllowedHosts, AllowedOrigins and TrustedProxies are for reverse proxies.
	AllowedHosts   *entry[string, []string]
	AllowedOrigins *entry[string, []string]
	TrustedProxies *entry[string, []string]
}
