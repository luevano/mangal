package config

import (
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/theme/icon"
)

var (
	dir      = xdgConfig()
	filename = meta.AppName + ".toml"
	// Path to the config file, set by Load.
	Path = filepath.Join(dir, filename)
)

// CheckTemplate validates a name template, set by the template package (which needs the
// config, so config can't call it directly).
var CheckTemplate func(text string) error

func xdgConfig() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(xdg.Home, ".config", meta.AppName)
	}
	return filepath.Join(xdg.ConfigHome, meta.AppName)
}

func boolEntry(key string, def bool, description string) *entry[bool, bool] {
	return reg(entry[bool, bool]{Key: key, Default: def, Description: description})
}

func stringEntry(key, def, description string) *entry[string, string] {
	return reg(entry[string, string]{Key: key, Default: def, Description: description})
}

// pathEntry expands a leading ~ to the home dir.
func pathEntry(key, def, description string, required bool) *entry[string, string] {
	return reg(entry[string, string]{
		Key:         key,
		Default:     def,
		Description: description,
		Unmarshal:   expandPath,
		Marshal:     func(s string) (string, error) { return s, nil },
		Validate: func(s string) error {
			if required && s == "" {
				return fmt.Errorf("the path is empty")
			}
			return nil
		},
	})
}

func durationEntry(key, def, description string, allowNegative bool) *entry[string, string] {
	return reg(entry[string, string]{
		Key:         key,
		Default:     def,
		Description: description,
		Validate: func(s string) error {
			d, err := time.ParseDuration(s)
			if err != nil {
				return err
			}
			if !allowNegative && d < 0 {
				return fmt.Errorf("duration %q can't be negative", s)
			}
			return nil
		},
	})
}

func modeEntry(key string, def fs.FileMode, description string) *entry[int64, fs.FileMode] {
	return reg(entry[int64, fs.FileMode]{
		Key:         key,
		Default:     def,
		Description: description,
		Unmarshal: func(i int64) (fs.FileMode, error) {
			if i < 0 || i > int64(fs.ModePerm) {
				return 0, fmt.Errorf("%d isn't a permission mode (0-%d)", i, fs.ModePerm)
			}
			return fs.FileMode(i), nil
		},
		Marshal: func(mode fs.FileMode) (int64, error) { return int64(mode), nil },
	})
}

func templateEntry(key, def, description string) *entry[string, string] {
	return reg(entry[string, string]{
		Key:         key,
		Default:     def,
		Description: description + ` "mangal templates funcs" lists the functions.`,
		Validate: func(s string) error {
			if CheckTemplate == nil {
				return nil
			}
			return CheckTemplate(s)
		},
	})
}

// enumEntry is stored by name, values lists the names.
func enumEntry[T fmt.Stringer](key string, def T, description string, parse func(string) (T, error), values []string) *entry[string, T] {
	return reg(entry[string, T]{
		Key:         key,
		Default:     def,
		Description: fmt.Sprintf("%s One of: %s.", description, strings.Join(values, ", ")),
		Values:      values,
		Unmarshal: func(s string) (T, error) {
			v, err := parse(s)
			if err != nil {
				return v, fmt.Errorf("%q isn't one of %s", s, strings.Join(values, ", "))
			}
			return v, nil
		},
		Marshal: func(v T) (string, error) { return v.String(), nil },
	})
}

func oneOfEntry(key, def, description string, values ...string) *entry[string, string] {
	return reg(entry[string, string]{
		Key:         key,
		Default:     def,
		Description: description,
		Values:      values,
		Validate: func(s string) error {
			if !slices.Contains(values, s) {
				return fmt.Errorf("%q isn't one of %s", s, strings.Join(values, ", "))
			}
			return nil
		},
	})
}

var webhookPath = regexp.MustCompile(`/api/webhooks/\d+/[\w-]+`)

