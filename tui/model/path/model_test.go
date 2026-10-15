package path

import (
	"context"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/path"
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

// TestRows: every path, by name, the path column takes the width.
func TestRows(t *testing.T) {
	s := New().(*state)
	s.Resize(base.Size{Width: 100, Height: 20})
	if w := s.table.Columns()[1].Width; w != 100-10-4 {
		t.Errorf("path column %d wide", w)
	}
	rows := s.table.Rows()
	if len(rows) != len(path.All().Keys()) || len(rows) == 0 {
		t.Fatalf("%d rows", len(rows))
	}
	for i, r := range rows {
		if i > 0 && rows[i-1][0] > r[0] {
			t.Errorf("not sorted: %s before %s", rows[i-1][0], r[0])
		}
		if r[1] == "" {
			t.Errorf("%s: no path", r[0])
		}
	}
	if !strings.Contains(s.View(), rows[0][0]) {
		t.Errorf("view:\n%s", s.View())
	}
	// the copy itself isn't run: it would write the clipboard
	if cmd := s.Update(context.Background(), util.KeyPress("c")); cmd == nil {
		t.Error("c doesn't copy")
	}
}
