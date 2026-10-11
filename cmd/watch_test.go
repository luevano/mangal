package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
)

func TestParseSince(t *testing.T) {
	for in, want := range map[string]time.Duration{"7d": 7 * 24 * time.Hour, "1.5d": 36 * time.Hour, "12h": 12 * time.Hour, "0": 0} {
		if got, err := parseSince(in); err != nil || got != want {
			t.Errorf("%s: %s, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "d", "-1d", "7days", "-2h"} {
		if _, err := parseSince(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestParseIDs(t *testing.T) {
	if ids, err := parseIDs([]string{"3", "12"}); err != nil || len(ids) != 2 || ids[1] != 12 {
		t.Errorf("%v, %v", ids, err)
	}
	for _, bad := range []string{"0", "-1", "x", "1.5"} {
		if _, err := parseIDs([]string{bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func f32(f float32) *float32 { return &f }

func TestCheckLines(t *testing.T) {
	w := storage.Watch{Title: "Dandadan", Provider: "mango-mangaplus"}
	r := watch.Result{WatchRun: storage.WatchRun{From: f32(65), Status: storage.StatusNew,
		Chapters: []storage.RunChapter{{Number: 65.5}, {Number: 66}, {Number: 70}}}}
	line := checkLine(w, r) + " " + checkOutcome(r, false)
	if line != "Checking for new chapters [65-] for Dandadan from mango-mangaplus... downloaded 3 (65 → 70)" {
		t.Errorf("got %q", line)
	}
	if got := checkOutcome(r, true); got != "would download 3 (65.5, 66, 70)" {
		t.Errorf("dry run %q", got)
	}
	r.From, r.Gaps = nil, true
	if got := checkLine(w, r); !strings.HasPrefix(got, "Downloading all chapters and gaps for Dandadan") {
		t.Errorf("first %q", got)
	}
	none := watch.Result{WatchRun: storage.WatchRun{Status: storage.StatusNone}, Unavailable: 2}
	if got := checkOutcome(none, false); got != "no new chapters, 2 unavailable" {
		t.Errorf("none %q", got)
	}
	failed := watch.Result{WatchRun: storage.WatchRun{Status: storage.StatusError, Error: "site down"}}
	if got := checkOutcome(failed, false); got != "ERROR site down" {
		t.Errorf("error %q", got)
	}
}

func TestWatchFlags(t *testing.T) {
	cmd, args, err := rootCmd.Find([]string{"watch", "add", "mango-mangapill", "One-Punch", "Man", "--from", "200", "-a", "21087"})
	if err != nil || cmd != watchAddCmd {
		t.Fatalf("%v, %v", cmd, err)
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	defer func() { watchAddArgs.From, watchAddArgs.AnilistID = 0, 0 }()
	if rest := cmd.Flags().Args(); strings.Join(rest[1:], " ") != "One-Punch Man" || watchAddArgs.From != 200 || watchAddArgs.AnilistID != 21087 ||
		!cmd.Flags().Changed("from") || cmd.Flags().Changed("gaps") {
		t.Errorf("parsed %v %+v", rest, watchAddArgs)
	}
	for _, sub := range []string{"ls", "rm", "pause", "resume", "set", "check", "log", "stale"} {
		if c, _, err := rootCmd.Find([]string{"watch", sub}); err != nil || c.Name() != sub {
			t.Errorf("watch %s: %v", sub, err)
		}
	}
}