func validateWebhookURL(s string) error {
	if s == "" {
		return nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || !webhookPath.MatchString(u.Path) {
		return fmt.Errorf("not a discord webhook url (https://discord.com/api/webhooks/<id>/<token>)")
	}
	return nil
}

var Icons = enumEntry("icons", icon.TypeASCII, "Icon format to use (nerd needs a Nerd Font).", icon.TypeString, []string{"ascii", "nerd"})

var Cache = configCache{
	Path: pathEntry("cache.path", filepath.Join(xdg.CacheHome, meta.AppName), "Path where cache will be written to.", true),
	TTL: durationEntry("cache.ttl", "24h",
		`Time to live. A duration string is a possibly signed sequence of decimal numbers, each with optional fraction and a unit suffix, such as "300ms", "-1.5h" or "2h45m". Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".`,
		true),
	MirrorTTL: durationEntry("cache.mirror_ttl", "24h",
		"How long a provider keeps using the mirror that worked when its website was down, before trying its preferred one (providers.mirrors) or website again. Same format as cache.ttl.",
		true),
}

var CLI = configCLI{
	ColoredHelp: boolEntry("cli.colored_help", true, "Enable colors in cli help."),
}

var Read = configRead{
	Format: enumEntry("read.format", libmangal.FormatPDF, "Format to read chapters in.", libmangal.FormatString, libmangal.FormatStrings()),
	History: configReadHistory{
		Anilist: boolEntry("read.history.anilist", true, "Sync to Anilist reading history if logged in."),
		Local:   boolEntry("read.history.local", true, "Save to local history."),
		Kitsu:   boolEntry("read.history.kitsu", false, "Sync to Kitsu reading history if logged in."),
	},
	DownloadOnRead: boolEntry("read.download_on_read", false, "Download chapter to the configured directory when opening for reading."),
}

var Metadata = configMetadata{
	Kitsu: configMetadataKitsu{
		Enabled: boolEntry("metadata.kitsu.enabled", true,
			"Search Kitsu for metadata when Anilist finds none (or fails). Kitsu often lacks the authors, such metadata isn't valid (see download.metadata.strict)."),
	},
}

var Download = configDownload{
	Path:      pathEntry("download.path", xdg.UserDirs.Download, "Path where chapters will be downloaded.", true),
	Format:    enumEntry("download.format", libmangal.FormatPDF, "Format to download chapters in.", libmangal.FormatString, libmangal.FormatStrings()),
	UserAgent: stringEntry("download.user_agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:126.0) Gecko/20100101 Firefox/126.0", "User-Agent to use for making HTTP requests."),
	ModeDir:   modeEntry("download.mode_dir_decimal", 0o755, "Permission bits used for all dirs created. Encodes into `int64` (decimal)."),
	ModeFile:  modeEntry("download.mode_file_decimal", 0o644, "Permission bits used for all files created. Encodes into `int64` (decimal)."),
	SkipIfExists: boolEntry("download.skip_if_exists", true,
		"Skip downloading chapter if its already downloaded (exists at path). Metadata will still be created if needed."),
	Provider: configDownloadProvider{
		CreateDir: boolEntry("download.provider.create_dir", false, "Create provider directory."),
		NameTemplate: templateEntry("download.provider.name_template", "{{ .Name | sanitize }}",
			"Name of the provider directories (with download.provider.create_dir), a Go template of the provider info (.Name, .ID, .Version, ...)."),
	},
	Manga: configDownloadManga{
		CreateDir: boolEntry("download.manga.create_dir", true, "Create manga directory."),
		ReuseDir: boolEntry("download.manga.reuse_dir", true,
			"Download into the directory that already has the manga (found by its .libmangal.json) even if the name template now gives another name."),
		Cover:  boolEntry("download.manga.cover", false, "Download manga cover."),
		Banner: boolEntry("download.manga.banner", false, "Download manga banner."),
		NameTemplate: templateEntry("download.manga.name_template", `{{ .Metadata.String | sanitize }}`,
			"Name of the manga directories when there's valid metadata, a Go template of .Provider, .Manga (the provider's info) and .Metadata (e.g. .Metadata.String, the title)."),
		NameTemplateFallback: templateEntry("download.manga.name_template_fallback", `{{ .Manga.Title | sanitize }}`,
			"Name of the manga directories when there's no valid metadata, a Go template of .Provider, .Manga and .Metadata."),
	},
	Volume: configDownloadVolume{
		CreateDir: boolEntry("download.volume.create_dir", false, "Create volume directory"),
		NameTemplate: templateEntry("download.volume.name_template", `{{ printf "Vol. %.1f" .Volume.Number | sanitize }}`,
			"Name of the volume directories (with download.volume.create_dir), a Go template of .Provider, .Manga and .Volume (e.g. .Volume.Number)."),
	},
	Chapter: configDownloadChapter{
		NameTemplate: templateEntry("download.chapter.name_template", `{{ printf "[%06.1f] %s" .Chapter.Number .Chapter.Title | sanitize }}`,
			"Name of the chapter files (without the extension), a Go template of .Provider, .Manga, .Volume and .Chapter (e.g. .Chapter.Number, .Chapter.Title)."),
	},
	Metadata: configDownloadMetadata{
		Strict: boolEntry("download.metadata.strict", true,
			"If metadata is invalid (nil/missing necessary fields) or if during metadata/banner/cover creation an error occurs the chapter won't be downloaded. Some metadata is potentially written to disk."),
		Search: boolEntry("download.metadata.search", true,
			"Search metadata with the available metadata providers, replaces the incoming (from providers usually) metadata even if not found. Priority is always to search by ID if available then by title."),
		SeriesJSON: boolEntry("download.metadata.series_json", true, "Generate `series.json` file."),
		SkipSeriesJSONIfOngoing: boolEntry("download.metadata.skip_series_json_if_ongoing", true,
			"Avoid writing the `series.json` file if the manga is ongoing (publishing)."),
		ComicInfoXML: boolEntry("download.metadata.comicinfo_xml", true, "Generate `ComicInfo.xml` file."),
		ComicInfoXMLVolume: boolEntry("download.metadata.comicinfo_xml_volume", false,
			"Write the volume number in `ComicInfo.xml`. Off by default, most scrapers put every chapter in volume 1."),
	},
}

var TUI = configTUI{
	SkipHome: boolEntry("tui.skip_home", true, "Skip home view when starting mangal and go straight to the providers view."),
	ShowBreadcrumbs: boolEntry("tui.show_breadcrumbs", true,
		`Show the view history breadcrumb at the top left of the screen. For example "Providers > MangaDex > 'Manga name'".`),
	ExpandAllVolumes:   boolEntry("tui.expand_all_volumes", false, "Skip selecting volumes. Supersedes `tui.expand_single_volume`."),
	ExpandSingleVolume: boolEntry("tui.expand_single_volume", true, "Skip selecting volume if there's only one."),
	Chapter: configTUIChapter{
		// TODO: add validation to the format
		VolumeNumberFormat: stringEntry("tui.chapter.volume_number_format", "[V.%s]",
			"Format that the chapter's volume number (string) should take, for example '[V.%s]'."),
		ShowVolumeNumber: boolEntry("tui.chapter.show_volume_number", true,
			"If the chapter's volume number should be shown prepended to the chapter title."),
		// TODO: add validation to the format
		NumberFormat: stringEntry("tui.chapter.number_format", "[%06.1f]",
			"Format that the chapter number (float 32) should take, for example '[%06.1f]'."),
		ShowNumber: boolEntry("tui.chapter.show_number", true, "If the chapter number should be shown prepended to the chapter title."),
		ShowDate:   boolEntry("tui.chapter.show_date", true, "If the chapter date should be shown in the description."),
		ShowGroup:  boolEntry("tui.chapter.show_group", true, "If the chapter scanlation group should be shown in the description."),
	},
}

var Providers = configProviders{
	Path: pathEntry("providers.path", filepath.Join(dir, "providers"), "Path where providers will be placed/looked for.", true),
	Parallelism: reg(entry[int64, uint8]{
		Key:         "providers.parallelism",
		Default:     15,
		Description: "Parallelism to use for the scrapers that support it (1-255).",
		Unmarshal: func(i int64) (uint8, error) {
			if i < 1 || i > 255 {
				return 0, fmt.Errorf("%d isn't between 1 and 255", i)
			}
			return uint8(i), nil
		},
		Marshal: func(i uint8) (int64, error) { return int64(i), nil },
	}),
	Headless: configProvidersHeadless{
		UseFlaresolverr: boolEntry("providers.headless.use_flaresolverr", false,
			"If Flaresolverr should be used for headless providers, requires a flaresolverr service to connect to. providers.headless.flaresolverr_url needs to be set."),
		FlaresolverrURL: stringEntry("providers.headless.flaresolverr_url", "http://localhost:8191/v1", "Flaresolverr URL to use for headless providers."),
	},
	Mirrors: reg(entry[map[string]any, map[string]string]{
		Key:         "providers.mirrors",
		Default:     map[string]string{},
		Description: "Preferred website by provider id, its website or one of its mirrors (see mangal providers), e.g. mango-manganato = 'https://www.nelomanga.net/'. When it's down the others are tried.",
		Unmarshal: func(raw map[string]any) (map[string]string, error) {
			mirrors := make(map[string]string, len(raw))
			for id, v := range raw {
				s, ok := v.(string)
				if !ok {
					return nil, fmt.Errorf("mirror of %q: expected a string, got %T", id, v)
				}
				mirrors[id] = s
			}
			return mirrors, nil
		},
		Marshal: func(mirrors map[string]string) (map[string]any, error) {
			raw := make(map[string]any, len(mirrors))
			for id, s := range mirrors {
				raw[id] = s
			}
			return raw, nil
		},
		Validate: func(mirrors map[string]string) error {
			for id, mirror := range mirrors {
				u, err := url.Parse(mirror)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					return fmt.Errorf("mirror of %q: %q isn't an http(s) url", id, mirror)
				}
			}
			return nil
		},
	}),
	Filter: configProvidersFilter{
		NSFW: boolEntry("providers.filter.nsfw", true, "If NSFW content should be included, usually used for MangaDex."),
		// TODO: add validation to language filter
		Language: stringEntry("providers.filter.language", "en", "The language the manga should be on."),
		AvoidDuplicateChapters: boolEntry("providers.filter.avoid_duplicate_chapters", true,
			"Only select one chapter when multiple of the same number are present."),
		ShowUnavailableChapters: boolEntry("providers.filter.show_unavailable_chapters", false,
			"When there are non-downloadable chapters, show them anyways. Should only be used to search around."),
	},
	MangaDex: configProvidersMangaDex{
		DataSaver: boolEntry("providers.mangadex.data_saver", false, `Use MangaDex "data-saver" option for chapter pages.`),
	},
	MangaPlus: configProvidersMangaPlus{
		Quality: oneOfEntry("providers.mangaplus.quality", "super_high",
			`MangaPlus page image quality, one of "low", "high" or "super_high".`, "low", "high", "super_high"),
		OSVersion:  stringEntry("providers.mangaplus.os_version", "36", "The OS Version used for the MangaPlus API calls."),
		AppVersion: stringEntry("providers.mangaplus.app_version", "265", "The App Version used for the MangaPlus API calls."),
		AndroidID: reg(entry[string, string]{
			Key:         "providers.mangaplus.android_id",
			Description: "The Android ID used for the MangaPlus API calls. If empty will be randomly generated.",
			Secret:      true,
		}),
		Viewer: oneOfEntry("providers.mangaplus.viewer", "app",
			`MangaPlus chapter viewer, "app" (same as the official app, needed for subscription images) or "legacy" (deprecated, old endpoint).`, "app", "legacy"),
		RequestInterval: durationEntry("providers.mangaplus.request_interval", "500ms",
			`Minimum time between MangaPlus API requests, a duration string such as "500ms" or "1s". Warning: lowering or removing it ("0s") risks getting the device locked out for 10+ minutes.`,
			false),
		FreeSecret: reg(entry[string, string]{
			Key:         "providers.mangaplus.free_secret",
			Description: "Device secret of a free MangaPlus device, saved automatically the first time one is registered so it's reused. Only used when there's no secret_file.",
			Secret:      true,
		}),
		SecretFile: pathEntry("providers.mangaplus.secret_file", "",
			"Path to a file with the device secret from the official MangaPlus app, for subscription chapters. If empty a new device is registered (free chapters only).",
			false),
	},
}

