package base

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"github.com/luevano/mangal/v5/tui/util"
)

// Every key of the screens (not the globals, the list's, the viewers' or a dialog's): a
// screen copies the binding instead of writing its own, so an action has one key and a key
// one meaning on every screen. keyConcepts has the keys with more than one help.
var (
	// keys from before v5.14 ("old"), they never move

	KeyInfo          = util.Bind("info", "i")
	KeyMetadata      = util.Bind("metadata", "m")
	KeyFullMetadata  = util.Bind("toggle full meta", "M")
	KeyBindings      = util.Bind("bindings", "A")
	KeySearch        = util.Bind("search", "s")
	KeyLogIn         = util.Bind("log in", "i")
	KeyLogOut        = util.Bind("log out", "o")
	KeyNewLogin      = util.Bind("new login", "n")
	KeyForget        = util.Bind("forget", "ctrl+d")
	KeyCloseAll      = util.Bind("close all", "backspace")
	KeyToggle        = util.Bind("toggle", "space")
	KeyRead          = util.Bind("read", "r")
	KeyDownload      = util.Bind("download", "d")
	KeyFormat        = util.Bind("change format", "f")
	KeyOpenURL       = util.Bind("open url", "o")
	KeySelectAll     = util.Bind("select all", "a")
	KeyUnselectAll   = util.Bind("unselect all", "backspace")
	KeyVolumeNumber  = util.Bind("toggle vol num", "v")
	KeyChapterNumber = util.Bind("toggle number", "c")
	KeyGroup         = util.Bind("toggle group", "ctrl+g")
	KeyDate          = util.Bind("toggle date", "ctrl+d")
	KeyOpenDir       = util.Bind("open directory", "o")
	KeyRetry         = util.Bind("retry", "r")
	KeyCopy          = util.Bind("copy", "c")
	KeyUp            = util.BindNamedKey("↑/k", "up", "up", "k")
	KeyDown          = util.BindNamedKey("↓/j", "down", "down", "j")

	// newer keys: none of them is a global or list key, so each works on every screen

	KeyLibrary         = util.Bind("library", "L")
	KeyWatches         = util.Bind("watches", "W")
	KeyAccounts        = util.Bind("accounts", "K")
	KeyMigrate         = util.Bind("migrate", "X")
	KeyHealth          = util.Bind("health", "!")
	KeyWatch           = util.Bind("watch", "w")
	KeyItsWatch        = util.Bind("its watch", "W")
	KeyLibraryManga    = util.Bind("library manga", "L")
	KeyMirrors         = util.Bind("mirrors", "alt+m")
	KeyCopyURL         = util.Bind("copy url", "c")
	KeyAccountLogIn    = util.Bind("log in / set secret", "i")
	KeyAPIClient       = util.Bind("API client", "e")
	KeyRefresh         = util.Bind("refresh", "ctrl+r")
	KeyEdit            = util.Bind("edit", "e")
	KeyAdd             = util.Bind("add", "n")
	KeyDelete          = util.Bind("delete", "d")
	KeyRemove          = util.Bind("remove", "d")
	KeyCancel          = util.Bind("cancel", "d")
	KeyCancelAll       = util.Bind("cancel all", "alt+d")
	KeyClearQueue      = util.Bind("clear its queue", "backspace")
	KeyClearQueues     = util.Bind("clear every queue", "alt+backspace")
	KeyOrigin          = util.Bind("its screen", "o")
	KeyReset           = util.Bind("reset to default", "backspace")
	KeyExpandAll       = util.Bind("expand/collapse all", "E")
	KeyChange          = util.BindNamedKey("enter/space", "change", "enter", "space")
	KeyOnOff           = util.Bind("on/off", "space")
	KeyScan            = util.Bind("scan", "u")
	KeyScanIDs         = util.Bind("scan and check ids", "S")
	KeyMerge           = util.Bind("merge", "J")
	KeyMove            = util.Bind("move to a provider", "X")
	KeyTrash           = util.Bind("trash", "t")
	KeyEmptyTrash      = util.Bind("empty", "d")
	KeyUndo            = util.Bind("undo", "U")
	KeyDuplicates      = util.Bind("duplicates only", "D")
	KeyBackups         = util.Bind("fill backups", "B")
	KeyCheck           = util.Bind("check now", "u")
	KeyCheckDue        = util.Bind("check due", "C")
	KeyCheckProvider   = util.Bind("check provider", "alt+c")
	KeyFillGaps        = util.Bind("fill gaps", "F")
	KeyPause           = util.Bind("pause/resume", "p")
	KeyHold            = util.Bind("hold / new provider", "alt+p")
	KeyPauseProvider   = util.Bind("pause/resume its provider", "P")
	KeyStopWatching    = util.Bind("stop watching", "d")
	KeyDeleteRun       = util.Bind("delete check", "d")
	KeyRelink          = util.Bind("re-link manga", "b")
	KeyLiveLog         = util.Bind("live log", "O")
	KeyLiveChecks      = util.Bind("every live check", "T")
	KeyRuns            = util.Bind("every check run", "V")
	KeyProviderHistory = util.Bind("provider history", "alt+h")
	KeyReadHistory     = util.Bind("read history", "alt+h")
	KeyClearAll        = util.Bind("clear all", "backspace")
	KeyNextProvider    = util.Bind("next provider", "tab")
	KeyPickProvider    = util.Bind("provider", "tab")
	KeyNext            = util.Bind("next", "tab")
	KeyGroupByProvider = util.Bind("group by provider", "alt+o")
	KeyNeedsRelink     = util.Bind("needs re-link only", "N")
	KeyNotInLibrary    = util.Bind("not in library only", "I")
	// the log viewer's: n and N are taken (new login, needs re-link)
	KeyNextMatch = util.Bind("next match", "ctrl+n")
	KeyPrevMatch = util.Bind("previous match", "ctrl+p")
	KeyLevel     = util.Bind("level", "tab")
	// only the matching lines, as less's &; free everywhere
	KeyNarrow = util.Bind("narrow", "&")
	// Logs (the current log): the files are its past ones, like V's check runs; [ and ]
	// step through the periods (h/l page)
	KeyLogFiles   = util.Bind("every log file", "V")
	KeyPrevPeriod = util.Bind("previous period", "[")
	KeyNextPeriod = util.Bind("next period", "]")

	// the health screen's: a and A are select all and bindings (old)
	KeyApplyFix = util.Bind("apply fix", "x")
	KeyNetwork  = util.Bind("network checks", "alt+n")
	// dismiss (a finding that's fine), z and Z were free everywhere
	KeyDismiss       = util.Bind("dismiss", "z")
	KeyShowDismissed = util.Bind("show dismissed", "Z")

	// library rename (library, health's rename fixes) and its options (mangal library
	// rename --chapters, --fallback); r and R are read and reverse
	KeyRename       = util.Bind("rename", "alt+r")
	KeyChapterFiles = util.Bind("chapter files", "alt+f")
	KeyFallback     = util.Bind("fallback names", "alt+b")

	// library retitle (library manga): t is the trash
	KeyRetitle = util.Bind("retitle chapters", "alt+t")

	// a manga by its id instead of a title (mangas, re-link/move search: a toggle;
	// providers: open the search by id); alt: it works while typing, i is info (old)
	KeyByID = util.Bind("search by id", "alt+i")
)

