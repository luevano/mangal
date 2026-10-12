package cmd

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/pflag"
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

func TestSelectWatches(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	w := func(id int64, provider string, enabled bool, next time.Duration) watch.Watch {
		return watch.Watch{Watch: storage.Watch{ID: id, Provider: provider, Enabled: enabled}, NextCheck: now.Add(next)}
	}
	all := []watch.Watch{w(1, "p", true, -time.Hour), w(2, "p", true, time.Hour), w(3, "p", false, -time.Hour), w(4, "q", true, -time.Hour)}
	for _, tc := range []struct {
		ids      []int64
		provider string
		due      bool
		want     []int64
	}{
		{provider: "p", want: []int64{1, 2}},
		{provider: "p", due: true, want: []int64{1}},
		{due: true, want: []int64{1, 4}},
		{ids: []int64{2, 3, 4}, due: true, want: []int64{3, 4}},
	} {
		if got := selectWatches(all, tc.ids, tc.provider, tc.due, now); !slices.Equal(got, tc.want) {
			t.Errorf("%+v: %v", tc, got)
		}
	}
}

func TestScheduleFlag(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		want    string
		changed bool
		err     bool
	}{
		{args: nil},
		{args: []string{"--schedule", " @daily "}, want: "@daily", changed: true},
		{args: []string{"--schedule", ""}, want: "", changed: true},
		{args: []string{"--schedule", "every day"}, err: true},
		{args: []string{"--interval", "4h"}, want: "0 */4 * * *", changed: true},
		{args: []string{"--interval", "36h"}, want: "@every 36h", changed: true},
		{args: []string{"--interval", "4h", "--schedule", "@daily"}, err: true},
	} {
		var schedule string
		var interval time.Duration
		f := pflag.NewFlagSet("", pflag.ContinueOnError)
		f.StringVar(&schedule, "schedule", "", "")
		intervalAlias(f, &interval)
		if err := f.Parse(tc.args); err != nil {
			t.Fatal(err)
		}
		got, changed, err := scheduleFlag(f, schedule, interval)
		if (err != nil) != tc.err || got != tc.want || changed != tc.changed {
			t.Errorf("%v: %q %v %v", tc.args, got, changed, err)
		}
	}
	if f := watchSetCmd.Flags().Lookup("interval"); f == nil || !f.Hidden {
		t.Error("--interval isn't a hidden flag of watch set")
	}
}

func TestMoveArgs(t *testing.T) {
	isProvider := func(id string) bool { return strings.HasPrefix(id, "mango-") }
	for _, tc := range []struct {
		args            []string
		provider, query string
	}{
		{nil, "", ""},
		{[]string{"mango-weebcentral"}, "mango-weebcentral", ""},
		{[]string{"Berserk"}, "", "Berserk"},
		{[]string{"mid:01J76"}, "", "mid:01J76"},
		{[]string{"mango-weebcentral", "One-Punch", "Man"}, "mango-weebcentral", "One-Punch Man"},
		{[]string{"lua-x", "mid:1"}, "lua-x", "mid:1"},
	} {
		if p, q := moveArgs(tc.args, isProvider); p != tc.provider || q != tc.query {
			t.Errorf("%q: %q %q, want %q %q", tc.args, p, q, tc.provider, tc.query)
		}
	}
}

func TestPrintWatchesLibraryIssue(t *testing.T) {
	var out strings.Builder
	printWatches(&out, []watch.Watch{
		{Watch: storage.Watch{ID: 1, Provider: "mango-mangadex", Title: "Chastity Reverse World", Enabled: true}, LibraryIssue: "in the library under mango-mangasee (Teisou Gyakuten Sekai): move it there (M in the library)"},
		{Watch: storage.Watch{ID: 2, Provider: "mango-mangadex", Title: "Berserk", Enabled: true}},
	})
	got := out.String()
	for _, want := range []string{"Chastity Reverse World (not in library)", "not in the library:\n  1  Chastity Reverse World: in the library under mango-mangasee"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Berserk (not in library)") {
		t.Error("a linked watch marked")
	}
}
