package anilist

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ help.KeyMap = (*keyMap)(nil)

func newKeyMap() keyMap {
	return keyMap{
		login:   util.Bind("login", "i"),
		logout:  util.Bind("logout", "o"),
		open:    util.Bind("open auth url", "ctrl+o"),
		new:     util.Bind("new", "n"),
		delete:  util.Bind("forget", "ctrl+d"),
		up:      util.BindNamedKey("↑/k", "up", "k", "up"),
		down:    util.BindNamedKey("↓/j", "down", "j", "down"),
		selekt:  util.Bind("edit field", "enter"),
		confirm: util.Bind("confirm", "enter"),
		cancel:  util.Bind("cancel", "esc"),
		clear:   util.Bind("clear field", "c"),
		back:    util.Bind("select login", "esc"),
		help:    util.Bind("help", "?"),
		quit:    util.Bind("quit", "q", "ctrl+c"),
	}
}

// keyMap implements help.KeyMap.
type keyMap struct {
	login,
	logout,
	open,
	new,
	delete,
	up,
	down,
	selekt, // select input
	confirm, // confirm input content
	cancel, // cancel input content
	clear, // clear input content
	back, // go from new login to select existing logins (if any)
	help,
	quit key.Binding
}

// ShortHelp implements help.KeyMap.
func (k *keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.up,
		k.down,
		k.login,
		k.logout,
		k.open,
		k.new,
		k.delete,
		k.selekt,
		k.clear,
		k.help,
	}
}

// FullHelp implements help.KeyMap.
func (k *keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{
			k.up,
			k.down,
		},
		{
			k.login,
			k.logout,
			k.open,
		},
		{
			k.new,
			k.delete,
		},
		{
			k.selekt,
			k.clear,
		},
		{
			k.back,
			k.quit,
			k.help,
		},
	}
}
