package log

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStamps(t *testing.T) {
	at := time.Date(2026, 10, 13, 15, 4, 0, 0, time.Local)
	for p, want := range map[Period]string{Hourly: "2026-10-13T15", Daily: "2026-10-13", Weekly: "2026-W42", Monthly: "2026-10"} {
		stamp := p.stamp(at)
		if stamp != want {
			t.Errorf("%s: %q, want %q", p, stamp, want)
		}
		start, end, ok := parseStamp(stamp)
		if !ok || at.Before(start) || !at.Before(end) {
			t.Errorf("%s: %q is %v-%v, %v", p, stamp, start, end, ok)
		}
	}
	if start, _, _ := parseStamp("2026-W42"); !start.Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, time.Local)) {
		t.Errorf("week 42 starts %v", start)
	}
	for name, want := range map[string]string{
		"2026-10-13.log": "2026-10-13 0 false", "2026-10-13.3.log.gz": "2026-10-13 3 true",
		"2026-W42.1.log": "2026-W42 1 false", "notes.log": "", "2026-10-13.x.log": "", "2026-10-13.0.log": "",
		".2026-10-13.log.gz.12.tmp": "",
	} {
		stamp, seg, gz, ok := parseName(name)
		got := ""
		if ok {
			got = fmt.Sprintf("%s %d %v", stamp, seg, gz)
		}
		if got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}

// fakeClock makes now return at, advanced by step at each call.
func fakeClock(t *testing.T, at time.Time, step time.Duration) {
	t.Helper()
	var mu sync.Mutex
	saved := now
	now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		t := at
		at = at.Add(step)
		return t
	}
	t.Cleanup(func() { now = saved })
}

// writerEnv makes the test binary a log writer (TestHelperWriter) instead of the tests.
const writerEnv = "MANGAL_LOG_TEST_WRITER"

// TestHelperWriter is a process writing lines: id, dir, lines, max size, period, from
// writerEnv; its clock steps a minute per line from 2026-10-13 22:00.
func TestHelperWriter(t *testing.T) {
	args := strings.Split(os.Getenv(writerEnv), ",")
	if len(args) != 5 {
		t.Skip("only as a writer process")
	}
	lines, _ := strconv.Atoi(args[2])
	size, _ := strconv.ParseInt(args[3], 10, 64)
	period, _ := ParsePeriod(args[4])
	fakeClock(t, time.Date(2026, 10, 13, 22, 0, 0, 0, time.Local), time.Minute)
	Init(Options{Dir: args[1], Level: LevelInfo, MaxSize: size, Rotate: period})
	for i := range lines {
		Info("writer %s line %d %s", args[0], i, strings.Repeat("x", 40))
	}
	if err := Close(); err != nil {
		t.Fatal(err)
	}
}

