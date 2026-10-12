package base

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/util"
)

// The keys of the actions on more than one screen: a screen with the action binds it to
// the same key (copy the binding into its key map).
var (
	// keys from before v5.14, they never move

	KeyInfo     = util.Bind("info", "i")
	KeyMetadata = util.Bind("metadata", "m")
	KeyBindings = util.Bind("bindings", "A")
	KeySearch   = util.Bind("search", "s")
	KeyLogIn    = util.Bind("log in", "i")
	KeyLogOut   = util.Bind("log out", "o")
	KeyNewLogin = util.Bind("new login", "n")
	KeyForget   = util.Bind("forget", "ctrl+d")

	// newer keys: none of them is a global or list key, so each works on every screen

	KeyWatch           = util.Bind("watch", "w")
	KeyItsWatch        = util.Bind("its watch", "W")
	KeyLibraryManga    = util.Bind("library manga", "L")
	KeyProviderHistory = util.Bind("provider history", "v")
	KeyLiveLog         = util.Bind("live log", "t")
	KeyRelink          = util.Bind("re-link manga", "m")
	KeyPause           = util.Bind("pause/resume", "p")
	KeyEdit            = util.Bind("edit", "e")
	KeyRefresh         = util.Bind("refresh", "r")
	KeyCheckProvider   = util.Bind("check provider", "alt+c")
	KeyNextProvider    = util.Bind("next provider", "tab")
	KeyScan            = util.Bind("scan", "s")
	KeyDelete          = util.Bind("delete", "D")
	KeyRemove          = util.Bind("remove", "x")
	KeyUndo            = util.Bind("undo", "U")
	KeyBackups         = util.Bind("fill backups", "B")
)

// sharedKeys are the Key* bindings, the old ones (from before v5.14) first.
func sharedKeys() (old, newer []key.Binding) {
	return []key.Binding{KeyInfo, KeyMetadata, KeyBindings, KeySearch, KeyLogIn, KeyLogOut, KeyNewLogin, KeyForget},
		[]key.Binding{
			KeyWatch, KeyItsWatch, KeyLibraryManga, KeyProviderHistory, KeyLiveLog, KeyRelink, KeyPause, KeyEdit,
			KeyRefresh, KeyCheckProvider, KeyNextProvider, KeyScan, KeyDelete, KeyRemove, KeyUndo, KeyBackups,
		}
}

// KeyInconsistencies are the screen's bindings of a shared action (same help) without its
// shared key, as "library manga: l, not L". For tests.
func KeyInconsistencies(screen help.KeyMap) []string {
	old, newer := sharedKeys()
	var bad []string
	for _, b := range util.Bindings(screen) {
		for _, shared := range append(old, newer...) {
			if b.Help().Desc != shared.Help().Desc {
				continue
			}
			for _, k := range shared.Keys() {
				if !slices.Contains(b.Keys(), k) {
					bad = append(bad, b.Help().Desc+": "+strings.Join(b.Keys(), "/")+", not "+k)
				}
			}
		}
	}
	return bad
}
