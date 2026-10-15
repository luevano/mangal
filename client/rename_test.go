package client

import (
	"testing"

	"github.com/luevano/mangal/v5/library"
)

func TestRenameLines(t *testing.T) {
	p := RenamePlan{Root: "/dl"}
	for _, c := range []struct {
		r    library.Rename
		want string
	}{
		{library.Rename{From: "/dl/A", To: "/dl/B"}, "A -> B"},
		{library.Rename{From: "/dl/A/1.cbz", To: "/dl/A/[1] One.cbz", Chapter: true}, "A/1.cbz -> [1] One.cbz"},
		{library.Rename{From: "/dl/P", RemoveEmpty: true}, "remove P once empty"},
		{library.Rename{From: "/dl/A", To: "/dl/B", Skip: "taken"}, "skip A: taken"},
	} {
		if got := p.Line(c.r); got != c.want {
			t.Errorf("%+v: %q, want %q", c.r, got, c.want)
		}
	}
	if got := RenameSummary(RenameCounts([]library.Rename{{From: "a"}, {From: "b", Chapter: true}, {From: "c", Skip: "x"}})); got != "rename 1 folders, 1 chapter files" {
		t.Errorf("summary %q", got)
	}
}