// writers runs n writer processes at once.
func writers(t *testing.T, n int, dir string, lines int, size int64, period Period) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Go(func() {
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperWriter$", "-test.count=1")
			cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d,%s,%d,%d,%s", writerEnv, i, dir, lines, size, period))
			if out, err := cmd.CombinedOutput(); err != nil {
				errs[i] = fmt.Errorf("writer %d: %w\n%s", i, err, out)
			}
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

// readAll are the lines of every file (plain ones), checking each is a whole entry.
func readAll(t *testing.T, dir string) map[string][]string {
	t.Helper()
	files, err := listFiles(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	all := map[string][]string{}
	for _, f := range files {
		r, err := os.Open(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			if _, ok := parseLine(sc.Text()); !ok {
				t.Errorf("%s: broken line %q", f.Name, sc.Text())
			}
			all[f.Name] = append(all[f.Name], sc.Text())
		}
		r.Close()
	}
	return all
}

// Two processes write the same dir: the size limit starts new segments both use, the
// period change new files; every line is whole and there once.
func TestTwoWriters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	const lines, size = 300, 4096
	// 300 minutes from 22:00: three days, hourly files
	writers(t, 2, dir, lines, size, Hourly)

	all := readAll(t, dir)
	seen := map[string]bool{}
	segments := 0
	for name, ls := range all {
		stamp, seg, _, _ := parseName(name)
		segments = max(segments, seg)
		var bytes int
		for _, l := range ls {
			bytes += len(l) + 1
			e, _ := parseLine(l)
			if got := Hourly.stamp(e.Time); got != stamp {
				t.Errorf("%s has a line of %s", name, got)
			}
			if seen[e.Message] {
				t.Errorf("twice: %q", e.Message)
			}
			seen[e.Message] = true
		}
		// a writer goes over by its last line at most, each
		if bytes > size+2*len(ls[0])+2 {
			t.Errorf("%s: %d bytes", name, bytes)
		}
	}
	if len(seen) != 2*lines {
		t.Errorf("%d lines, want %d", len(seen), 2*lines)
	}
	if segments == 0 {
		t.Error("no file was split by size")
	}
	for _, stamp := range []string{"2026-10-13T22", "2026-10-14T00", "2026-10-14T02"} {
		if _, ok := all[stamp+".log"]; !ok {
			t.Errorf("no %s.log: %v", stamp, slices.Sorted(mapKeys(all)))
		}
	}
}

// Daily files without a size limit: one file per day, the old names.
func TestTwoWritersDaily(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	writers(t, 2, dir, 180, 0, Daily)
	all := readAll(t, dir)
	if names := slices.Sorted(mapKeys(all)); strings.Join(names, " ") != "2026-10-13.log 2026-10-14.log" {
		t.Errorf("files %v", names)
	}
	if n := len(all["2026-10-13.log"]) + len(all["2026-10-14.log"]); n != 360 {
		t.Errorf("%d lines", n)
	}
}

func mapKeys[K comparable, V any](m map[K]V) func(func(K) bool) {
	return func(yield func(K) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// The writer moves to the next segment once its file is full, and starts at the last one
// that isn't (another process's).
func TestSegments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	fakeClock(t, time.Date(2026, 10, 13, 10, 0, 0, 0, time.Local), time.Second)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// another process filled the first two
	for _, name := range []string{"2026-10-13.log", "2026-10-13.1.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("x", 200)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reset(t, Options{Dir: dir, Level: LevelInfo, MaxSize: 100})
	Info("one")
	if got := filepath.Base(Current()); got != "2026-10-13.2.log" {
		t.Errorf("writes %s, want the first segment not full", got)
	}
	Info("%s", strings.Repeat("y", 100))
	Info("three")
	if got := filepath.Base(Current()); got != "2026-10-13.3.log" {
		t.Errorf("writes %s after it was full", got)
	}
}

func logFile(t *testing.T, dir, name string, size int, mod time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.Repeat("2026-10-13T10:00:00.000 INFO  x\n", size)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func names(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var list []string
	for _, e := range entries {
		list = append(list, e.Name())
	}
	return strings.Join(list, " ")
}

// Retention deletes by age, count and total size, oldest first, never a file that may
// still be written (the current period's last segment, a full one written lately).
func TestRetention(t *testing.T) {
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.Local)
	old := at.Add(-time.Hour)
	setup := func(t *testing.T) string {
		dir := t.TempDir()
		logFile(t, dir, "2026-10-01.log", 1, time.Date(2026, 10, 1, 23, 0, 0, 0, time.Local))
		logFile(t, dir, "2026-10-10.log", 10, time.Date(2026, 10, 10, 23, 0, 0, 0, time.Local))
		logFile(t, dir, "2026-10-11.log", 10, time.Date(2026, 10, 11, 23, 0, 0, 0, time.Local))
		logFile(t, dir, "2026-10-12.log.gz", 10, time.Date(2026, 10, 12, 23, 0, 0, 0, time.Local))
		logFile(t, dir, "2026-10-13.log", 10, old)
		// full, but written a moment ago: another process may still be at it
		logFile(t, dir, "2026-10-13.1.log", 10, at.Add(-10*time.Second))
		logFile(t, dir, "2026-10-13.2.log", 10, at)
		logFile(t, dir, "notes.txt", 1, old)
		return dir
	}
	for _, c := range []struct {
		name string
		opts Options
		want string
	}{
		{"none", Options{}, "2026-10-01.log 2026-10-10.log 2026-10-11.log 2026-10-12.log.gz 2026-10-13.1.log 2026-10-13.2.log 2026-10-13.log notes.txt"},
		{"3 days", Options{Keep: 3}, "2026-10-11.log 2026-10-12.log.gz 2026-10-13.1.log 2026-10-13.2.log 2026-10-13.log notes.txt"},
		{"4 files", Options{MaxFiles: 4}, "2026-10-12.log.gz 2026-10-13.1.log 2026-10-13.2.log 2026-10-13.log notes.txt"},
		// the closed ones go until it fits: never the two that may be written
		{"2 files", Options{MaxFiles: 2}, "2026-10-13.1.log 2026-10-13.2.log notes.txt"},
		{"total size", Options{MaxTotalSize: 32 * 41}, "2026-10-12.log.gz 2026-10-13.1.log 2026-10-13.2.log 2026-10-13.log notes.txt"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := setup(t)
			if err := maintain(dir, at, c.opts); err != nil {
				t.Fatal(err)
			}
			if got := names(t, dir); got != c.want {
				t.Errorf("kept %s\nwant %s", got, c.want)
			}
		})
	}
}

// Compression only takes the closed files: the current one and a full one written lately
// stay plain. The content and the last write stay.
func TestCompressClosedOnly(t *testing.T) {
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.Local)
	dir := t.TempDir()
	day := time.Date(2026, 10, 12, 23, 0, 0, 0, time.Local)
	logFile(t, dir, "2026-10-12.log", 3, day)
	logFile(t, dir, "2026-10-13.log", 3, at.Add(-time.Hour))
	logFile(t, dir, "2026-10-13.1.log", 3, at.Add(-10*time.Second))
	logFile(t, dir, "2026-10-13.2.log", 3, at)
	// a temp file of a compression that died, and one of a running one
	logFile(t, dir, ".2026-10-11.log.gz.99.tmp", 1, at.Add(-2*time.Hour))
	logFile(t, dir, ".2026-10-11.log.gz.98.tmp", 1, at)
	if err := maintain(dir, at, Options{Compress: true}); err != nil {
		t.Fatal(err)
	}
	want := ".2026-10-11.log.gz.98.tmp 2026-10-12.log.gz 2026-10-13.1.log 2026-10-13.2.log 2026-10-13.log.gz"
	if got := names(t, dir); got != want {
		t.Errorf("files %s\nwant  %s", got, want)
	}
	info, err := os.Stat(filepath.Join(dir, "2026-10-12.log.gz"))
	if err != nil || !info.ModTime().Equal(day) {
		t.Errorf("last write %v, want %v (%v)", info.ModTime(), day, err)
	}
	files, _ := listFiles(dir, at)
	var got []string
	if err := Scan(context.Background(), files[:1], Filter{}, func(e Entry) error {
		got = append(got, e.Message)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "x,x,x" {
		t.Errorf("compressed file reads %q", got)
	}
}

// Init's options reach the writer: the files compressed and deleted when a file is opened,
// in the background, Close waits.
func TestMaintenanceOnOpen(t *testing.T) {
	dir := t.TempDir()
	logFile(t, dir, "2026-10-01.log", 1, time.Date(2026, 10, 1, 23, 0, 0, 0, time.Local))
	logFile(t, dir, "2026-10-12.log", 1, time.Date(2026, 10, 12, 23, 0, 0, 0, time.Local))
	fakeClock(t, time.Date(2026, 10, 13, 12, 0, 0, 0, time.Local), time.Second)
	reset(t, Options{Dir: dir, Level: LevelInfo, Keep: 3, Compress: true})
	Info("x")
	Close()
	if got := names(t, dir); got != "2026-10-12.log.gz 2026-10-13.log" {
		t.Errorf("files %s", got)
	}
}

// TestRepeatedHour: when the clocks go back, the repeated hour's file is still written.
func TestRepeatedHour(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	local := time.Local
	time.Local = ny
	t.Cleanup(func() { time.Local = local })
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "2026-11-01T01.log"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		now    time.Time
		closed bool
	}{
		{time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC), false}, // 01:30 EDT
		{time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC), false}, // 01:30 EST, again
		{time.Date(2026, 11, 1, 7, 5, 0, 0, time.UTC), true},   // 02:05 EST
	} {
		files, err := listFiles(dir, tc.now)
		if err != nil || len(files) != 1 || files[0].Closed != tc.closed {
			t.Errorf("at %s: %+v, %v; want closed %t", tc.now.In(ny), files, err, tc.closed)
		}
	}
}
