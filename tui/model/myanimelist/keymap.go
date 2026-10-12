package myanimelist

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
		login:  base.KeyLogIn,
		logout: base.KeyLogOut,
		new:    base.KeyNewLogin,
		forget: base.KeyForget,
		back:   util.Bind("saved logins", "esc"),
		client: util.Bind("API client", "c"),
		save:   util.Bind("save", "enter"),
		next:   util.Bind("next", "tab", "down"),
		prev:   util.Bind("previous", "shift+tab", "up"),
		cancel: util.Bind("cancel", "esc"),
	}
}

// keyMap implements help.KeyMap.
type keyMap struct {
	login,
	logout,
	new,
	forget,
	back,
	client,
	save,
	next,
	prev,
	cancel key.Binding
}

// ShortHelp implements help.KeyMap.
func (k *keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.save, k.next, k.cancel, k.login, k.logout, k.new, k.forget, k.client, k.back}
}

// FullHelp implements help.KeyMap.
func (k *keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.save, k.next, k.prev, k.cancel}, {k.login, k.logout}, {k.new, k.forget}, {k.client, k.back}}
}
