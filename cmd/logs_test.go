package cmd

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// writeLogs makes a log dir: a compressed day, and today in two segments.
func writeLogs(t *testing.T) (string, time.Time) {
	t.Helper()
	dir := t.TempDir()
	now := time.Date(2026, 10, 13, 15, 0, 0, 0, time.Local)
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte("2026-10-12T10:00:00.000 INFO  old one\n2026-10-12T11:00:00.000 ERROR old failure\n  at line 2\n"))
	zw.Close()
	files := map[string]string{
		"2026-10-12.log.gz": gz.String(),
		"2026-10-13.log":    "2026-10-13T09:00:00.000 DEBUG page 1\n2026-10-13T10:00:00.000 WARN  MangaDex timeout\n",
		"2026-10-13.1.log":  "2026-10-13T14:00:00.000 INFO  downloaded Foo\n",
		"notes.txt":         "not a log\n",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, now
}

func runLogsArgs(t *testing.T, dir string, now time.Time, args ...string) (string, string, error) {
	t.Helper()
	saved := logsArgs
	t.Cleanup(func() {
		logsArgs = saved
		logsCmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
	})
	logsCmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
	logsArgs = logsFlags{Level: "debug"}
	if err := logsCmd.Flags().Parse(args); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.Flags().AddFlagSet(logsCmd.Flags())
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())
	err := runLogs(cmd, dir, now)
	return out.String(), errOut.String(), err
}

func TestLogs(t *testing.T) {
	dir, now := writeLogs(t)
	tests := []struct {
		args       []string
		want, not  []string
		wantStderr string
	}{
		// the current period, both segments
		{nil, []string{"page 1", "MangaDex timeout", "downloaded Foo"}, []string{"old"}, ""},
		{[]string{"-a"}, []string{"old one", "old failure\n  at line 2", "downloaded Foo"}, nil, ""},
		{[]string{"--file", "2026-10-12.log"}, []string{"old one"}, []string{"Foo"}, ""},
		{[]string{"-a", "-L", "warn"}, []string{"old failure", "MangaDex timeout"}, []string{"old one", "page 1", "Foo"}, ""},
		// a capital: case-sensitive
		{[]string{"-a", "-g", "MangaDex|FAILURE"}, []string{"MangaDex timeout"}, []string{"old failure"}, ""},
		// lowercase: case-insensitive, the gzipped file is searched too
		{[]string{"-a", "-g", "mangadex|fail"}, []string{"MangaDex timeout", "old failure"}, []string{"Foo"}, ""},
		{[]string{"-a", "-g", "mangadex", "--case-sensitive"}, nil, []string{"timeout"}, "no log entries\n"},
		{[]string{"--since", "2026-10-12T10:30", "--until", "2026-10-13T09:30"}, []string{"old failure", "page 1"}, []string{"old one", "timeout"}, ""},
		{[]string{"--until", "2026-10-12"}, []string{"old one", "old failure"}, []string{"page 1"}, ""},
		{[]string{"--since", "2h"}, []string{"downloaded Foo"}, []string{"timeout"}, ""},
		{[]string{"-a", "-n", "2"}, []string{"timeout", "Foo"}, []string{"page 1", "old"}, ""},
		{[]string{"-g", "nothing"}, nil, nil, "no log entries in the current period (--all reads every file)\n"},
	}
	for _, tt := range tests {
		out, stderr, err := runLogsArgs(t, dir, now, tt.args...)
		if err != nil {
			t.Errorf("%v: %v", tt.args, err)
			continue
		}
		for _, s := range tt.want {
			if !strings.Contains(out, s) {
				t.Errorf("%v: no %q in\n%s", tt.args, s, out)
			}
		}
		for _, s := range tt.not {
			if strings.Contains(out, s) {
				t.Errorf("%v: %q in\n%s", tt.args, s, out)
			}
		}
		if stderr != tt.wantStderr {
			t.Errorf("%v: stderr %q", tt.args, stderr)
		}
	}
}

func TestLogsOrderAndJSON(t *testing.T) {
	dir, now := writeLogs(t)
	out, _, err := runLogsArgs(t, dir, now, "-a", "-j")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 5 {
		t.Fatalf("%d entries:\n%s", len(lines), out)
	}
	var first, last logEntryJSON
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[4]), &last); err != nil {
		t.Fatal(err)
	}
	if first.Message != "old one" || first.File != "2026-10-12.log.gz" || first.Level != "INFO" {
		t.Errorf("first %+v", first)
	}
	if last.Message != "downloaded Foo" || last.File != "2026-10-13.1.log" {
		t.Errorf("last %+v", last)
	}
}

func TestLogsList(t *testing.T) {
	dir, now := writeLogs(t)
	out, _, err := runLogsArgs(t, dir, now, "-l")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"2026-10-12.log.gz", "2026-10-13.log", "2026-10-13.1.log"}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != len(names) {
		t.Fatalf("list:\n%s", out)
	}
	for i, name := range names {
		if !strings.HasPrefix(lines[i], name+" ") {
			t.Errorf("line %d %q, want %s", i, lines[i], name)
		}
	}
}

func TestLogsErrors(t *testing.T) {
	dir, now := writeLogs(t)
	for _, args := range [][]string{
		{"--file", "2026-10-01.log"},
		{"-L", "loud"},
		{"--since", "yesterday"},
	} {
		if _, _, err := runLogsArgs(t, dir, now, args...); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}
