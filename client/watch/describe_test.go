package watch

import (
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/storage"
)

func TestCheckLines(t *testing.T) {
	r := Result{WatchRun: storage.WatchRun{Title: "Dandadan", Provider: "mango-mangaplus", From: f32(65), Status: storage.StatusNew,
		Chapters: []storage.RunChapter{{Number: 65.5}, {Number: 66}, {Number: 70}}}}
	line := Describe(r, false)
	if line != "Checking for new chapters [65-] for Dandadan from mango-mangaplus... downloaded 3 (65 → 70)" {
		t.Errorf("got %q", line)
	}
	if got := outcome(r, true); got != "would download 3 (65.5, 66, 70)" {
		t.Errorf("dry run %q", got)
	}
	r.From, r.Gaps = nil, true
	if got := Describe(r, false); !strings.HasPrefix(got, "Downloading all chapters and gaps for Dandadan") {
		t.Errorf("first %q", got)
	}
	gaps := Result{Gaps: true, WatchRun: storage.WatchRun{Title: "OPM", Provider: "p", From: f32(241), Status: storage.StatusNew,
		Chapters: []storage.RunChapter{{Number: 233}, {Number: 234}}}}
	if got := outcome(gaps, false); got != "downloaded 2 (233 → 234)" {
		t.Errorf("gaps %q", got)
	}
	none := Result{WatchRun: storage.WatchRun{Status: storage.StatusNone}, Unavailable: 2}
	if got := outcome(none, false); got != "no new chapters, 2 unavailable" {
		t.Errorf("none %q", got)
	}
	failed := Result{WatchRun: storage.WatchRun{Status: storage.StatusError, Error: "site down"}}
	if got := outcome(failed, false); got != "ERROR site down" {
		t.Errorf("error %q", got)
	}
}
