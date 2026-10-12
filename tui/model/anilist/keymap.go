package anilist

import (
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

// noticeDuration is how long the login notices stay, some are long.
const noticeDuration = 5 * time.Second

var _ help.KeyMap = (*keyMap)(nil)

func newKeyMap() keyMap {
	return keyMap{
		login:   base.KeyLogIn,
		logout:  base.KeyLogOut,
		new:     base.KeyNewLogin,
		forget:  base.KeyForget,
		up:      util.BindNamedKey("↑/k", "up", "k", "up"),
		down:    util.BindNamedKey("↓/j", "down", "j", "down"),
		edit:    util.Bind("edit field", "enter"),
		confirm: util.Bind("confirm", "enter"),
		cancel:  util.Bind("cancel", "esc"),
		clear:   util.Bind("clear field", "c"),
		back:    util.Bind("saved logins", "esc"),
	}
}

// keyMap implements help.KeyMap.
type keyMap struct {
	login,
	logout,
	new,
	forget,
	up,
	down,
	edit,
	confirm,
	cancel,
	clear,
	back key.Binding
}

// ShortHelp implements help.KeyMap.
func (k *keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.login, k.logout, k.new, k.forget, k.edit, k.confirm, k.cancel, k.clear, k.back}
}

// FullHelp implements help.KeyMap.
func (k *keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.up, k.down},
		{k.login, k.logout},
		{k.new, k.forget},
		{k.edit, k.clear, k.confirm, k.cancel},
		{k.back},
	}
}
