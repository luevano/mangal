package path

import (
	"testing"

	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

// TestKeys: the table's keys are the lists' (h/l page), none is the screen's or a global
// one.
func TestKeys(t *testing.T) {
	s := New().(*state)
	if keys := base.KeyCollisions(s.KeyMap(), util.TableBrowsing(s.table.KeyMap)...); len(keys) != 0 {
		t.Error(keys)
	}
	if bad := base.KeyInconsistencies(s.KeyMap()); len(bad) != 0 {
		t.Error(bad)
	}
}
