package config

import (
	"fmt"
	"io/fs"
	"math"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/dustin/go-humanize"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/util/cron"
	// before the defaults below read the xdg dirs: tests never touch the user's files
	_ "github.com/luevano/mangal/v5/util/testenv"
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

// formatEntry is a fmt format of one value, checked with sample.
func formatEntry(key, def, description string, sample any) *entry[string, string] {
	return reg(entry[string, string]{
		Key:         key,
		Default:     def,
		Description: description,
		Validate: func(s string) error {
			if out := fmt.Sprintf(s, sample); strings.Contains(out, "%!") {
				return fmt.Errorf("%q isn't a format of one value (it gives %q)", s, out)
			}
			return nil
		},
	})
}

// sampleVolume is a volume for formatEntry, formatted as its String.
type sampleVolume struct{}

func (sampleVolume) String() string { return "1" }

// languageCode is a language code as the providers take it: "en", "es-la", "pt-br".
var languageCode = regexp.MustCompile(`^[a-z]{2,3}(-[a-z]{2,4})?$`)

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

// scheduleEntry is a cron schedule (util/cron), empty only when allowEmpty.
func scheduleEntry(key, def, description string, allowEmpty bool) *entry[string, string] {
	return reg(entry[string, string]{
		Key:         key,
		Default:     def,
		Description: description,
		Validate: func(s string) error {
			if s == "" && allowEmpty {
				return nil
			}
			_, err := cron.Parse(s)
			return err
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

func intEntry(key string, def, minimum, maximum int, description string) *entry[int64, int] {
	return reg(entry[int64, int]{
		Key:         key,
		Default:     def,
		Description: description,
		Unmarshal: func(i int64) (int, error) {
			if i < int64(minimum) || i > int64(maximum) {
				return 0, fmt.Errorf("%d isn't between %d and %d", i, minimum, maximum)
			}
			return int(i), nil
		},
		Marshal: func(i int) (int64, error) { return int64(i), nil },
	})
}

// sizeEntry is a size in bytes written like 10MB or 512KiB, 0 for none.
func sizeEntry(key, def, description string) *entry[string, int64] {
	return reg(entry[string, int64]{
		Key:         key,
		Default:     mustParseSize(def),
		Description: description,
		Unmarshal:   parseSize,
		Marshal:     func(n int64) (string, error) { return formatSize(n), nil },
	})
}

func parseSize(s string) (int64, error) {
	n, err := humanize.ParseBytes(s)
	if err != nil {
		return 0, fmt.Errorf("%q isn't a size like 10MB or 512KiB", s)
	}
	if n > math.MaxInt64 {
		return 0, fmt.Errorf("%q is too big", s)
	}
	return int64(n), nil
}

func mustParseSize(s string) int64 {
	n, err := parseSize(s)
	if err != nil {
		panic(err)
	}
	return n
}

// formatSize writes the size back without losing bytes: 10MB, 512KiB, 1234B.
func formatSize(n int64) string {
	for _, u := range []struct {
		name string
		size int64
	}{{"GiB", 1 << 30}, {"GB", 1e9}, {"MiB", 1 << 20}, {"MB", 1e6}, {"KiB", 1 << 10}, {"KB", 1e3}} {
		if n != 0 && n%u.size == 0 {
			return strconv.FormatInt(n/u.size, 10) + u.name
		}
	}
	return strconv.FormatInt(n, 10)
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

// scopesEntry is a list of serve token scopes (read, watch, progress, write).
func scopesEntry(key string, def []string, description string) *entry[string, []string] {
	e := listEntry(key, description)
	e.Default = def
	e.Validate = func(list []string) error {
		for _, s := range list {
			if !slices.Contains(ServeScopes, s) {
				return fmt.Errorf("%q isn't a scope (%s)", s, strings.Join(ServeScopes, ", "))
			}
		}
		return nil
	}
	return e
}

// ServeScopes are the scopes of the serve API's tokens.
var ServeScopes = []string{"read", "watch", "progress", "write"}

// listEntry is a comma separated list, its entries trimmed (empty ones dropped).
func listEntry(key, description string) *entry[string, []string] {
	return reg(entry[string, []string]{
		Key:         key,
		Default:     []string{},
		Description: description,
		Unmarshal: func(raw string) ([]string, error) {
			list := []string{}
			for item := range strings.SplitSeq(raw, ",") {
				if item = strings.TrimSpace(item); item != "" {
					list = append(list, item)
				}
			}
			return list, nil
		},
		Marshal:  func(list []string) (string, error) { return strings.Join(list, ", "), nil },
		Validate: func([]string) error { return nil },
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

// ValidateWebhookURL checks a Discord webhook url, empty is fine (none).
func ValidateWebhookURL(s string) error {
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
		MyAnimeList: boolEntry("read.history.myanimelist", false,
			"Sync to MyAnimeList reading history if logged in (mangal myanimelist)."),
	},
	DownloadOnRead: boolEntry("read.download_on_read", false, "Download chapter to the configured directory when opening for reading."),
}

var Metadata = configMetadata{
	Anilist: configMetadataAnilist{
		TagRankMin: intEntry("metadata.anilist.tag_rank_min", anilist.DefaultTagRankMin, 1, 100,
			"Lowest AniList tag rank (how relevant the tag is to the manga, 1 to 100) kept in the tags, 1 keeps them all. Applies to metadata fetched after the change."),
	},
	Kitsu: configMetadataKitsu{
		Enabled: boolEntry("metadata.kitsu.enabled", true,
			"Search Kitsu for metadata when Anilist finds none (or fails). Kitsu often lacks the authors, such metadata isn't valid (see download.metadata.strict)."),
	},
	MyAnimeList: configMetadataMyAnimeList{
		Enabled: boolEntry("metadata.myanimelist.enabled", true,
			"Search MyAnimeList for metadata when Anilist and Kitsu find none (or fail). Only with a MyAnimeList API client (mangal myanimelist client)."),
	},
}

var Download = configDownload{
	Path:      pathEntry("download.path", xdg.UserDirs.Download, "Path where chapters will be downloaded.", true),
	Format:    enumEntry("download.format", libmangal.FormatPDF, "Format to download chapters in.", libmangal.FormatString, libmangal.FormatStrings()),
	UserAgent: stringEntry("download.user_agent", libmangal.DefaultUserAgent, "User-Agent of the providers' requests, a current Firefox by default (libmangal's, updated with it)."),
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
		VolumeNumberFormat: formatEntry("tui.chapter.volume_number_format", "[V.%s]",
			"Format that the chapter's volume number (string) should take, for example '[V.%s]'.", sampleVolume{}),
		ShowVolumeNumber: boolEntry("tui.chapter.show_volume_number", true,
			"If the chapter's volume number should be shown prepended to the chapter title."),
		NumberFormat: formatEntry("tui.chapter.number_format", "[%06.1f]",
			"Format that the chapter number (float 32) should take, for example '[%06.1f]'.", float32(1)),
		ShowNumber: boolEntry("tui.chapter.show_number", true, "If the chapter number should be shown prepended to the chapter title."),
		ShowDate:   boolEntry("tui.chapter.show_date", true, "If the chapter date should be shown in the description."),
		ShowGroup:  boolEntry("tui.chapter.show_group", true, "If the chapter scanlation group should be shown in the description."),
	},
}

var Providers = configProviders{
	Path: pathEntry("providers.path", filepath.Join(dir, "providers"), "Path where providers will be placed/looked for.", true),
	Parallelism: reg(entry[int64, uint8]{
		Key:         "providers.parallelism",
		Default:     4,
		Description: "How many requests mangoprovider's HTML scrapers make at once (1-255, capped at 4).",
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
		Language: reg(entry[string, string]{
			Key:         "providers.filter.language",
			Default:     "en",
			Description: `The language the manga should be on, a code like "en", "es-la" or "pt-br" (MangaDex's).`,
			Validate: func(s string) error {
				if !languageCode.MatchString(s) {
					return fmt.Errorf("%q isn't a language code like \"en\" or \"pt-br\"", s)
				}
				return nil
			},
		}),
		AvoidDuplicateChapters: boolEntry("providers.filter.avoid_duplicate_chapters", true,
			"Only select one chapter when multiple of the same number are present."),
		ShowUnavailableChapters: boolEntry("providers.filter.show_unavailable_chapters", false,
			"When there are non-downloadable chapters, show them anyways. Should only be used to search around."),
	},
	MangaDex: configProvidersMangaDex{
		DataSaver: boolEntry("providers.mangadex.data_saver", false, `Use MangaDex "data-saver" option for chapter pages.`),
		ExcludeGroups: listEntry("providers.mangadex.exclude_groups",
			`Scanlation groups whose MangaDex chapters are left out, comma separated: names (any case) or group ids (a name with a comma needs the id), e.g. "Group A, 0d8e2a7c-...".`),
		PreferGroups: listEntry("providers.mangadex.prefer_groups",
			`Scanlation groups to take a MangaDex chapter from when there are several of the same number (providers.filter.avoid_duplicate_chapters), first listed first, comma separated names (any case) or group ids. Without one of them, the group with the most chapters in the volume.`),
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
			Description: "Device secret of a free MangaPlus device. mangal saves the one it registers with the logins (auth.keyring) and uses that one; this key is only read when none is saved yet (it's then saved there). Only used when there's no secret_file.",
			Secret:      true,
		}),
		SecretFile: pathEntry("providers.mangaplus.secret_file", "",
			"Path to a file with the device secret from the official MangaPlus app, for subscription chapters. If empty a new device is registered (free chapters only).",
			false),
	},
}

var Auth = configAuth{
	Keyring: boolEntry("auth.keyring", false,
		"Keep the logins and secrets (Anilist/Kitsu/MyAnimeList tokens and API clients, MangaPlus secrets, Discord webhook, serve token) in the OS keyring (Secret Service on Linux, Keychain on macOS, Credential Manager on Windows) instead of mangal's database. They move on the next run after changing it."),
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
			Description: "Webhook URL, if present discord notifications are sent after `mangal inline download`, watch checks, and TUI downloads with notification.tui (not from scripts or the web UI). Only used when none is saved with `mangal notify webhook` (or the TUI accounts).",
			Secret:      true,
			Validate:    ValidateWebhookURL,
		}),
	},
}

var Data = configData{
	Path: pathEntry("data.path", "",
		"Directory of mangal's data database (library, logins, watches), empty for the XDG data directory (~/.local/share/mangal). Another mangal instance (e.g. a second `mangal serve`) uses another config pointing to another directory.",
		false),
	Backups: intEntry("data.backups", 10, 1, 1000,
		"How many snapshots of the data database are kept in its backups folder (the newest). One is taken before each library operation that changes files (rename, merge, move, mark, migrate, delete, emptying the trash) and before an undo; `mangal library undo` restores it."),
}

var Watch = configWatch{
	Schedule: scheduleEntry("watch.schedule", "0 */4 * * *",
		"When the watched mangas are checked for new chapters (by `mangal serve`): a cron schedule in the local time zone (minute hour day-of-month month day-of-week, e.g. \"0 */4 * * *\" every 4 hours on the hour), @hourly, @daily, @weekly, @monthly or @every <duration>. A watch is due once a tick passed since its last check. A watch can have its own (`mangal watch set --schedule`).", false),
	Jitter: durationEntry("watch.jitter", "15m",
		"Up to how long after a tick a provider's watches are checked: each provider starts somewhere between 0 and this (fixed per provider and tick, the same in every mangal process). Out of politeness: the sites don't get every client's requests on the exact hour, and checks at varying times look less like a bot. 0 starts them right on the tick.", false),
	GapSchedule: scheduleEntry("watch.gap_schedule", "",
		"When the watches with gap filling also download the chapters below the last downloaded one the library misses, a cron schedule like watch.schedule (e.g. \"0 3 * * 0\" on sundays at 3:00). Empty turns it off (`mangal watch check --gaps` still does it).", true),
	Gaps: boolEntry("watch.gaps", false, "Gap filling for new watches (`mangal watch add --gaps` sets it per watch)."),
	ParallelProviders: intEntry("watch.parallel_providers", 2, 1, 32,
		"How many providers are checked at the same time, the mangas of one provider are always checked one at a time."),
	Backoff: intEntry("watch.backoff", 3, 0, 1000,
		"After this many failed checks in a row a watch skips ticks of its schedule (waits 2, 4, then 8 ticks). 0 never skips."),
	Notify: oneOfEntry("watch.notify", "both",
		"What watch checks notify (Discord): new chapters, errors, both or none.", "new", "errors", "both", "none"),
}

var Serve = configServe{
	Host: stringEntry("serve.host", "127.0.0.1", "Address `mangal serve` listens on, 127.0.0.1 only accepts local connections."),
	Port: intEntry("serve.port", 6977, 1, 65535, "Port `mangal serve` listens on, one per instance."),
	Token: reg(entry[string, string]{
		Key:         "serve.token",
		Description: "A token of the `mangal serve` API with every scope (Authorization: Bearer <token>), needed when it listens on an address that isn't loopback (see serve.auth, serve.loopback_scopes). mangal's own commands and TUI send the token saved with `mangal serve token` (kept with the logins) instead when there's one; the server takes both.",
		Secret:      true,
	}),
	TokenOnLoopback: boolEntry("serve.token_on_loopback", false, "Require a token from the same machine too, as serve.loopback_scopes = \"\" does (kept for older configs)."),
	Web:             boolEntry("serve.web", false, "Also serve the web UI (as `mangal web`) on the `mangal serve` port (`mangal serve --web`), behind the token when one is required (the browser asks for it: any user name, the token as the password)."),
	Auth: oneOfEntry("serve.auth", "token",
		"How `mangal serve` authenticates: token (Authorization: Bearer <token>; serve.token, the token saved with `mangal serve token` and the named ones it makes, each with its scopes; requests from the same machine without one get serve.loopback_scopes) or none (every request may do everything: only behind something that authenticates, e.g. a reverse proxy).",
		"token", "none"),
	LoopbackScopes: scopesEntry("serve.loopback_scopes", []string{"read", "watch", "progress"},
		"Scopes of the requests from the same machine (a loopback address, not through a proxy) without a token: read (GET, events), watch (add, change, check the watches), progress (the web reader records reads: history, AniList/Kitsu/MyAnimeList progress), write (change files, the library, the config, downloads). Empty requires a token there too. Behind a reverse proxy on the same machine, see serve.trusted_proxies."),
	AllowedHosts: listEntry("serve.allowed_hosts",
		"Host names `mangal serve` answers besides localhost and its address, comma separated (e.g. the name a reverse proxy passes, mangal.example.com). On loopback only localhost is answered otherwise (DNS rebinding); set, it's checked on any address."),
	AllowedOrigins: listEntry("serve.allowed_origins",
		"Origins (https://mangal.example.com) whose pages may change things through `mangal serve` (POST, PUT, PATCH, DELETE): a reverse proxy's public address when the browser's Origin isn't the Host the server sees. Other sites' pages are refused."),
	TrustedProxies: listEntry("serve.trusted_proxies",
		"Addresses or CIDRs (127.0.0.1, 10.0.0.0/8) of reverse proxies whose X-Forwarded-For and X-Forwarded-Host `mangal serve` believes: the client's address (whether it's on the same machine) and the host. Empty believes none: a request with forwarding headers is never treated as local."),
}

var Log = configLog{
	Rotate: oneOfEntry("log.rotate", "daily",
		"How often a new log file starts (in $XDG_STATE_HOME/mangal/logs, named after its period: 2026-10-13T15.log, 2026-10-13.log, 2026-W42.log, 2026-10.log); a long `mangal serve` moves to the next one by itself. Every mangal process (serve, CLI, TUI) appends to the same files.",
		"hourly", "daily", "weekly", "monthly"),
	MaxSize: sizeEntry("log.max_size", "10MB",
		"A log file this big starts the next file of its period (2026-10-13.1.log, .2...), roughly (a line or two over). A size like 10MB, 512KiB; 0 for no limit."),
	KeepDays: intEntry("log.keep_days", 14, 0, 100000,
		"How many days of log files are kept (today's included): the files of older periods are deleted when a process opens a new file. 0 keeps them all (as far as log.max_files and log.max_total_size allow)."),
	MaxFiles: intEntry("log.max_files", 0, 0, 1000000,
		"Most log files kept, the oldest are deleted beyond it (only the ones no process writes to anymore). 0 for no limit."),
	MaxTotalSize: sizeEntry("log.max_total_size", "500MB",
		"Most space the log files take, the oldest are deleted beyond it (only the ones no process writes to anymore). 0 for no limit."),
	Compress: boolEntry("log.compress", false,
		"Gzip the log files no process writes to anymore (.log.gz; `mangal logs` and the TUI's logs read them)."),
}

var Web = configWeb{
	Host: stringEntry("web.host", "127.0.0.1", "Address `mangal web` listens on, 127.0.0.1 only accepts local connections; off loopback a serve token is required (serve.auth)."),
	Port: intEntry("web.port", 6970, 1, 65535, "Port `mangal web` listens on (not 6969, the logins' callback port)."),
}
