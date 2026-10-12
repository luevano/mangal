package library

import (
	"testing"
	"time"

	"github.com/luevano/mangal/v5/storage"
)

func TestSources(t *testing.T) {
	moved := time.Date(2026, 10, 12, 4, 0, 0, 0, time.Local)
	one := []storage.Source{{Provider: "mango-asurascans", MangaID: "dungeon-odyssey", Current: true}}
	if got := SourcesSummary(one); got != "mango-asurascans" {
		t.Errorf("one: %q", got)
	}
	several := []storage.Source{
		{Provider: "mango-weebcentral", MangaID: "01J", Moved: moved, Current: true},
		{Provider: "mango-asurascans", MangaID: "dungeon-odyssey"},
		{Provider: "mango-mangasee", MangaID: "Dungeon-Odyssey"},
	}
	if got := SourcesSummary(several); got != "mango-weebcentral (was mango-asurascans, mango-mangasee)" {
		t.Errorf("several: %q", got)
	}
	if SourcesSummary(nil) != "" {
		t.Error("none")
	}
	lines := SourceLines(several[:2])
	if len(lines) != 2 || lines[0] != "source: mango-weebcentral 01J · moved 2026-10-12" || lines[1] != "previous: mango-asurascans dungeon-odyssey" {
		t.Errorf("lines %q", lines)
	}
}
