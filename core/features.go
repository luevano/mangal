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
)

// Modes are every mode.
var Modes = []Mode{ModeCLI, ModeTUI, ModeScript, ModeAPI}

// Feature is something mangal does and where each mode does it, so the modes stay in
// parity: tests check that every cobra command, change kind, TUI key and API operation
// here exists, and every API v1 operation is a feature's (or APIOperations').
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
	{Name: "providers", API: []string{"listProviders", "getProviderInfo"}, CLI: []string{"providers ls"}, TUI: "enter (home)", Script: "client.info"},
	{Name: "provider install", CLI: []string{"providers add", "providers up", "providers rm", "providers new"},
		NotApplicable: map[Mode]string{ModeAPI: "providers run code: never installed over HTTP", ModeTUI: "git installs are a terminal's job", ModeScript: "a script doesn't install code"}},
	{Name: "search and browse", API: []string{"searchProvider", "getProviderManga", "listProviderChapters", "proxyImage"}, CLI: []string{"inline json"}, TUI: "s (mangas)", TUIKey: "s", Script: "client.search_mangas"},
	{Name: "download", API: []string{"startDownload", "listJobs", "getJob", "cancelJob", "retryJob", "removeJob", "clearJobQueue"}, CLI: []string{"inline download"}, TUI: "d (chapters)", TUIKey: "d", Script: "client.download_chapter"},
	{Name: "read history", API: []string{"listHistory"}, CLI: []string{"history"}, TUI: "alt+h (home)", TUIKey: "alt+h",
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "history clear", API: []string{"clearHistory"}, CLI: []string{"history clear"}, TUI: "d, backspace (read history)", TUIKey: "d", Change: KindHistoryClear,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "library list", API: []string{"listLibraryMangas", "getLibraryManga", "listLibraryChapters", "listDownloads"}, CLI: []string{"library ls", "library downloads"}, TUI: "L (home)", TUIKey: "L",
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "library scan", API: []string{"scanLibrary"}, CLI: []string{"library scan"}, TUI: "u, S (library)", TUIKey: "u",
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "merge", API: []string{"mergeLibraryMangas", "listDuplicates"}, CLI: []string{"library merge"}, TUI: "J (library, health)", TUIKey: "J", Change: KindMerge,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "move", API: []string{"moveLibraryManga"}, CLI: []string{"library move"}, TUI: "X (library)", TUIKey: "X", Change: KindMove,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "links", API: []string{"linkLibraryManga"}, CLI: []string{"library bind"}, TUI: "A (library, library manga)", TUIKey: "A", Change: KindLink,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "backup links", API: []string{"fillBackupLinks"}, CLI: []string{"library bind"}, TUI: "B (library, links)", TUIKey: "B", Change: KindBackups,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "mark", API: []string{"markLibraryMangas"}, CLI: []string{"library mark", "library scan"}, TUI: "x (health)", TUIKey: "x", Change: KindMark,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "rename", API: []string{"renameLibrary"}, CLI: []string{"library rename"}, TUI: "alt+r (library)", TUIKey: "alt+r", Change: KindRename,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "forget", API: []string{"forgetLibraryMangas"}, CLI: []string{"library scan", "library rm"}, TUI: "d (library, a missing manga)", TUIKey: "d", Change: KindForget,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "delete", API: []string{"deleteLibraryManga"}, CLI: []string{"library rm"}, TUI: "d (library, library manga)", TUIKey: "d", Change: KindDelete,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "trash", API: []string{"getTrash"}, CLI: []string{"library trash"}, TUI: "t (library)", TUIKey: "t",
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "empty the trash", API: []string{"emptyTrash"}, CLI: []string{"library trash empty"}, TUI: "d (trash)", TUIKey: "d", Change: KindEmptyTrash,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "undo", API: []string{"listOps", "undoOp"}, CLI: []string{"library undo"}, TUI: "U (library)", TUIKey: "U", Change: KindUndo,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "migrate", API: []string{"migrateLibrary"}, CLI: []string{"migrate"}, TUI: "X (home)", TUIKey: "X", Change: KindMigrate,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "bindings", API: []string{"listBindings", "setBinding"}, CLI: []string{"bind", "bind ls"}, TUI: "A (mangas, volumes, chapters)", TUIKey: "A", Change: KindBind,
		NotApplicable: map[Mode]string{ModeScript: "scripts bind titles to AniList ids only (anilist.bind_title_with_id)"}},
	{Name: "unbind", API: []string{"removeBinding"}, CLI: []string{"bind"}, TUI: "d (bindings)", TUIKey: "d", Change: KindUnbind,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "metadata search", CLI: []string{"bind"}, TUI: "s (bindings)", TUIKey: "s", Script: "anilist.search_mangas", NotApplicable: map[Mode]string{ModeAPI: apiNotYet}},
	{Name: "health", API: []string{"getHealth", "getHealthJob", "dismissHealthFindings"}, CLI: []string{"health"}, TUI: "! (home)", TUIKey: "!",
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "health fix", API: []string{"fixHealthFinding"}, CLI: []string{"health fix"}, TUI: "x (health)", TUIKey: "x", Change: KindHealthFix,
		NotApplicable: map[Mode]string{ModeScript: noLibraryScripts}},
	{Name: "watches", API: []string{"listWatches", "addWatch", "getWatch", "updateWatch", "removeWatch", "relinkWatch", "listStaleWatches"}, CLI: []string{"watch ls", "watch add", "watch rm", "watch set", "watch pause", "watch resume", "watch move", "watch stale"}, TUI: "W (home)", TUIKey: "W",
		NotApplicable: map[Mode]string{ModeScript: "watches are serve's (its API)"}},
	{Name: "watch check", API: []string{"checkWatches", "cancelRun"}, CLI: []string{"watch check"}, TUI: "u (watches)", TUIKey: "u",
		NotApplicable: map[Mode]string{ModeScript: "watches are serve's (its API)"}},
	{Name: "watch history", API: []string{"listRuns", "getRun", "removeWatchRun"}, CLI: []string{"watch log"}, TUI: "V (watches)", TUIKey: "V",
		NotApplicable: map[Mode]string{ModeScript: "watches are serve's (its API)"}},
	{Name: "accounts", API: []string{"listAccounts"}, CLI: []string{"accounts", "anilist", "anilist login", "kitsu", "myanimelist", "myanimelist login", "myanimelist client", "notify webhook"}, TUI: "K (home)", TUIKey: "K",
		NotApplicable: map[Mode]string{ModeScript: "logins are interactive"}},
	{Name: "config", API: []string{"listConfig", "setConfig"}, CLI: []string{"config get", "config set", "config info", "config write"}, TUI: ", (anywhere)",
		NotApplicable: map[Mode]string{ModeScript: cliEnough}},
	{Name: "logs", API: []string{"listLogs", "listLogFiles"}, CLI: []string{"logs"}, TUI: "ctrl+l (anywhere)",
		NotApplicable: map[Mode]string{ModeScript: cliEnough}},
	{Name: "cache", CLI: []string{"cache clear"},
		NotApplicable: map[Mode]string{ModeAPI: cliEnough, ModeTUI: cliEnough, ModeScript: cliEnough}},
	{Name: "templates", CLI: []string{"templates exec", "templates funcs"},
		NotApplicable: map[Mode]string{ModeAPI: cliEnough, ModeTUI: cliEnough, ModeScript: cliEnough}},
	{Name: "paths", CLI: []string{"path"}, NotApplicable: map[Mode]string{ModeAPI: cliEnough, ModeTUI: cliEnough, ModeScript: cliEnough}},
	{Name: "formats", API: []string{"listFormats"}, CLI: []string{"formats"}, NotApplicable: map[Mode]string{ModeTUI: "the chapters' f popup lists them", ModeScript: cliEnough}},
}

// ModeCommands are the commands of no feature: the modes themselves and cobra's own.
var ModeCommands = map[string]string{
	"inline":      "the inline mode",
	"script":      "the script mode",
	"script doc":  "the script mode's docs",
	"serve":       "the serve mode",
	"serve token": "the serve mode's token",
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
