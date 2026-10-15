package login

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ help.KeyMap = (*keyMap)(nil)

func newKeyMap() keyMap {
	return keyMap{
		login:  base.KeyLogIn,
		logout: base.KeyLogOut,
		new:    base.KeyNewLogin,
		forget: base.KeyForget,
		client: base.KeyAPIClient,
		back:   util.Bind("saved logins", "esc"),
		cancel: util.Bind("cancel", "esc"),
	}
}

// keyMap implements help.KeyMap, the forms have their own.
type keyMap struct {
	login,
	logout,
	new,
	forget,
	client,
	back,
	cancel key.Binding
}

// ShortHelp implements help.KeyMap.
func (k *keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.login, k.logout, k.new, k.forget, k.client, k.back, k.cancel}
}

// FullHelp implements help.KeyMap.
func (k *keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.login, k.logout}, {k.new, k.forget}, {k.client, k.back, k.cancel}}
}