// The global keys of the screens base opens from anywhere (GlobalKeys, not Key* keys: no
// screen may use them). Q and ctrl+w were free on every screen.
var (
	KeyDownloads = util.Bind("downloads", "Q")
	KeyWindows   = util.Bind("windows", "ctrl+w")
)

// keyConcepts are the keys with more than one help, each one concept: the helps they may
// have. The rest have one.
var keyConcepts = map[string][]string{
	// old: info, and log in on the login screens (no info there)
	"i": {"info", "log in", "log in / set secret"},
	// old: chapter number on the chapters, copy
	"c": {"toggle number", "copy", "copy url"},
	// old: open, and log out on the logins
	"o": {"open url", "open directory", "log out", "its screen"},
	// old: read on the chapters, retry on the downloads
	"r": {"read", "retry"},
	// old: forget on the logins, the date on the chapters
	"ctrl+d": {"forget", "toggle date"},
	// delete; download is the chapters' old key (no delete there)
	"d": {"delete", "remove", "stop watching", "empty", "delete check", "download", "cancel"},
	// clear
	"backspace": {"close all", "unselect all", "reset to default", "clear its queue", "clear all"},
	// the past: a provider's checks, the chapters read
	"alt+h": {"provider history", "read history"},
	"space": {"toggle", "change", "on/off"},
	"tab":   {"next provider", "provider", "next", "level"},
	"n":     {"new login", "add"},
	"e":     {"edit", "API client"},
	// look for new chapters: on the provider (a watch), in the folders (the library)
	"u": {"check now", "scan"},
	"L": {"library manga", "library"},
	"W": {"its watch", "watches"},
	"X": {"move to a provider", "migrate"},
	// the past ones, a list: a watch's check runs, the log files
	"V": {"every check run", "every log file"},
}

