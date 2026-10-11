package volumes

import (
	"testing"

	"github.com/luevano/mangal/v5/tui/base"
)

// TestKeys: no key means two things on the screen (its keys, the list's, the global ones).
func TestKeys(t *testing.T) {
	s := New(nil, nil, nil).(*state)
	if keys := base.KeyCollisions(s.keyMap(), s.List.KeyMap.Browsing()...); len(keys) != 0 {
		t.Error(keys)
	}
}
