package health

import (
	"context"
	"strings"
	"testing"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/health"
	"github.com/luevano/mangal/v5/tui/base"
	tuilibrary "github.com/luevano/mangal/v5/tui/state/library"
)

func TestKeys(t *testing.T) {
	summary := health.Finding{Check: "W10", Items: []health.Finding{{Check: "W10"}}}
	for name, s := range map[string]*state{"health": newState(nil), "summary": newState(&summary)} {
		if keys := base.KeyCollisions(s.keyMap(), s.List.KeyMap.Browsing()...); len(keys) != 0 {
			t.Errorf("%s: %v", name, keys)
		}
	}
}

func TestHints(t *testing.T) {
	s := newState(nil)
	for _, c := range []struct {
		f    health.Finding
		want string
	}{
		{health.Finding{Check: "W3", Subject: health.Subject{Kind: "watch", ID: 4}, Fix: health.Fix{Action: &health.Action{Kind: health.ActionResume, Watch: 4}}},
			"enter watch details · x resume"},
		{health.Finding{Check: "M1", Subject: health.Subject{Kind: "manga", ID: 2},
			Fix: health.Fix{Action: &health.Action{Kind: health.ActionLink, Source: metadata.IDCodeAnilist, IDs: []string{"30021"}}}},
			"enter links · x link AniList 30021"},
		{health.Finding{Check: "M6", Subject: health.Subject{Kind: "manga", ID: 1}, Fix: health.Fix{Action: &health.Action{Kind: health.ActionMerge, Mangas: []int64{1, 2}}}},
			"enter library manga · x merge · J merge"},
		{health.Finding{Check: "W10", Subject: health.Subject{Kind: "library", Name: "library"}, Items: make([]health.Finding, 3)},
			"enter list them (3)"},
		{health.Finding{Check: "L3", Subject: health.Subject{Kind: "provider", Name: "mango-mangasee"}, Fix: health.Fix{Commands: []string{"mangal migrate"}}},
			"enter migrate"},
		{health.Finding{Check: "A1", Subject: health.Subject{Kind: "account", Name: "storage"}, Fix: health.Fix{}}, ""},
		{health.Finding{Check: "L6", Subject: health.Subject{Kind: "library", Name: "trash"}, Fix: health.Fix{Action: &health.Action{Kind: health.ActionEmptyTrash}}},
			"enter trash · x empty"},
		{health.Finding{Check: "L1", Subject: health.Subject{Kind: "manga", ID: 3}, Fix: health.Fix{Action: &health.Action{Kind: health.ActionForget, Manga: 3}}},
			"enter library manga · x scan or forget"},
		{health.Finding{Check: "L2", Subject: health.Subject{Kind: "library", Name: "library"}, Items: make([]health.Finding, 2),
			Fix: health.Fix{Action: &health.Action{Kind: health.ActionMark, Mangas: []int64{1, 2}}}},
			"enter list them (2) · x mark 2 folders"},
		{health.Finding{Check: "L4", Subject: health.Subject{Kind: "directory", Name: "mango-mangasee"},
			Fix: health.Fix{Action: &health.Action{Kind: health.ActionRename, Paths: []string{"/dl/mango-mangasee"}}}},
			"enter rename screen · x rename"},
	} {
		if got := s.hint(c.f); got != c.want {
			t.Errorf("%s: %q, want %q", c.f.Check, got, c.want)
		}
	}
}

func TestCategoriesAndNetworkKept(t *testing.T) {
	s := newState(nil)
	w12 := health.Finding{Check: "W12", Category: health.Watches, Subject: health.Subject{Kind: "manga", ID: 5}}
	gone := health.Finding{Check: "W12", Category: health.Watches, Subject: health.Subject{Kind: "manga", ID: 6}}
	w10 := health.Finding{Check: "W10", Category: health.Watches, Subject: health.Subject{Kind: "library"},
		Items: []health.Finding{{Subject: health.Subject{Kind: "manga", ID: 5}}}}
	g1 := health.Finding{Check: "G1", Category: health.Gaps}
	s.onLoaded(loadedMsg{network: true, report: health.Report{Findings: []health.Finding{w12, gone, w10, g1},
		Counts: health.Counts{ByCategory: map[health.Category]int{health.Watches: 3, health.Gaps: 1}}}})
	// offline again: W12 of a manga still unwatched stays, the other one was watched
	s.onLoaded(loadedMsg{report: health.Report{Findings: []health.Finding{w10, g1},
		Counts: health.Counts{ByCategory: map[health.Category]int{health.Watches: 1, health.Gaps: 1}}}})
	var checks []string
	for _, f := range s.report.Findings {
		checks = append(checks, f.Check)
	}
	if len(checks) != 3 || checks[1] != "W12" || s.report.Findings[1].Subject.ID != 5 || !s.network {
		t.Errorf("findings %v (network %v)", checks, s.network)
	}
	for _, want := range []health.Category{health.Watches, health.Gaps, ""} {
		s.category = s.nextCategory()
		if s.category != want {
			t.Errorf("category %q, want %q", s.category, want)
		}
	}
}

func TestEmptyTrashListsBlockedUndos(t *testing.T) {
	s := newState(nil)
	s.Resize(base.Size{Width: 120, Height: 40})
	s.Update(context.Background(), trashCheckedMsg{message: "3 files in the trash", blocked: tuilibrary.TrashCheckedMsg{{ID: 7, Kind: "delete", Summary: "delete Berserk"}}})
	view := s.Confirm.View()
	for _, want := range []string{"3 files in the trash", "These operations can't be undone", "delete Berserk", "Delete them for good?"} {
		if !strings.Contains(view, want) {
			t.Errorf("confirm misses %q:\n%s", want, view)
		}
	}
}
