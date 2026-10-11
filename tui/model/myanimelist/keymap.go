package myanimelist

import (
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/util"
)

// noticeDuration is how long the login notices stay, some are long.
const noticeDuration = 5 * time.Second

var _ help.KeyMap = (*keyMap)(nil)

func newKeyMap() keyMap {
	return keyMap{
		login:  util.Bind("log in", "i"),
		logout: util.Bind("log out", "o"),
		new:    util.Bind("new login", "n"),
		forget: util.Bind("forget", "ctrl+d"),
		back:   util.Bind("saved logins", "esc"),
	}
}

// keyMap implements help.KeyMap.
type keyMap struct {
	login,
	logout,
	new,
	forget,
	back key.Binding
}

// ShortHelp implements help.KeyMap.
func (k *keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.login, k.logout, k.new, k.forget, k.back}
}

// FullHelp implements help.KeyMap.
func (k *keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.login, k.logout}, {k.new, k.forget}, {k.back}}
}
