package base

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/util"
)

var (
	_ help.KeyMap = (*keyMap)(nil)
	_ help.KeyMap = (*combinedKeyMap)(nil)
)

func newKeyMap() *keyMap {
	return &keyMap{
		quit:      util.Bind("quit", "ctrl+c"),
		quitQ:     util.Bind("quit", "q"),
		back:      util.Bind("back", "esc"),
		home:      util.Bind("home", "H"),
		help:      util.Bind("help", "?"),
		log:       util.Bind("logs", "ctrl+l"),
		config:    util.Bind("config", ","),
		configFix: util.Bind("change it", "e"),
		downloads: KeyDownloads,
		windows:   KeyWindows,
	}
}

// ConfigKeys are the keys of the config window and of changing an error's config key, for
// the messages that tell them.
func ConfigKeys() (open, change string) {
	k := newKeyMap()
	return k.config.Help().Key, k.configFix.Help().Key
}

// keyMap implements help.KeyMap.
type keyMap struct {
	quit,
	quitQ,
	back,
	home,
	help,
	log,
	config,
	configFix,
	downloads,
	windows key.Binding
}

// ShortHelp implements help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.back,
		k.configFix,
		k.quitQ,
		k.help,
	}
}

// FullHelp implements help.KeyMap.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		k.ShortHelp(),
		{
			k.home,
			k.downloads,
			k.windows,
			k.config,
			k.quit,
			k.log,
		},
	}
}

// CombinedKeyMap is a convenience function to create a combined KeyMap that
// implements help.KeyMap, useful for concatenating them.
//
// The resulting keybind is not ment to be modified in-place.
func CombinedKeyMap(first, second help.KeyMap) combinedKeyMap {
	return combinedKeyMap{
		first:  first,
		second: second,
	}
}

// combinedKeyMap implements help.KeyMap.
type combinedKeyMap struct {
	first  help.KeyMap
	second help.KeyMap
}

// ShortHelp implements help.KeyMap.
func (c combinedKeyMap) ShortHelp() []key.Binding {
	return append(c.first.ShortHelp(), c.second.ShortHelp()...)
}

// FullHelp implements help.KeyMap.
func (c combinedKeyMap) FullHelp() [][]key.Binding {
	return append(c.first.FullHelp(), c.second.FullHelp()...)
}

// GlobalKeys are the keys every screen has while not typing (quit, back, home, help, log,
// config, downloads, windows): a screen's own keys mustn't use them.
func GlobalKeys() []key.Binding {
	k := newKeyMap()
	return []key.Binding{k.quit, k.back, k.home, k.help, k.log, k.config, k.downloads, k.windows}
}

// KeyCollisions are the keys that would mean two things on a screen, between its own keys
// (screen), the list's (extra, e.g. list.KeyMap.Browsing) and the global ones, and the
// screen's KeyInconsistencies. For tests.
func KeyCollisions(screen help.KeyMap, extra ...key.Binding) []string {
	collisions := util.Collisions(append(append(GlobalKeys(), util.Bindings(screen)...), extra...)...)
	return append(collisions, KeyInconsistencies(screen)...)
}

// ViewerKeyCollisions are KeyCollisions without the KeyInconsistencies, for a viewer (the
// live log): its pager keys (b, f, space...) are its own.
func ViewerKeyCollisions(screen help.KeyMap) []string {
	return util.Collisions(append(GlobalKeys(), util.Bindings(screen)...)...)
}
