package util

import (
	"slices"
	"strings"

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

// Bindings are the bindings of a key map's full help (the short help when it has none),
// each once.
func Bindings(km help.KeyMap) []key.Binding {
	var all []key.Binding
	seen := map[string]bool{}
	add := func(b key.Binding) {
		id := strings.Join(b.Keys(), "\x00") + "\x01" + b.Help().Desc
		if !seen[id] {
			seen[id] = true
			all = append(all, b)
		}
	}
	for _, col := range km.FullHelp() {
		for _, b := range col {
			add(b)
		}
	}
	if len(all) == 0 {
		for _, b := range km.ShortHelp() {
			add(b)
		}
	}
	return all
}

// Collisions are the keys of more than one of the bindings, as "d: delete, next page",
// sorted: keys that would mean two things on the same screen.
func Collisions(bindings ...key.Binding) []string {
	byKey := map[string][]string{}
	for _, b := range bindings {
		for _, k := range b.Keys() {
			if desc := b.Help().Desc; !slices.Contains(byKey[k], desc) {
				byKey[k] = append(byKey[k], desc)
			}
		}
	}
	var collisions []string
	for k, descs := range byKey {
		if len(descs) > 1 {
			collisions = append(collisions, k+": "+strings.Join(descs, ", "))
		}
	}
	slices.Sort(collisions)
	return collisions
}
