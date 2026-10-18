package mangas

import (
	"testing"

	"charm.land/bubbles/v2/key"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

// TestKeys: no key means two things on the screen (its keys, the list's, the global ones).
func TestKeys(t *testing.T) {
	s := New(nil, nil).(*state)
	k := s.keys
	own := util.KeyMap{Short: []key.Binding{k.confirm, k.search, k.anilist, k.metadata, k.info, k.watch, k.jump, k.toggleFullMeta}}
	if keys := base.KeyCollisions(own, s.List.KeyMap.Browsing()...); len(keys) != 0 {
		t.Error(keys)
	}
}

func TestJoinNotice(t *testing.T) {
	for _, tt := range [][3]string{{"", "", ""}, {"a", "", "a"}, {"", "b", "b"}, {"a", "b", "a · b"}} {
		if got := joinNotice(tt[0], tt[1]); got != tt[2] {
			t.Errorf("joinNotice(%q, %q) = %q", tt[0], tt[1], got)
		}
	}
}
