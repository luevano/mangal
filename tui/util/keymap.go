package util

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

// Bind is a convenience function to create key bindings easily.
func Bind(help string, primaryKey string, extraKeys ...string) key.Binding {
	keys := append([]string{primaryKey}, extraKeys...)
	return BindNamedKey(primaryKey, help, keys...)
}

// BindNamedKey creates a keybind with specified help and list of keys.
func BindNamedKey(keyHelp, help string, keys ...string) key.Binding {
	if keyHelp == " " {
		keyHelp = "space"
	}
	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(keyHelp, help),
	)
}

var _ help.KeyMap = KeyMap{}

// KeyMap is a help.KeyMap from its bindings. Build it in the State's KeyMap method from
// the State's key fields, so it shows their current enabled state:
//
//	func (s *state) KeyMap() help.KeyMap {
//		return util.KeyMap{Short: []key.Binding{s.keys.open, s.keys.delete}}
//	}
type KeyMap struct {
	Short []key.Binding
	// Full is the full help, Short as one column when nil.
	Full [][]key.Binding
}

// ShortHelp implements help.KeyMap.
func (k KeyMap) ShortHelp() []key.Binding {
	return k.Short
}

// FullHelp implements help.KeyMap.
func (k KeyMap) FullHelp() [][]key.Binding {
	if k.Full == nil && k.Short != nil {
		return [][]key.Binding{k.Short}
	}
	return k.Full
}
