package core

import (
	"slices"
	"strings"
)

// Mode is a way to use mangal.
type Mode string

// The modes.
const (
	ModeCLI    Mode = "cli"
	ModeTUI    Mode = "tui"
	ModeScript Mode = "script"
	// ModeAPI is the serve API v1 (web/openapi.yaml).
	ModeAPI Mode = "api"
	// ModeWeb is the web UI (web/ui, its routes by name).
	ModeWeb Mode = "web"
)

// Modes are every mode.
var Modes = []Mode{ModeCLI, ModeTUI, ModeScript, ModeAPI, ModeWeb}

// Feature is something mangal does and where each mode does it, so the modes stay in
// parity: tests check that every cobra command, change kind, TUI key and API operation
// here exists, and every API v1 operation is a feature's (or APIOperations'); the web
// UI's routes are checked by its own test against features.json (go generate ./core).
type Feature struct {
	Name string
	// CLI are its commands ("library merge").
	CLI []string
	// TUI is its key (a base.Key* binding's) and where, TUIKey the key alone.
	TUI, TUIKey string
	// Script is its Lua function ("client.search_mangas"), "api" for mangal.api (every API
	// feature: its operations through the API).
	Script string
	// API are its operationIds in the serve API v1.
	API []string
	// Web are the web UI's routes (vue-router names) that have it.
	Web []string
	// Change is the kind of change it applies, "" for none.
	Change Kind
	// NotApplicable are the modes that don't have it on purpose, with why.
	NotApplicable map[Mode]string
}

// Has is true when the mode has the feature.
func (f Feature) Has(m Mode) bool {
	switch m {
	case ModeCLI:
		return len(f.CLI) != 0
	case ModeTUI:
		return f.TUI != ""
	case ModeScript:
		return f.Script != ""
	case ModeAPI:
		return len(f.API) != 0
	case ModeWeb:
		return len(f.Web) != 0
	}
	return false
}

const (
	cliEnough = "a one-off command, the CLI is enough"
	apiNotYet = "not in the API yet (docs/core.md Progress)"
)

