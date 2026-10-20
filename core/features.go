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
	// Script is its Lua function ("client.search_mangas").
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
	noLibraryScripts = "scripts don't change the library (docs/core.md: read-only Lua modules or the serve API, to decide)"
	cliEnough        = "a one-off command, the CLI is enough"
	apiNotYet        = "not in the API yet (docs/core.md Progress)"
)

// Features are mangal's features.
var Features = []Feature{
	{Name: "providers", Web: []string{"providers"}, API: []string{"listProviders", "getProviderInfo", "getProviderIcon"}, CLI: []string{"providers ls"}, TUI: "enter (home)", Script: "client.info"},
	{Name: "provider install", CLI: []string{"providers add", "providers rm", "providers new"},
		NotApplicable: map[Mode]string{ModeWeb: "providers run code: never installed over HTTP", ModeAPI: "providers run code: never installed over HTTP", ModeTUI: "git installs are a terminal's job", ModeScript: "a script doesn't install code"}},
	{Name: "provider update", Web: []string{"providers"}, API: []string{"updateProviders"}, CLI: []string{"providers up"},
		NotApplicable: map[Mode]string{ModeTUI: "git updates are a terminal's job", ModeScript: "a script doesn't update code"}},
	{Name: "mirrors", Web: []string{"providers"}, API: []string{"setProviderMirror"}, CLI: []string{"providers ls", "config set"}, TUI: "alt+m (providers)", TUIKey: "alt+m",
		NotApplicable: map[Mode]string{ModeScript: cliEnough}},
	{Name: "search and browse", Web: []string{"search", "provider-manga"}, API: []string{"searchProvider", "getProviderManga", "listProviderChapters", "proxyImage"}, CLI: []string{"inline json"}, TUI: "s (mangas)", TUIKey: "s", Script: "client.search_mangas"},
	{Name: "multi-provider search", Web: []string{"search", "provider-manga"}, API: []string{"searchProvider"}, CLI: []string{"search", "library fill"}, TUI: "space, enter (providers); alt+g (chapters)", TUIKey: "space",
		NotApplicable: map[Mode]string{ModeScript: "a script searches each provider itself (client.search_mangas)"}},
	{Name: "download", Web: []string{"provider-manga", "jobs"}, API: []string{"startDownload", "listJobs", "getJob", "cancelJob", "retryJob", "removeJob", "clearJobQueue"}, CLI: []string{"inline download"}, TUI: "d (chapters)", TUIKey: "d", Script: "client.download_chapter"},
	{Name: "read", Web: []string{"read-library", "read-online"}, API: []string{"listChapterPages", "getChapterPage", "getChapterFile", "listProviderPages", "getProviderPage", "recordRead", "getReaderPosition", "putReaderPosition"}, TUI: "r (chapters)", TUIKey: "r",
		NotApplicable: map[Mode]string{ModeCLI: "reading is interactive (the TUI opens the default app, the web UI has a reader)", ModeScript: "reading is interactive"}},
	{Name: "read history", Web: []string{"history"}, API: []string{"listHistory"}, CLI: []string{"history"}, TUI: "alt+h (home)", TUIKey: "alt+h",
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "history clear", Web: []string{"history"}, API: []string{"clearHistory"}, CLI: []string{"history clear"}, TUI: "d, backspace (read history)", TUIKey: "d", Change: KindHistoryClear,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "library list", Web: []string{"library", "library-manga"}, API: []string{"listLibraryMangas", "getLibraryManga", "getLibraryCover", "listLibraryChapters", "listDownloads", "getLibraryMangaZip"}, CLI: []string{"library ls", "library downloads"}, TUI: "L (home)", TUIKey: "L",
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "library scan", Web: []string{"library"}, API: []string{"scanLibrary"}, CLI: []string{"library scan"}, TUI: "u, S (library)", TUIKey: "u",
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "merge", Web: []string{"duplicates", "library-manga"}, API: []string{"mergeLibraryMangas", "listDuplicates"}, CLI: []string{"library merge"}, TUI: "J (library, health)", TUIKey: "J", Change: KindMerge,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "move", Web: []string{"library-manga"}, API: []string{"moveLibraryManga"}, CLI: []string{"library move"}, TUI: "X (library)", TUIKey: "X", Change: KindMove,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "retitle", Web: []string{"library-manga"}, API: []string{"retitleLibraryManga"}, CLI: []string{"library retitle"}, TUI: "alt+t (library manga)", TUIKey: "alt+t", Change: KindRetitle,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "source urls", Web: []string{"library-manga"}, API: []string{"getLibrarySourceURL", "lookupSourceURLs"}, CLI: []string{"library urls"}, TUI: "o (library manga)", TUIKey: "o",
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "get unavailable chapters elsewhere", Web: []string{"provider-manga"}, API: []string{"fillProviderManga"}, CLI: []string{"library fill"}, TUI: "alt+g (chapters)", TUIKey: "alt+g",
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "links", Web: []string{"library-manga"}, API: []string{"linkLibraryManga"}, CLI: []string{"library bind"}, TUI: "A (library, library manga)", TUIKey: "A", Change: KindLink,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "dismiss a link pick", CLI: []string{"library bind"}, TUI: "d (links, a pick)", TUIKey: "d", Change: KindDismissPick,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts, ModeAPI: apiNotYet, ModeWeb: apiNotYet}},
	{Name: "backup links", Web: []string{"library"}, API: []string{"fillBackupLinks"}, CLI: []string{"library bind"}, TUI: "B (library, links)", TUIKey: "B", Change: KindBackups,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "mark", Web: []string{"library", "library-manga"}, API: []string{"markLibraryMangas"}, CLI: []string{"library mark", "library scan"}, TUI: "x (health)", TUIKey: "x", Change: KindMark,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "rename", Web: []string{"library", "library-manga"}, API: []string{"renameLibrary"}, CLI: []string{"library rename"}, TUI: "alt+r (library)", TUIKey: "alt+r", Change: KindRename,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "forget", Web: []string{"library-manga"}, API: []string{"forgetLibraryMangas"}, CLI: []string{"library scan", "library rm"}, TUI: "d (library, a missing manga)", TUIKey: "d", Change: KindForget,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "delete", Web: []string{"library-manga"}, API: []string{"deleteLibraryManga"}, CLI: []string{"library rm"}, TUI: "d (library, library manga)", TUIKey: "d", Change: KindDelete,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "trash", Web: []string{"trash"}, API: []string{"getTrash"}, CLI: []string{"library trash"}, TUI: "t (library)", TUIKey: "t",
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "empty the trash", Web: []string{"trash"}, API: []string{"emptyTrash"}, CLI: []string{"library trash empty"}, TUI: "d (trash)", TUIKey: "d", Change: KindEmptyTrash,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "undo", Web: []string{"ops"}, API: []string{"listOps", "undoOp"}, CLI: []string{"library undo"}, TUI: "U (library)", TUIKey: "U", Change: KindUndo,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "migrate", Web: []string{"library"}, API: []string{"migrateLibrary"}, CLI: []string{"migrate"}, TUI: "X (home)", TUIKey: "X", Change: KindMigrate,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "bindings", Web: []string{"provider-manga", "library-manga"}, API: []string{"listBindings", "setBinding"}, CLI: []string{"bind", "bind ls"}, TUI: "A (mangas, volumes, chapters)", TUIKey: "A", Change: KindBind,
		NotApplicable: map[Mode]string{ModeScript: "scripts bind titles to AniList ids only (anilist.bind_title_with_id)"}},
	{Name: "unbind", Web: []string{"provider-manga"}, API: []string{"removeBinding"}, CLI: []string{"bind"}, TUI: "d (bindings)", TUIKey: "d", Change: KindUnbind,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "metadata search", Web: []string{"provider-manga", "library-manga"}, API: []string{"listMetadataSources", "searchMetadata", "getMetadataIcon"}, CLI: []string{"bind"}, TUI: "s (bindings)", TUIKey: "s", Script: "anilist.search_mangas"},
	{Name: "health", Web: []string{"health"}, API: []string{"getHealth", "getHealthJob", "dismissHealthFindings"}, CLI: []string{"health"}, TUI: "! (home)", TUIKey: "!",
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "health fix", Web: []string{"health"}, API: []string{"fixHealthFinding"}, CLI: []string{"health fix"}, TUI: "x (health)", TUIKey: "x", Change: KindHealthFix,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "watches", Web: []string{"watches", "watch"}, API: []string{"listWatches", "addWatch", "getWatch", "updateWatch", "removeWatch", "relinkWatch", "listStaleWatches"}, CLI: []string{"watch ls", "watch add", "watch rm", "watch set", "watch pause", "watch resume", "watch move", "watch stale"}, TUI: "W (home)", TUIKey: "W",
		NotApplicable: map[Mode]string{ModeScript: "watches are serve's (its API)"}},
	{Name: "hold marks", Web: []string{"watch", "library-manga"}, API: []string{"holdWatch", "holdLibraryManga"}, CLI: []string{"watch hold", "library hold"}, TUI: "alt+p (watches, library)", TUIKey: "alt+p",
		NotApplicable: map[Mode]string{ModeScript: "watches are serve's (its API)"}},
	{Name: "watch check", Web: []string{"watches", "watch", "run"}, API: []string{"checkWatches", "cancelRun"}, CLI: []string{"watch check"}, TUI: "u (watches)", TUIKey: "u",
		NotApplicable: map[Mode]string{ModeScript: "watches are serve's (its API)"}},
	{Name: "watch history", Web: []string{"runs", "run", "watch"}, API: []string{"listRuns", "getRun", "removeWatchRun"}, CLI: []string{"watch log"}, TUI: "V (watches)", TUIKey: "V",
		NotApplicable: map[Mode]string{ModeScript: "watches are serve's (its API)"}},
	{Name: "accounts", Web: []string{"accounts"}, API: []string{"listAccounts"}, CLI: []string{"accounts", "anilist", "anilist login", "kitsu", "myanimelist", "myanimelist login", "myanimelist client", "notify webhook"}, TUI: "K (home)", TUIKey: "K",
		NotApplicable: map[Mode]string{ModeScript: "logins are interactive"}},
	{Name: "config", Web: []string{"config"}, API: []string{"listConfig", "setConfig"}, CLI: []string{"config get", "config set", "config info", "config write", "config clean"}, TUI: ", (anywhere)",
		NotApplicable: map[Mode]string{ModeScript: cliEnough}},
	{Name: "logs", Web: []string{"logs"}, API: []string{"listLogs", "listLogFiles"}, CLI: []string{"logs"}, TUI: "ctrl+l (anywhere)",
		NotApplicable: map[Mode]string{ModeScript: cliEnough}},
	{Name: "cache", CLI: []string{"cache clear"},
		NotApplicable: map[Mode]string{ModeWeb: cliEnough, ModeAPI: cliEnough, ModeTUI: cliEnough, ModeScript: cliEnough}},
	{Name: "templates", CLI: []string{"templates exec", "templates funcs"},
		NotApplicable: map[Mode]string{ModeWeb: cliEnough, ModeAPI: cliEnough, ModeTUI: cliEnough, ModeScript: cliEnough}},
	{Name: "paths", CLI: []string{"path"}, NotApplicable: map[Mode]string{ModeWeb: cliEnough, ModeAPI: cliEnough, ModeTUI: cliEnough, ModeScript: cliEnough}},
	{Name: "formats", API: []string{"listFormats"}, CLI: []string{"formats"}, NotApplicable: map[Mode]string{ModeWeb: "downloads use download.format (the config)", ModeTUI: "the chapters' f popup lists them", ModeScript: cliEnough}},
	{Name: "logins", Web: []string{"login", "users"}, API: []string{"login", "logout", "getMe", "listUsers", "addUser", "updateUser", "removeUser", "endUserLogins", "listTokens", "addToken", "removeToken"},
		TUI: "K (home): users and API tokens after the accounts", TUIKey: "K",
		CLI:           []string{"user add", "user passwd", "user perms", "user logout", "user rm", "user ls", "token add", "token ls", "token rm"},
		NotApplicable: map[Mode]string{ModeScript: "scripts use the server's key or an API token"}},
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
