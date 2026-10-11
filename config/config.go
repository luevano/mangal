package config

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"text/template"
	"time"

	"github.com/adrg/xdg"
	"github.com/disgoorg/disgo/webhook"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/template/funcs"
	"github.com/luevano/mangal/v5/theme/icon"
)

// TODO: cleanup the config setup, register each config directly into
// the variable instead of in the config struct?

var (
	dir      string = xdgConfig()
	filename string = fmt.Sprintf("%s.toml", meta.AppName)
	// variable declarations run before init() functions,
	// this ensures the config is initialized before any cmd
	cfg config = initConfig()
)

// Exported config
var (
	// Path to the config file
	Path         = filepath.Join(dir, filename)
	Icons        = cfg.Icons
	Cache        = cfg.Cache
	CLI          = cfg.CLI
	Read         = cfg.Read
	Download     = cfg.Download
	TUI          = cfg.TUI
	Providers    = cfg.Providers
	Library      = cfg.Library
	Notification = cfg.Notification
	Auth         = cfg.Auth
)

func xdgConfig() string {
	var d string
	if runtime.GOOS == "darwin" {
		d = filepath.Join(xdg.Home, ".config", meta.AppName)
	} else {
		d = filepath.Join(xdg.ConfigHome, meta.AppName)
	}
	return d
}

