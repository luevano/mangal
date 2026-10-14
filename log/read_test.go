package log

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseLine(t *testing.T) {
	e, ok := parseLine("2026-10-13T15:04:05.123 WARN  provider: page 3 failed")
	want := time.Date(2026, 10, 13, 15, 4, 5, 123e6, time.Local)
	if !ok || !e.Time.Equal(want) || e.Level != LevelWarn || e.Message != "provider: page 3 failed" {
		t.Errorf("%+v %v", e, ok)
	}
	if e.String() != "2026-10-13T15:04:05.123 WARN  provider: page 3 failed" {
		t.Errorf("String %q", e.String())
	}
	for _, line := range []string{"", "  caused by: x", "2026-10-13T15:04:05.123 NOPE  x", "2026-10-13 15:04:05.123 INFO  x"} {
		if _, ok := parseLine(line); ok {
			t.Errorf("%q is an entry", line)
		}
	}
}

func TestPattern(t *testing.T) {
	for _, c := range []struct {
		expr string
		cs   bool
		text string
		want bool
	}{
		{"error", false, "ERROR here", true},
		{"Error", false, "error here", false},
		{"error", true, "ERROR here", false},
		{"page \\d+", false, "page 12", true},
		{"(unclosed", false, "an (unclosed paren", true},
	} {
		if got := Pattern(c.expr, c.cs).MatchString(c.text); got != c.want {
			t.Errorf("%q (case %v) on %q: %v", c.expr, c.cs, c.text, got)
		}
	}
	if Pattern("", false) != nil {
		t.Error("an empty pattern matches something")
	}
}

func writeLog(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const day12 = `2026-10-12T10:00:00.000 INFO  started
2026-10-12T11:00:00.000 ERROR get manga "x": not found
  caused by: 404
2026-10-12T12:00:00.000 DEBUG page 1
`

const day13 = `2026-10-13T09:00:00.000 WARN  rate limited
2026-10-13T10:00:00.000 INFO  downloaded chapter 3
`

func scanAll(t *testing.T, dir string, f Filter) []string {
	t.Helper()
	files, err := Files(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := Scan(context.Background(), files, f, func(e Entry) error {
		got = append(got, e.File+": "+e.Message)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return got
}

// Scan reads plain and compressed files in order, an entry keeps its continuation lines,
// the filter picks by time, level and pattern.
func TestScan(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "2026-10-12.log", day12)
	writeLog(t, dir, "2026-10-13.log", day13)
	writeLog(t, dir, "notes.txt", "2026-10-13T10:00:00.000 INFO  not a log\n")
	files, _ := listFiles(dir, time.Date(2026, 10, 14, 0, 0, 0, 0, time.Local))
	if err := compress(files[0]); err != nil {
		t.Fatal(err)
	}

	all := scanAll(t, dir, Filter{})
	want := []string{
		"2026-10-12.log.gz: started",
		"2026-10-12.log.gz: get manga \"x\": not found\n  caused by: 404",
		"2026-10-12.log.gz: page 1",
		"2026-10-13.log: rate limited",
		"2026-10-13.log: downloaded chapter 3",
	}
	if strings.Join(all, "|") != strings.Join(want, "|") {
		t.Errorf("all:\n%q\nwant\n%q", all, want)
	}
	for _, c := range []struct {
		name   string
		filter Filter
		want   string
	}{
		{"level", Filter{Level: LevelWarn}, "2026-10-12.log.gz: get manga \"x\": not found\n  caused by: 404|2026-10-13.log: rate limited"},
		{"pattern", Filter{Pattern: Pattern("chapter|404", false)}, "2026-10-12.log.gz: get manga \"x\": not found\n  caused by: 404|2026-10-13.log: downloaded chapter 3"},
		{"since", Filter{Since: time.Date(2026, 10, 12, 11, 30, 0, 0, time.Local)}, "2026-10-12.log.gz: page 1|2026-10-13.log: rate limited|2026-10-13.log: downloaded chapter 3"},
		{"until", Filter{Until: time.Date(2026, 10, 12, 10, 30, 0, 0, time.Local)}, "2026-10-12.log.gz: started"},
	} {
		if got := strings.Join(scanAll(t, dir, c.filter), "|"); got != c.want {
			t.Errorf("%s: %q\nwant %q", c.name, got, c.want)
		}
	}
}

// A tail gets what's appended, a line written in two parts once whole, then moves on to a
// newer file.
func TestTail(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "2026-10-13.log", day13)
	tail, err := NewTail(dir)
	if err != nil {
		t.Fatal(err)
	}
	next := func() []string {
		t.Helper()
		var got []string
		if err := tail.Next(Filter{}, func(e Entry) error {
			got = append(got, e.File+": "+e.Message)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := next(); len(got) != 0 {
		t.Errorf("the lines before: %q", got)
	}
	appendTo := func(name, s string) {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString(s)
		f.Close()
	}
	appendTo("2026-10-13.log", "2026-10-13T11:00:00.000 INFO  one\n2026-10-13T11:00:01.000 INFO  tw")
	if got := next(); strings.Join(got, "|") != "2026-10-13.log: one" {
		t.Errorf("got %q", got)
	}
	appendTo("2026-10-13.log", "o\n")
	appendTo("2026-10-13.1.log", "2026-10-13T12:00:00.000 INFO  three\n")
	if got := next(); strings.Join(got, "|") != "2026-10-13.log: two|2026-10-13.1.log: three" {
		t.Errorf("got %q", got)
	}
	if tail.Following() != "2026-10-13.1.log" {
		t.Errorf("follows %s", tail.Following())
	}
}

// Follow stops with ctx.
func TestTailAt(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "2026-10-12.log", day12)
	writeLog(t, dir, "2026-10-13.log", day13)
	files, err := Files(dir)
	if err != nil {
		t.Fatal(err)
	}
	tail := NewTailAt(dir, files[0])
	var got []string
	if err := tail.Next(Filter{Level: LevelWarn}, func(e Entry) error {
		got = append(got, e.File+": "+e.Message)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := "2026-10-12.log: get manga \"x\": not found\n  caused by: 404|2026-10-13.log: rate limited"
	if strings.Join(got, "|") != want || tail.Following() != "2026-10-13.log" {
		t.Errorf("got %q, following %s", got, tail.Following())
	}
}

func TestFollow(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Follow(ctx, dir, 10*time.Millisecond, Filter{}, func(e Entry) error {
			got <- e.Message
			return nil
		})
	}()
	time.Sleep(30 * time.Millisecond)
	writeLog(t, dir, "2026-10-13.log", "2026-10-13T11:00:00.000 INFO  new\n")
	select {
	case m := <-got:
		if m != "new" {
			t.Errorf("got %q", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing followed")
	}
	cancel()
	if err := <-done; err != nil {
		t.Error(err)
	}
}