var Auth = configAuth{
	Keyring: boolEntry("auth.keyring", false,
		"Keep the logins and secrets (Anilist/Kitsu/MyAnimeList tokens, MangaPlus secrets) in the OS keyring (Secret Service on Linux, Keychain on macOS, Credential Manager on Windows) instead of mangal's database. They move on the next run after changing it."),
}

var Notification = configNotification{
	TUI: boolEntry("notification.tui", false,
		"Also send the notification after a download in the TUI's download screen (by default only `mangal inline download` sends it)."),
	IncludeExisting: boolEntry("notification.include_existing", false,
		"If existing downloaded chapters should be included in the notification. If false and all chapters already existed, no notification is sent."),
	IncludeDirectory: boolEntry("notification.include_directory", true,
		"If the full chapter download directory should be included, only unique directories are shown."),
	Discord: configNotificationDiscord{
		Username: stringEntry("notification.discord.username", "Mangal",
			"Username of the message sender. If empty it will use the configured name in the server."),
		WebhookURL: reg(entry[string, string]{
			Key:         "notification.discord.webhook_url",
			Description: "Webhook URL, if present discord notifications are sent after `mangal inline download`, and after TUI downloads with notification.tui (not from scripts or the web UI).",
			Secret:      true,
			Validate:    validateWebhookURL,
		}),
	},
}