func initConfig() config {
	// setups viper
	Init()
	// create the config struct first to register the values
	c := config{
		Icons: reg(entry[string, icon.Type]{
			Key:         "icons",
			Default:     icon.TypeASCII,
			Description: "Icon format to use.",
			Unmarshal: func(s string) (icon.Type, error) {
				return icon.TypeString(s)
			},
			Marshal: func(i icon.Type) (string, error) {
				return i.String(), nil
			},
		}),
		Cache: configCache{
			Path: reg(entry[string, string]{
				Key:         "cache.path",
				Default:     filepath.Join(xdg.CacheHome, meta.AppName),
				Description: "Path where cache will be written to.",
				Unmarshal: func(s string) (string, error) {
					return expandPath(s)
				},
				Validate: func(s string) error {
					if s == "" {
						return fmt.Errorf("cache path is empty")
					}
					return nil
				},
			}),
			TTL: reg(entry[string, string]{
				Key:         "cache.ttl",
				Default:     "24h",
				Description: `Time to live. A duration string is a possibly signed sequence of decimal numbers, each with optional fraction and a unit suffix, such as "300ms", "-1.5h" or "2h45m". Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".`,
				Validate: func(s string) error {
					_, err := time.ParseDuration(s)
					return err
				},
			}),
		},
		CLI: configCLI{
			ColoredHelp: reg(entry[bool, bool]{
				Key:         "cli.colored_help",
				Default:     true,
				Description: "Enable colors in cli help.",
			}),
		},
		Read: configRead{
			Format: reg(entry[string, libmangal.Format]{
				Key:         "read.format",
				Default:     libmangal.FormatPDF,
				Description: "Format to read chapters in.",
				Unmarshal: func(s string) (libmangal.Format, error) {
					return libmangal.FormatString(s)
				},
				Marshal: func(format libmangal.Format) (string, error) {
					return format.String(), nil
				},
			}),
			History: configReadHistory{
				Anilist: reg(entry[bool, bool]{
					Key:         "read.history.anilist",
					Default:     true,
					Description: "Sync to Anilist reading history if logged in.",
				}),
				Local: reg(entry[bool, bool]{
					Key:         "read.history.local",
					Default:     true,
					Description: "Save to local history.",
				}),
			},
			DownloadOnRead: reg(entry[bool, bool]{
				Key:         "read.download_on_read",
				Default:     false,
				Description: "Download chapter to the configured directory when opening for reading.",
			}),
		},
		Download: configDownload{
			Path: reg(entry[string, string]{
				Key:         "download.path",
				Default:     xdg.UserDirs.Download,
				Description: "Path where chapters will be downloaded.",
				Unmarshal: func(s string) (string, error) {
					return expandPath(s)
				},
				Validate: func(s string) error {
					if s == "" {
						return fmt.Errorf("download path is empty")
					}
					return nil
				},
			}),
			Format: reg(entry[string, libmangal.Format]{
				Key:         "download.format",
				Default:     libmangal.FormatPDF,
				Description: "Format to download chapters in.",
				Unmarshal: func(s string) (libmangal.Format, error) {
					return libmangal.FormatString(s)
				},
				Marshal: func(format libmangal.Format) (string, error) {
					return format.String(), nil
				},
			}),
			UserAgent: reg(entry[string, string]{
				Key:         "download.user_agent",
				Default:     "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:126.0) Gecko/20100101 Firefox/126.0",
				Description: "User-Agent to use for making HTTP requests.",
			}),
			ModeDir: reg(entry[int64, fs.FileMode]{
				Key:         "download.mode_dir_decimal",
				Default:     fs.FileMode(0o755),
				Description: "Permission bits used for all dirs created. Encodes into `int64` (decimal).",
				Unmarshal: func(i int64) (fs.FileMode, error) {
					return fs.FileMode(i), nil
				},
				Marshal: func(mode fs.FileMode) (int64, error) {
					return int64(mode), nil
				},
			}),
			ModeFile: reg(entry[int64, fs.FileMode]{
				Key:         "download.mode_file_decimal",
				Default:     fs.FileMode(0o644),
				Description: "Permission bits used for all files created. Encodes into `int64` (decimal).",
				Unmarshal: func(i int64) (fs.FileMode, error) {
					return fs.FileMode(i), nil
				},
				Marshal: func(mode fs.FileMode) (int64, error) {
					return int64(mode), nil
				},
			}),
			ModeDB: reg(entry[int64, fs.FileMode]{
				Key:         "download.mode_db_decimal",
				Default:     fs.FileMode(0o600),
				Description: "Permission bits used for database files created. Encodes into `int64` (decimal).",
				Unmarshal: func(i int64) (fs.FileMode, error) {
					return fs.FileMode(i), nil
				},
				Marshal: func(mode fs.FileMode) (int64, error) {
					return int64(mode), nil
				},
			}),
			SkipIfExists: reg(entry[bool, bool]{
				Key:         "download.skip_if_exists",
				Default:     true,
				Description: "Skip downloading chapter if its already downloaded (exists at path). Metadata will still be created if needed.",
			}),
			Provider: configDownloadProvider{
				CreateDir: reg(entry[bool, bool]{
					Key:         "download.provider.create_dir",
					Default:     false,
					Description: "Create provider directory.",
				}),
				NameTemplate: reg(entry[string, string]{
					Key:         "download.provider.name_template",
					Default:     "{{ .Name | sanitize }}",
					Description: "Template to use for naming downloaded providers.", // TODO: change these generic descriptions
					Validate: func(s string) error {
						_, err := template.
							New("").
							Funcs(funcs.FuncMap).
							Parse(s)
						return err
					},
				}),
			},
			Manga: configDownloadManga{
				CreateDir: reg(entry[bool, bool]{
					Key:         "download.manga.create_dir",
					Default:     true,
					Description: "Create manga directory.",
				}),
				ReuseDir: reg(entry[bool, bool]{
					Key:         "download.manga.reuse_dir",
					Default:     true,
					Description: "Download into the directory that already has the manga (found by its .libmangal.json) even if the name template now gives another name.",
				}),
				Cover: reg(entry[bool, bool]{
					Key:         "download.manga.cover",
					Default:     false,
					Description: "Download manga cover.",
				}),
				Banner: reg(entry[bool, bool]{
					Key:         "download.manga.banner",
					Default:     false,
					Description: "Download manga banner.",
				}),
				NameTemplate: reg(entry[string, string]{
					Key:         "download.manga.name_template",
					Default:     `{{ .Metadata.String | sanitize }}`,
					Description: "Template to use for naming downloaded mangas, when valid Metadata is available.",
					Validate: func(s string) error {
						_, err := template.
							New("").
							Funcs(funcs.FuncMap).
							Parse(s)
						return err
					},
				}),
				NameTemplateFallback: reg(entry[string, string]{
					Key:         "download.manga.name_template_fallback",
					Default:     `{{ .Manga.Title | sanitize }}`,
					Description: "Template to use for naming downloaded mangas, when no valid Metadata is available.",
					Validate: func(s string) error {
						_, err := template.
							New("").
							Funcs(funcs.FuncMap).
							Parse(s)
						return err
					},
				}),
			},
			Volume: configDownloadVolume{
				CreateDir: reg(entry[bool, bool]{
					Key:         "download.volume.create_dir",
					Default:     false,
					Description: "Create volume directory",
				}),
				NameTemplate: reg(entry[string, string]{
					Key:         "download.volume.name_template",
					Default:     `{{ printf "Vol. %.1f" .Volume.Number | sanitize }}`,
					Description: "Template to use for naming downloaded volumes.",
					Validate: func(s string) error {
						_, err := template.
							New("").
							Funcs(funcs.FuncMap).
							Parse(s)
						return err
					},
				}),
			},
			Chapter: configDownloadChapter{
				NameTemplate: reg(entry[string, string]{
					Key:         "download.chapter.name_template",
					Default:     `{{ printf "[%06.1f] %s" .Chapter.Number .Chapter.Title | sanitize }}`,
					Description: "Template to use for naming downloaded chapters.",
					Validate: func(s string) error {
						_, err := template.
							New("").
							Funcs(funcs.FuncMap).
							Parse(s)

						return err
					},
				}),
			},
			Metadata: configDownloadMetadata{
				Strict: reg(entry[bool, bool]{
					Key:         "download.metadata.strict",
					Default:     true,
					Description: "If metadata is invalid (nil/missing necessary fields) or if during metadata/banner/cover creation an error occurs the chapter won't be downloaded. Some metadata is potentially written to disk.",
				}),
				Search: reg(entry[bool, bool]{
					Key:         "download.metadata.search",
					Default:     true,
					Description: "Search metadata with the available metadata providers, replaces the incoming (from providers usually) metadata even if not found. Priority is always to search by ID if available then by title.",
				}),
				SeriesJSON: reg(entry[bool, bool]{
					Key:         "download.metadata.series_json",
					Default:     true,
					Description: "Generate `series.json` file.",
				}),
				SkipSeriesJSONIfOngoing: reg(entry[bool, bool]{
					Key:         "download.metadata.skip_series_json_if_ongoing",
					Default:     true,
					Description: "Avoid writing the `series.json` file if the manga is ongoing (publishing).",
				}),
				ComicInfoXML: reg(entry[bool, bool]{
					Key:         "download.metadata.comicinfo_xml",
					Default:     true,
					Description: "Generate `ComicInfo.xml` file.",
				}),
				ComicInfoXMLVolume: reg(entry[bool, bool]{
					Key:         "download.metadata.comicinfo_xml_volume",
					Default:     false,
					Description: "Write the volume number in `ComicInfo.xml`. Off by default, most scrapers put every chapter in volume 1.",
				}),
			},
		},
		TUI: configTUI{
			SkipHome: reg(entry[bool, bool]{
				Key:         "tui.skip_home",
				Default:     true,
				Description: "Skip home view when starting mangal and go straight to the providers view.",
			}),
			ShowBreadcrumbs: reg(entry[bool, bool]{
				Key:         "tui.show_breadcrumbs",
				Default:     true,
				Description: `Show the view history breadcrumb at the top left of the screen. For example "Providers > MangaDex > 'Manga name'".`,
			}),
			ExpandAllVolumes: reg(entry[bool, bool]{
				Key:         "tui.expand_all_volumes",
				Default:     false,
				Description: "Skip selecting volumes. Supersedes `tui.expand_single_volume`.",
			}),
			ExpandSingleVolume: reg(entry[bool, bool]{
				Key:         "tui.expand_single_volume",
				Default:     true,
				Description: "Skip selecting volume if there's only one.",
			}),
			Chapter: configTUIChapter{
				// TODO: add validation to the format
				VolumeNumberFormat: reg(entry[string, string]{
					Key:         "tui.chapter.volume_number_format",
					Default:     "[V.%s]",
					Description: "Format that the chapter's volume number (string) should take, for example '[V.%s]'.",
				}),
				ShowVolumeNumber: reg(entry[bool, bool]{
					Key:         "tui.chapter.show_volume_number",
					Default:     true,
					Description: "If the chapter's volume number should be shown prepended to the chapter title.",
				}),
				// TODO: add validation to the format
				NumberFormat: reg(entry[string, string]{
					Key:         "tui.chapter.number_format",
					Default:     "[%06.1f]",
					Description: "Format that the chapter number (float 32) should take, for example '[%06.1f]'.",
				}),
				ShowNumber: reg(entry[bool, bool]{
					Key:         "tui.chapter.show_number",
					Default:     true,
					Description: "If the chapter number should be shown prepended to the chapter title.",
				}),
				ShowDate: reg(entry[bool, bool]{
					Key:         "tui.chapter.show_date",
					Default:     true,
					Description: "If the chapter date should be shown in the description.",
				}),
				ShowGroup: reg(entry[bool, bool]{
					Key:         "tui.chapter.show_group",
					Default:     true,
					Description: "If the chapter scanlation group should be shown in the description.",
				}),
			},
		},
		Providers: configProviders{
			Path: reg(entry[string, string]{
				Key:         "providers.path",
				Default:     filepath.Join(dir, "providers"),
				Description: "Path where providers will be placed/looked for.",
				Unmarshal: func(s string) (string, error) {
					return expandPath(s)
				},
				Validate: func(s string) error {
					if s == "" {
						return fmt.Errorf("providers path is empty")
					}
					return nil
				},
			}),
			Parallelism: reg(entry[int64, uint8]{
				Key:         "providers.parallelism",
				Default:     15,
				Description: "Parallelism to use for the scrapers that support it.",
				Unmarshal: func(i int64) (uint8, error) {
					return uint8(i), nil
				},
				Marshal: func(i uint8) (int64, error) {
					return int64(i), nil
				},
			}),
			Headless: configProvidersHeadless{
				UseFlaresolverr: reg(entry[bool, bool]{
					Key:         "providers.headless.use_flaresolverr",
					Default:     false,
					Description: "If Flaresolverr should be used for headless providers, requires a flaresolverr service to connect to. providers.headless.flaresolverr_url needs to be set.",
				}),
				FlaresolverrURL: reg(entry[string, string]{
					Key:         "providers.headless.flaresolverr_url",
					Default:     "http://localhost:8191/v1",
					Description: "Flaresolverr URL to use for headless providers.",
				}),
			},
			Filter: configProvidersFilter{
				NSFW: reg(entry[bool, bool]{
					Key:         "providers.filter.nsfw",
					Default:     true,
					Description: "If NSFW content should be included, usually used for MangaDex.",
				}),
				// TODO: add validation to language filter
				Language: reg(entry[string, string]{
					Key:         "providers.filter.language",
					Default:     "en",
					Description: "The language the manga should be on.",
				}),
				AvoidDuplicateChapters: reg(entry[bool, bool]{
					Key:         "providers.filter.avoid_duplicate_chapters",
					Default:     true,
					Description: "Only select one chapter when multiple of the same number are present.",
				}),
				ShowUnavailableChapters: reg(entry[bool, bool]{
					Key:         "providers.filter.show_unavailable_chapters",
					Default:     false,
					Description: "When there are non-downloadable chapters, show them anyways. Should only be used to search around.",
				}),
			},
			MangaDex: configProvidersMangaDex{
				DataSaver: reg(entry[bool, bool]{
					Key:         "providers.mangadex.data_saver",
					Default:     false,
					Description: `Use MangaDex "data-saver" option for chapter pages.`,
				}),
			},
			MangaPlus: configProvidersMangaPlus{
				Quality: reg(entry[string, string]{
					Key:         "providers.mangaplus.quality",
					Default:     "super_high",
					Description: `MangaPlus page image quality, one of "low", "high" or "super_high".`,
					Validate: func(s string) error {
						if s != "low" && s != "high" && s != "super_high" {
							return fmt.Errorf("MangaPlus image quality %q not supported, needs to be one of %q, %q or %q", s, "low", "high", "super_high")
						}
						return nil
					},
				}),
				OSVersion: reg(entry[string, string]{
					Key:         "providers.mangaplus.os_version",
					Default:     "36",
					Description: "The OS Version used for the MangaPlus API calls.",
				}),
				AppVersion: reg(entry[string, string]{
					Key:         "providers.mangaplus.app_version",
					Default:     "265",
					Description: "The App Version used for the MangaPlus API calls.",
				}),
				AndroidID: reg(entry[string, string]{
					Key:         "providers.mangaplus.android_id",
					Default:     "",
					Description: "The Android ID used for the MangaPlus API calls. If empty will be randomly generated.",
				}),
				Viewer: reg(entry[string, string]{
					Key:         "providers.mangaplus.viewer",
					Default:     "app",
					Description: `MangaPlus chapter viewer, "app" (same as the official app, needed for subscription images) or "legacy" (deprecated, old endpoint).`,
					Validate: func(s string) error {
						if s != "app" && s != "legacy" {
							return fmt.Errorf("MangaPlus viewer %q not supported, needs to be one of %q or %q", s, "app", "legacy")
						}
						return nil
					},
				}),
				RequestInterval: reg(entry[string, string]{
					Key:         "providers.mangaplus.request_interval",
					Default:     "500ms",
					Description: `Minimum time between MangaPlus API requests, a duration string such as "500ms" or "1s". Warning: lowering or removing it ("0s") risks getting the device locked out for 10+ minutes.`,
					Validate: func(s string) error {
						d, err := time.ParseDuration(s)
						if err != nil {
							return err
						}
						if d < 0 {
							return fmt.Errorf("MangaPlus request interval %q can't be negative", s)
						}
						return nil
					},
				}),
				FreeSecret: reg(entry[string, string]{
					Key:         "providers.mangaplus.free_secret",
					Default:     "",
					Description: "Device secret of a free MangaPlus device, saved automatically the first time one is registered so it's reused. Only used when there's no secret_file.",
				}),
				SecretFile: reg(entry[string, string]{
					Key:         "providers.mangaplus.secret_file",
					Default:     "",
					Description: "Path to a file with the device secret from the official MangaPlus app, for subscription chapters. If empty a new device is registered (free chapters only).",
					Unmarshal: func(s string) (string, error) {
						return expandPath(s)
					},
				}),
			},
		},
		Library: configLibrary{
			Path: reg(entry[string, string]{
				Key:         "library.path",
				Default:     "",
				Description: "Path to the manga library. Empty string will fallback to the download.path.",
				Unmarshal: func(s string) (string, error) {
					return expandPath(s)
				},
			}),
		},
		Auth: configAuth{
			Keyring: reg(entry[bool, bool]{
				Key:         "auth.keyring",
				Default:     false,
				Description: "Keep the logins and secrets (Anilist/MyAnimeList tokens, MangaPlus secrets) in the OS keyring (Secret Service on Linux, Keychain on macOS, Credential Manager on Windows) instead of mangal's database. They move on the next run after changing it.",
			}),
		},
		Notification: configNotification{
			IncludeExisting: reg(entry[bool, bool]{
				Key:         "notification.include_existing",
				Default:     false,
				Description: "If existing downloaded chapters should be included in the notification. If false and all chapters already existed, no notification is sent.",
			}),
			IncludeDirectory: reg(entry[bool, bool]{
				Key:         "notification.include_directory",
				Default:     true,
				Description: "If the full chapter download directory should be included, only unique directories are shown.",
			}),
			Discord: configNotificationDiscord{
				Username: reg(entry[string, string]{
					Key:         "notification.discord.username",
					Default:     "Mangal",
					Description: "Username of the message sender. If empty it will use the configured name in the server.",
				}),
				WebhookURL: reg(entry[string, string]{
					Key:         "notification.discord.webhook_url",
					Default:     "",
					Description: "Webhook URL, if present discord notifications will be sent.",
					Validate: func(s string) error {
						if s == "" {
							return nil
						}
						_, err := webhook.NewWithURL(s)
						return err
					},
				}),
			},
		},
	}
	// Load from "default" config paths
	if err := Load(""); err != nil {
		panic(errorf("error loading config from default locations: %s", err.Error()))
	}
	return c
}