// Features are mangal's features.
var Features = []Feature{
	{Name: "providers", Web: []string{"providers"}, API: []string{"listProviders", "getProviderInfo", "getProviderIcon"}, CLI: []string{"providers ls"}, TUI: "enter (home)", Script: "client.info"},
	{Name: "provider install", CLI: []string{"providers add", "providers rm", "providers new"},
		NotApplicable: map[Mode]string{ModeWeb: "providers run code: never installed over HTTP", ModeAPI: "providers run code: never installed over HTTP", ModeTUI: "git installs are a terminal's job", ModeScript: "a script doesn't install code"}},
	{Name: "provider update", Web: []string{"providers"}, API: []string{"updateProviders"}, Script: "api", CLI: []string{"providers up"},
		NotApplicable: map[Mode]string{ModeTUI: "git updates are a terminal's job"}},
	{Name: "mirrors", Web: []string{"providers"}, API: []string{"setProviderMirror"}, Script: "api", CLI: []string{"providers ls", "config set"}, TUI: "alt+m (providers)", TUIKey: "alt+m"},
	{Name: "search and browse", Web: []string{"search", "provider-manga"}, API: []string{"searchProvider", "getProviderManga", "listProviderChapters", "proxyImage"}, CLI: []string{"inline json"}, TUI: "s (mangas)", TUIKey: "s", Script: "client.search_mangas"},
	{Name: "multi-provider search", Web: []string{"multi-search", "provider-manga"}, API: []string{"searchProvider"}, Script: "api", CLI: []string{"search", "library fill"}, TUI: "space, enter (providers); alt+g (chapters)", TUIKey: "space"},
	{Name: "download", Web: []string{"provider-manga", "downloads"}, API: []string{"startDownload", "listJobs", "getJob", "cancelJob", "retryJob", "removeJob", "cancelAllJobs", "clearJobQueue"}, CLI: []string{"inline download"}, TUI: "d (chapters)", TUIKey: "d", Script: "client.download_chapter"},
	{Name: "maintenance jobs", Web: []string{"jobs", "job-kind"}, API: []string{"listJobKinds", "getJobKind", "runJobKind", "pauseJobKind", "resumeJobKind", "setJobKindSettings"}, Script: "api",
		CLI: []string{"jobs", "jobs run", "jobs set", "jobs pause", "jobs resume"}, TUI: "alt+j (home, downloads)", TUIKey: "alt+j"},
	{Name: "read", Web: []string{"read-library", "read-online"}, API: []string{"listChapterPages", "getChapterPage", "getChapterFile", "listProviderPages", "getProviderPage", "recordRead", "getReaderPosition", "putReaderPosition"}, Script: "api", TUI: "r (chapters)", TUIKey: "r",
		NotApplicable: map[Mode]string{ModeCLI: "reading is interactive (the TUI opens the default app, the web UI has a reader)"}},
	{Name: "read history", Web: []string{"history"}, API: []string{"listHistory"}, Script: "api", CLI: []string{"history"}, TUI: "alt+h (home)", TUIKey: "alt+h"},
	{Name: "history clear", Web: []string{"history"}, API: []string{"clearHistory"}, Script: "api", CLI: []string{"history clear"}, TUI: "d, backspace (read history)", TUIKey: "d", Change: KindHistoryClear},
	{Name: "library list", Web: []string{"library", "library-manga"}, API: []string{"listLibraryMangas", "getLibraryManga", "getLibraryCover", "listLibraryChapters", "listDownloads", "getLibraryMangaZip"}, Script: "api", CLI: []string{"library ls", "library downloads"}, TUI: "L (home)", TUIKey: "L"},
	{Name: "library scan", Web: []string{"library"}, API: []string{"scanLibrary", "scanLibraryManga"}, Script: "api", CLI: []string{"library scan"}, TUI: "u, S (library)", TUIKey: "u"},
	{Name: "merge", Web: []string{"duplicates", "library-manga"}, API: []string{"mergeLibraryMangas", "listDuplicates"}, Script: "api", CLI: []string{"library merge"}, TUI: "J (library, health)", TUIKey: "J", Change: KindMerge},
	{Name: "move", Web: []string{"library-manga"}, API: []string{"moveLibraryManga"}, Script: "api", CLI: []string{"library move"}, TUI: "X (library)", TUIKey: "X", Change: KindMove},
	{Name: "retitle", Web: []string{"library-manga"}, API: []string{"retitleLibraryManga"}, Script: "api", CLI: []string{"library retitle"}, TUI: "alt+t (library manga)", TUIKey: "alt+t", Change: KindRetitle},
	{Name: "source urls", Web: []string{"library-manga"}, API: []string{"getLibrarySourceURL", "lookupSourceURLs"}, Script: "api", CLI: []string{"library urls"}, TUI: "o (library manga)", TUIKey: "o"},
	{Name: "get unavailable chapters elsewhere", Web: []string{"provider-manga"}, API: []string{"fillProviderManga"}, Script: "api", CLI: []string{"library fill"}, TUI: "alt+g (chapters)", TUIKey: "alt+g"},
	{Name: "links", Web: []string{"library-manga"}, API: []string{"linkLibraryManga", "getLibraryLinks"}, Script: "api", CLI: []string{"library bind"}, TUI: "A (library, library manga)", TUIKey: "A", Change: KindLink},
	{Name: "dismiss a link pick", API: []string{"dismissLinkPick"}, Script: "api", CLI: []string{"library bind"}, TUI: "d (links, a pick)", TUIKey: "d", Change: KindDismissPick,
		NotApplicable: map[Mode]string{ModeWeb: "the health page's dismiss hides a pick's finding"}},
	{Name: "backup links", Web: []string{"library"}, API: []string{"fillBackupLinks"}, Script: "api", CLI: []string{"library bind"}, TUI: "B (library, links)", TUIKey: "B", Change: KindBackups},
	{Name: "mark", Web: []string{"library", "library-manga"}, API: []string{"markLibraryMangas"}, Script: "api", CLI: []string{"library mark", "library scan"}, TUI: "x (health)", TUIKey: "x", Change: KindMark},
	{Name: "rename", Web: []string{"library", "library-manga"}, API: []string{"renameLibrary"}, Script: "api", CLI: []string{"library rename"}, TUI: "alt+r (library)", TUIKey: "alt+r", Change: KindRename},
	{Name: "forget", Web: []string{"library-manga"}, API: []string{"forgetLibraryMangas"}, Script: "api", CLI: []string{"library scan", "library rm"}, TUI: "d (library, a missing manga)", TUIKey: "d", Change: KindForget},
	{Name: "delete", Web: []string{"library-manga"}, API: []string{"deleteLibraryManga"}, Script: "api", CLI: []string{"library rm"}, TUI: "d (library, library manga)", TUIKey: "d", Change: KindDelete},
	{Name: "trash", Web: []string{"trash"}, API: []string{"getTrash"}, Script: "api", CLI: []string{"library trash"}, TUI: "t (library)", TUIKey: "t"},
	{Name: "empty the trash", Web: []string{"trash"}, API: []string{"emptyTrash"}, Script: "api", CLI: []string{"library trash empty"}, TUI: "d (trash)", TUIKey: "d", Change: KindEmptyTrash},
	{Name: "undo", Web: []string{"ops"}, API: []string{"listOps", "undoOp"}, Script: "api", CLI: []string{"library undo"}, TUI: "U (library)", TUIKey: "U", Change: KindUndo},
	{Name: "migrate", Web: []string{"library"}, API: []string{"migrateLibrary"}, Script: "api", CLI: []string{"migrate"}, TUI: "X (home)", TUIKey: "X", Change: KindMigrate},
	{Name: "bindings", Web: []string{"provider-manga", "library-manga"}, API: []string{"listBindings", "setBinding"}, Script: "api", CLI: []string{"bind", "bind ls"}, TUI: "A (mangas, volumes, chapters)", TUIKey: "A", Change: KindBind},
	{Name: "unbind", Web: []string{"provider-manga"}, API: []string{"removeBinding"}, Script: "api", CLI: []string{"bind"}, TUI: "d (bindings)", TUIKey: "d", Change: KindUnbind},
	{Name: "metadata search", Web: []string{"provider-manga", "library-manga"}, API: []string{"listMetadataSources", "searchMetadata", "getMetadataIcon"}, CLI: []string{"bind"}, TUI: "s (bindings)", TUIKey: "s", Script: "anilist.search_mangas"},
	{Name: "health", Web: []string{"health"}, API: []string{"getHealth", "getHealthJob", "dismissHealthFindings"}, Script: "api", CLI: []string{"health"}, TUI: "! (home)", TUIKey: "!"},
	{Name: "health fix", Web: []string{"health"}, API: []string{"fixHealthFinding"}, Script: "api", CLI: []string{"health fix"}, TUI: "x (health)", TUIKey: "x", Change: KindHealthFix},
	{Name: "watches", Web: []string{"watches", "watch"}, API: []string{"listWatches", "addWatch", "getWatch", "updateWatch", "removeWatch", "relinkWatch", "listStaleWatches"}, Script: "api", CLI: []string{"watch ls", "watch add", "watch rm", "watch set", "watch pause", "watch resume", "watch move", "watch stale"}, TUI: "W (home)", TUIKey: "W"},
	{Name: "hold marks", Web: []string{"watch", "library-manga"}, API: []string{"holdWatch", "holdLibraryManga"}, Script: "api", CLI: []string{"watch hold", "library hold"}, TUI: "alt+p (watches, library)", TUIKey: "alt+p"},
	{Name: "watch check", Web: []string{"watches", "watch", "run"}, API: []string{"checkWatches", "cancelRun"}, Script: "api", CLI: []string{"watch check"}, TUI: "u (watches)", TUIKey: "u"},
	{Name: "watch history", Web: []string{"runs", "run", "watch"}, API: []string{"listRuns", "getRun", "removeWatchRun"}, Script: "api", CLI: []string{"watch log"}, TUI: "V (watches)", TUIKey: "V"},
	{Name: "accounts", Web: []string{"accounts"}, API: []string{"listAccounts", "listMyAccounts", "loginMyAccount", "logoutMyAccount"}, Script: "api", CLI: []string{"accounts", "anilist", "anilist login", "kitsu", "myanimelist", "myanimelist login", "myanimelist client", "notify webhook"}, TUI: "K (home)", TUIKey: "K"},
	{Name: "tracker API clients", Web: []string{"accounts"}, API: []string{"listAPIClients", "setAPIClient", "removeAPIClient"}, Script: "api", CLI: []string{"anilist client", "myanimelist client"},
		TUI: "e (accounts on a server, the AniList and MyAnimeList login screens)", TUIKey: "e"},
	{Name: "config", Web: []string{"config"}, API: []string{"listConfig", "setConfig"}, Script: "api", CLI: []string{"config get", "config set", "config info", "config write", "config clean", "config export", "config import"}, TUI: ", (anywhere)"},
	{Name: "config history", Web: []string{"config"}, API: []string{"listConfigHistory", "undoConfigChange"}, Script: "api", CLI: []string{"config history", "config undo"}, TUI: "alt+h (config)", TUIKey: "alt+h"},
	{Name: "logs", Web: []string{"logs"}, API: []string{"listLogs", "listLogFiles"}, Script: "api", CLI: []string{"logs"}, TUI: "ctrl+l (anywhere)"},
	{Name: "cache", CLI: []string{"cache clear"},
		NotApplicable: map[Mode]string{ModeWeb: cliEnough, ModeAPI: cliEnough, ModeTUI: cliEnough, ModeScript: cliEnough}},
	{Name: "templates", CLI: []string{"templates exec", "templates funcs"},
		NotApplicable: map[Mode]string{ModeWeb: cliEnough, ModeAPI: cliEnough, ModeTUI: cliEnough, ModeScript: cliEnough}},
	{Name: "paths", CLI: []string{"path"}, NotApplicable: map[Mode]string{ModeWeb: cliEnough, ModeAPI: cliEnough, ModeTUI: cliEnough, ModeScript: cliEnough}},
	{Name: "formats", API: []string{"listFormats"}, Script: "api", CLI: []string{"formats"}, NotApplicable: map[Mode]string{ModeWeb: "downloads use download.format (the config)", ModeTUI: "the chapters' f popup lists them"}},
	{Name: "logins", Web: []string{"login", "users"}, API: []string{"login", "loginDevice", "logout", "getMe", "listUsers", "addUser", "updateUser", "removeUser", "endUserLogins", "listTokens", "addToken", "updateToken", "removeToken", "listPermissions"}, Script: "api",
		TUI: "K (home): users and API tokens after the accounts", TUIKey: "K",
		CLI: []string{"user add", "user passwd", "user perms", "user logout", "user rm", "user ls", "token add", "token ls", "token rm"}},
	{Name: "remote server", CLI: []string{"connection ls", "connection add", "connection rm", "connection use", "connection login", "connection logout", "connection status"},
		NotApplicable: map[Mode]string{ModeTUI: "the TUI runs on a server with --remote, logging in is the CLI's", ModeWeb: "the web UI is the server's", ModeAPI: "the client side of the API", ModeScript: cliEnough}},
}