// Keys are every Key* binding, the old ones (from before v5.14) first.
func Keys() (old, newer []key.Binding) {
	return []key.Binding{
		KeyInfo, KeyMetadata, KeyFullMetadata, KeyBindings, KeySearch, KeyLogIn, KeyLogOut, KeyNewLogin,
		KeyForget, KeyCloseAll, KeyToggle, KeyRead, KeyDownload, KeyFormat, KeyOpenURL,
		KeySelectAll, KeyUnselectAll, KeyVolumeNumber, KeyChapterNumber, KeyGroup, KeyDate, KeyOpenDir,
		KeyRetry, KeyCopy, KeyUp, KeyDown,
	}, []key.Binding{
		KeyLibrary, KeyWatches, KeyAccounts, KeyMigrate, KeyHealth, KeyWatch, KeyItsWatch, KeyLibraryManga,
		KeyMirrors, KeyCopyURL, KeyAccountLogIn, KeyAPIClient, KeyRefresh, KeyEdit, KeyAdd, KeyDelete,
		KeyRemove, KeyCancel, KeyCancelAll, KeyClearQueue, KeyClearQueues, KeyOrigin, KeyReset, KeyExpandAll, KeyChange, KeyOnOff, KeyScan, KeyScanIDs, KeyMerge, KeyMove, KeyTrash, KeyEmptyTrash,
		KeyUndo, KeyDuplicates, KeyBackups, KeyCheck, KeyCheckDue, KeyCheckProvider, KeyFillGaps, KeyPause, KeyHold,
		KeyPauseProvider, KeyStopWatching, KeyDeleteRun, KeyRelink, KeyLiveLog, KeyLiveChecks, KeyRuns,
		KeyProviderHistory, KeyNextProvider, KeyPickProvider, KeyNext, KeyGroupByProvider, KeyNeedsRelink,
		KeyNotInLibrary, KeyNextMatch, KeyPrevMatch, KeyLevel, KeyNarrow,
		KeyLogFiles, KeyPrevPeriod, KeyNextPeriod, KeyApplyFix, KeyNetwork, KeyDismiss, KeyShowDismissed,
		KeyRename, KeyChapterFiles, KeyFallback, KeyReadHistory, KeyClearAll, KeyByID, KeyRetitle,
	}
}

// plainKeys go with a screen key without making it a second key (enter is the screen's
// main action, the rest move): they aren't compared.
var plainKeys = []string{"enter", "esc", "up", "down", "left", "right", "home", "end", "pgup", "pgdown", "shift+tab"}

// screenKeys are the binding's keys but the plain ones.
func screenKeys(b key.Binding) []string {
	return slices.DeleteFunc(slices.Clone(b.Keys()), func(k string) bool { return slices.Contains(plainKeys, k) })
}

// KeyInconsistencies are the screen's bindings that aren't a Key* binding: another key for
// a Key* action ("library manga: l, not L"), another meaning for a Key* key ("m: merge, is
// metadata"), or a key no Key* binding has. For tests.
func KeyInconsistencies(screen help.KeyMap) []string {
	old, newer := Keys()
	all := append(old, newer...)
	var bad []string
	for _, b := range util.Bindings(screen) {
		keys := screenKeys(b)
		if len(keys) == 0 {
			continue
		}
		desc := b.Help().Desc
		if slices.ContainsFunc(all, func(k key.Binding) bool {
			return k.Help().Desc == desc && slices.Equal(screenKeys(k), keys)
		}) {
			continue
		}
		var problem string
		if i := slices.IndexFunc(all, func(k key.Binding) bool { return k.Help().Desc == desc }); i >= 0 {
			problem = desc + ": " + strings.Join(keys, "/") + ", not " + strings.Join(screenKeys(all[i]), "/")
		}
		for _, k := range keys {
			if problem != "" {
				break
			}
			if i := slices.IndexFunc(all, func(b key.Binding) bool { return slices.Contains(screenKeys(b), k) }); i >= 0 {
				problem = k + ": " + desc + ", is " + all[i].Help().Desc
			}
		}
		if problem == "" {
			problem = desc + ": " + strings.Join(keys, "/") + " isn't a base key"
		}
		bad = append(bad, problem)
	}
	return bad
}
