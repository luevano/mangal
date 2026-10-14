package log

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func reset(t *testing.T, opts Options) {
	t.Helper()
	Init(opts)
	next, ringFull = 0, false
	t.Cleanup(func() { Close(); Init(Options{Level: LevelInfo}) })
}

func TestLevelsAndFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	reset(t, Options{Dir: dir, Level: LevelInfo})

	Debug("hidden %d", 1)
	Info("100%% done: %s", "x")
	Warn("careful")

	lines := Lines()
	if strings.Contains(lines, "hidden") {
		t.Errorf("debug line at info:\n%s", lines)
	}
	for _, want := range []string{"100% done: x", "WARN careful"} {
		if !strings.Contains(lines, want) {
			t.Errorf("no %q in:\n%s", want, lines)
		}
	}

	Close()
	data, err := os.ReadFile(filepath.Join(dir, time.Now().Format(time.DateOnly)+".log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "WARN  careful") || strings.Contains(string(data), "hidden") {
		t.Errorf("file:\n%s", data)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("dir %v %v", info.Mode(), err)
	}
}

func TestNoFileBeforeInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	reset(t, Options{Level: LevelInfo})
	Info("x")
	Init(Options{Dir: dir, Level: LevelInfo})
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("dir created before a line: %v", err)
	}
}

func TestRing(t *testing.T) {
	reset(t, Options{Level: LevelInfo})
	for i := range maxLines + 10 {
		Info("line %d", i)
	}
	lines := strings.Split(strings.TrimSpace(Lines()), "\n")
	if len(lines) != maxLines {
		t.Fatalf("%d lines", len(lines))
	}
	if !strings.HasSuffix(lines[0], "line 10") || !strings.HasSuffix(lines[maxLines-1], fmt.Sprintf("line %d", maxLines+9)) {
		t.Errorf("first %q last %q", lines[0], lines[maxLines-1])
	}
}

func TestConcurrent(t *testing.T) {
	reset(t, Options{Dir: t.TempDir(), Level: LevelDebug})
	hook := ProviderHook("p")
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			for j := range 100 {
				hook("page %d of %d", j, i)
				_ = Lines()
			}
		})
	}
	wg.Wait()
}

func TestProviderHook(t *testing.T) {
	reset(t, Options{Level: LevelInfo})
	hook := ProviderHook("mango-x")
	hook("page %d downloaded", 3)
	hook("mango-x: page %d downloaded", 4)
	hook("searching %q", "one piece")
	lines := Lines()
	if strings.Contains(lines, "page") {
		t.Errorf("page lines at info:\n%s", lines)
	}
	if !strings.Contains(lines, `mango-x: searching "one piece"`) {
		t.Errorf("lines:\n%s", lines)
	}
}

// TestFilePerDay: a long-running process (mangal serve) logs each day to that day's file,
// the files older than Keep days go.
func TestFilePerDay(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2026-01-01.log", "2026-01-08.log", "notes.txt", "2026-01-02-x.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	day := time.Date(2026, 1, 10, 23, 59, 0, 0, time.Local)
	saved := now
	now = func() time.Time { return day }
	t.Cleanup(func() { now = saved })
	reset(t, Options{Dir: dir, Level: LevelInfo, Keep: 3})

	Info("late")
	day = day.Add(2 * time.Minute)
	Info("early")
	Close()

	for name, want := range map[string]string{"2026-01-10.log": "late", "2026-01-11.log": "early"} {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err != nil || !strings.Contains(string(data), want) || strings.Count(string(data), "\n") != 1 {
			t.Errorf("%s: %q, %v", name, data, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	// kept: the last 3 days of the 11th, and what isn't a dated log
	if want := []string{"2026-01-02-x.log", "2026-01-10.log", "2026-01-11.log", "notes.txt"}; strings.Join(names, " ") != strings.Join(want, " ") {
		t.Errorf("files %v, want %v", names, want)
	}
}
