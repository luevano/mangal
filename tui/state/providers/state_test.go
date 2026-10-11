package providers

import (
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/tui/base"
)

// TestKeys: no key means two things on the screen (its keys, the list's, the global ones).
func TestKeys(t *testing.T) {
	s := New(nil).(*state)
	m := newMirrors(libmangal.ProviderInfo{ID: "mango-mangapill"})
	for name, keys := range map[string][]string{
		"providers": base.KeyCollisions(s.keyMap(), s.List.KeyMap.Browsing()...),
		"mirrors":   base.KeyCollisions(m.keyMap(), m.List.KeyMap.Browsing()...),
	} {
		if len(keys) != 0 {
			t.Errorf("%s: %v", name, keys)
		}
	}
}
