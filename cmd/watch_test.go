package cmd

import (
	"strings"
	"testing"
	"time"
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