// ModeCommands are the commands of no feature: the modes themselves and cobra's own.
var ModeCommands = map[string]string{
	"inline":      "the inline mode",
	"script":      "the script mode",
	"script doc":  "the script mode's docs",
	"serve":       "the serve mode",
	"serve token": "mangal token before 5.25 (deprecated)",
	"user":        "the users (its subcommands are the logins feature)",
	"token":       "the API tokens (its subcommands are the logins feature)",
	"api":         "the serve API's client",
	"remote":      "a remote server's token (its subcommands are the remote server feature)",
	"web":         "the web mode",
	"version":     "mangal's version",
	"help":        "cobra's help",
	"completion":  "cobra's shell completion",
}

// APIOperations are the API v1 operations of no feature: the API's own.
var APIOperations = map[string]string{
	"getIndex":  "the API's index",
	"getSpec":   "the API's spec",
	"getStatus": "the server's status",
	"getEvents": "the events of every feature",
}

// WebRoutes are the web UI's own routes, of no feature.
var WebRoutes = map[string]string{
	"not-found": "the web UI's 404",
}

// APIFeatures are the names of the features the API has.
func APIFeatures() []string {
	var names []string
	for _, f := range Features {
		if f.Has(ModeAPI) {
			names = append(names, f.Name)
		}
	}
	return names
}

// FeatureOfOperation is the feature with the API operation.
func FeatureOfOperation(id string) (Feature, bool) {
	for _, f := range Features {
		if slices.Contains(f.API, id) {
			return f, true
		}
	}
	return Feature{}, false
}

// FeaturesOf are the features with the CLI command.
func FeaturesOf(command string) []Feature {
	var of []Feature
	for _, f := range Features {
		for _, c := range f.CLI {
			if c == command {
				of = append(of, f)
			}
		}
	}
	return of
}

// FeatureOf is the feature that applies the change kind, false if none.
func FeatureOf(k Kind) (Feature, bool) {
	for _, f := range Features {
		if f.Change == k {
			return f, true
		}
	}
	return Feature{}, false
}

// IsModeCommand is true for a command of ModeCommands, or under one (completion bash).
func IsModeCommand(command string) bool {
	for c := range ModeCommands {
		if command == c || strings.HasPrefix(command, c+" ") {
			return true
		}
	}
	return false
}
