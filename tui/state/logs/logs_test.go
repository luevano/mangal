package logs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/util"
)

var ctx = context.Background()

func press(k string) tea.KeyPressMsg {
	switch k {
	case "esc":
		return util.KeyPress("esc")
	case "enter":
		return util.KeyPress("enter")
	case "tab":
		return util.KeyPress("tab")
	case "ctrl+n":
		return util.KeyPress("ctrl+n")
	case "ctrl+p":
		return util.KeyPress("ctrl+p")
	}
	return util.KeyPress(k)
}

// run gives the state the msgs of cmd, and of what they start; the rest are returned.
func run(s base.State, cmd tea.Cmd) []tea.Msg {
	var other []tea.Msg
	for _, msg := range base.Drain(cmd) {
		switch msg.(type) {
		case entriesMsg, periodMsg, filesMsg, search.SearchMsg, search.SearchCancelMsg:
			other = append(other, run(s, s.Update(ctx, msg))...)
		default:
			other = append(other, msg)
		}
	}
	return other
}

func typeIn(s base.State, text string) {
	for _, r := range text {
		run(s, s.Update(ctx, util.KeyPress(string(r))))
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const day12 = `2026-10-12T10:00:00.000 INFO  started
2026-10-12T11:00:00.000 ERROR get manga "x": not found
  caused by: 404
`

const day13 = `2026-10-13T09:00:00.000 DEBUG page 1
2026-10-13T09:30:00.000 WARN  mangadex: rate limited
2026-10-13T10:00:00.000 INFO  downloaded chapter 3
2026-10-13T10:30:00.000 ERROR mangadex: timeout
`

func logDir(t *testing.T) string {
	dir := t.TempDir()
	writeFile(t, dir, "2026-10-12.log", day12)
	writeFile(t, dir, "2026-10-13.log", day13)
	return dir
}

func view(s base.State) string { return ansi.Strip(s.View()) }

func TestFiles(t *testing.T) {
	dir := logDir(t)
	s := newFiles(dir)
	s.Resize(base.Size{Width: 80, Height: 20})
	if msgs := run(s, s.Init(ctx)); len(msgs) != 0 {
		t.Fatalf("msgs %v", msgs)
	}
	var titles []string
	for _, i := range s.List.Items() {
		titles = append(titles, i.(*item).Title())
	}
	if strings.Join(titles, "|") != "This session|2026-10-13.log|2026-10-12.log" {
		t.Errorf("items %q", titles)
	}
	if s.Key() != "logs/files" || s.Title().Text != "Logs · files" {
		t.Errorf("key %q, title %q", s.Key(), s.Title().Text)
	}

	s.List.Select(1)
	msgs := run(s, s.Update(ctx, press("enter")))
	v, ok := msgs[0].(*viewer)
	if !ok || v.Key() != "logs/2026-10-13.log" || !v.follow || v.Title().Text != "Logs · 2026-10-13.log" {
		t.Fatalf("enter: %#v", msgs[0])
	}

	// enter on the older file: not followed
	s.List.Select(2)
	msgs = run(s, s.Update(ctx, press("enter")))
	if v, ok := msgs[0].(*viewer); !ok || v.Key() != "logs/2026-10-12.log" || v.followable {
		t.Errorf("enter: %#v", msgs[0])
	}
	s.List.Select(0)
	if v := run(s, s.Update(ctx, press("enter")))[0].(*viewer); v.Key() != "logs/session" {
		t.Errorf("session: %s", v.Key())
	}

	// s searches every file
	run(s, s.Update(ctx, press("s")))
	if !s.Inputting() || s.Backable() {
		t.Fatal("not searching")
	}
	typeIn(s, "re:not found|timeout")
	msgs = run(s, s.Update(ctx, press("enter")))
	v, ok = msgs[0].(*viewer)
	if !ok || v.Key() != "logs-search/re:not found|timeout" {
		t.Fatalf("search: %#v", msgs)
	}
	v.Resize(base.Size{Width: 80, Height: 20})
	run(v, v.Init(ctx))
	got := view(v)
	for _, want := range []string{"── 2026-10-12.log", "get manga \"x\": not found", "  caused by: 404", "── 2026-10-13.log", "mangadex: timeout"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "rate limited") {
		t.Errorf("not a match:\n%s", got)
	}
}

func loadedFile(t *testing.T, dir, name string, newest bool, size base.Size) *viewer {
	t.Helper()
	files, err := log.Files(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Name == name {
			v := newFile(dir, f, newest)
			v.Resize(size)
			run(v, v.Init(ctx))
			return v
		}
	}
	t.Fatalf("no %s", name)
	return nil
}

func TestViewerFindAndLevel(t *testing.T) {
	v := loadedFile(t, logDir(t), "2026-10-13.log", false, base.Size{Width: 80, Height: 10})
	if got := view(v); !strings.Contains(got, "09:00:00 DEBUG page 1") || !strings.Contains(got, "10:30:00 ERROR mangadex: timeout") {
		t.Fatalf("view:\n%s", got)
	}

	run(v, v.Update(ctx, press("/")))
	if !v.Inputting() {
		t.Fatal("/ doesn't find")
	}
	typeIn(v, "mangadex")
	run(v, v.Update(ctx, press("enter")))
	if v.Inputting() || v.Backable() || len(v.matches) != 2 || v.current != 0 {
		t.Fatalf("find: inputting %v, matches %v, current %d", v.Inputting(), v.matches, v.current)
	}
	if !strings.Contains(v.Subtitle(), "match 1/2") {
		t.Errorf("subtitle %q", v.Subtitle())
	}
	run(v, v.Update(ctx, press("ctrl+n")))
	if v.current != 1 {
		t.Errorf("next: %d", v.current)
	}
	run(v, v.Update(ctx, press("ctrl+n")))
	if v.current != 0 {
		t.Errorf("next wraps: %d", v.current)
	}
	run(v, v.Update(ctx, press("ctrl+p")))
	if v.current != 1 {
		t.Errorf("prev wraps: %d", v.current)
	}

	// tab: info and up, warn and up (the current match stays)
	run(v, v.Update(ctx, press("tab")))
	run(v, v.Update(ctx, press("tab")))
	got := view(v)
	if strings.Contains(got, "page 1") || strings.Contains(got, "chapter 3") || !strings.Contains(got, "rate limited") {
		t.Errorf("warn and up:\n%s", got)
	}
	if v.current != 1 || !strings.Contains(v.Subtitle(), "warn and up") {
		t.Errorf("current %d, subtitle %q", v.current, v.Subtitle())
	}
	run(v, v.Update(ctx, press("tab")))
	run(v, v.Update(ctx, press("tab")))
	if v.level != log.LevelDebug {
		t.Errorf("tab goes around: %s", v.level)
	}

	// esc clears the find, then goes back
	run(v, v.Update(ctx, press("esc")))
	if v.matcher != nil || !v.Backable() {
		t.Errorf("esc: matcher %v", v.matcher)
	}
}

func TestViewerScrollAndWrap(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	for i := range 30 {
		sb.WriteString(time.Date(2026, 10, 13, 10, 0, i, 0, time.Local).Format("2006-01-02T15:04:05.000"))
		sb.WriteString(" INFO  line ")
		sb.WriteString(strings.Repeat("x", i))
		sb.WriteString("\n")
	}
	writeFile(t, dir, "2026-10-13.log", sb.String())
	v := loadedFile(t, dir, "2026-10-13.log", false, base.Size{Width: 30, Height: 5})
	// a line of 20 chars + 29 x's wraps
	if v.rows <= 30 {
		t.Errorf("rows %d", v.rows)
	}
	if got := view(v); len(strings.Split(got, "\n")) != 5 || !strings.HasPrefix(got, "10:00:00 INFO  line") {
		t.Errorf("top:\n%s", got)
	}
	run(v, v.Update(ctx, press("G")))
	lines := strings.Split(view(v), "\n")
	if len(lines) != 5 || !strings.HasSuffix(lines[4], "xx") {
		t.Errorf("end:\n%s", strings.Join(lines, "\n"))
	}
	if v.follow {
		t.Error("an older file follows")
	}
}

func TestViewerFollows(t *testing.T) {
	dir := logDir(t)
	v := loadedFile(t, dir, "2026-10-13.log", true, base.Size{Width: 80, Height: 3})
	followEvery = time.Millisecond
	t.Cleanup(func() { followEvery = 500 * time.Millisecond })
	if !v.follow || !strings.Contains(view(v), "timeout") {
		t.Fatalf("follow %v:\n%s", v.follow, view(v))
	}
	f, err := os.OpenFile(filepath.Join(dir, "2026-10-13.log"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("2026-10-13T11:00:00.000 INFO  new one\n")
	f.Close()
	// the next file of the period
	writeFile(t, dir, "2026-10-13.1.log", "2026-10-13T12:00:00.000 INFO  in the next file\n")
	// one read, the tick it starts isn't run
	v.Update(ctx, v.tick(ctx)())
	got := view(v)
	if !strings.Contains(got, "new one") || !strings.Contains(got, "in the next file") {
		t.Errorf("followed:\n%s", got)
	}
	if !strings.Contains(v.Subtitle(), "now in 2026-10-13.1.log") {
		t.Errorf("subtitle %q", v.Subtitle())
	}
}

func TestViewerSession(t *testing.T) {
	log.Info("logs test: a session line")
	v := newSession()
	v.Resize(base.Size{Width: 80, Height: 10})
	v.Init(ctx)
	if !strings.Contains(view(v), "a session line") {
		t.Errorf("session:\n%s", view(v))
	}
}

func TestKeys(t *testing.T) {
	s := newFiles(t.TempDir())
	if bad := base.KeyCollisions(s.keyMap(), s.List.KeyMap.Browsing()...); len(bad) != 0 {
		t.Errorf("files: %v", bad)
	}
	for _, v := range []*viewer{newViewer(kindFile), newPeriod(t.TempDir(), "")} {
		v.matcher = newMatcher("x")
		var all []key.Binding
		for _, b := range util.Bindings(v.KeyMap()) {
			// esc clears the find while there's one: back is off then (Backable)
			if b.Help().Desc != "clear find" {
				all = append(all, b)
			}
		}
		if bad := base.ViewerKeyCollisions(util.KeyMap{Short: all}); len(bad) != 0 {
			t.Errorf("viewer %s: %v", v.Title().Text, bad)
		}
	}
	// the main viewer's own keys are base's
	v := newPeriod(t.TempDir(), "")
	k := util.KeyMap{Short: []key.Binding{v.keys.files, v.keys.prevPeriod, v.keys.nextPeriod, v.keys.level, v.keys.next, v.keys.prev, v.keys.narrow, v.keys.copy}}
	if bad := base.KeyInconsistencies(k); len(bad) != 0 {
		t.Errorf("main viewer: %v", bad)
	}
}

// periodDir has two periods, the current one (2026-10-13 at noon) in three files.
func periodDir(t *testing.T) string {
	t.Helper()
	old := now
	now = func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.Local) }
	t.Cleanup(func() { now = old })
	dir := logDir(t)
	writeFile(t, dir, "2026-10-13.1.log", "2026-10-13T11:00:00.000 INFO  in the second file\n")
	writeFile(t, dir, "2026-10-13.2.log", "2026-10-13T11:30:00.000 INFO  in the third file\n")
	return dir
}

func TestCurrentLog(t *testing.T) {
	// ctrl+l
	if v, ok := New().(*viewer); !ok || !v.main || v.kind != kindPeriod || v.period != "" {
		t.Fatalf("New: %#v", v)
	}
	dir := periodDir(t)
	v := newMain(dir, nil)
	v.Resize(base.Size{Width: 80, Height: 20})
	run(v, v.Init(ctx))
	followEvery = time.Millisecond
	t.Cleanup(func() { followEvery = 500 * time.Millisecond })
	if v.Key() != "logs" || v.Title().Text != "Logs" || !v.follow {
		t.Fatalf("key %q, title %q, follow %v", v.Key(), v.Title().Text, v.follow)
	}
	got := view(v)
	// every file of the period, in order; not the other period's
	order := []string{"page 1", "mangadex: timeout", "in the second file", "in the third file"}
	last := -1
	for _, want := range order {
		i := strings.Index(got, want)
		if i < last {
			t.Errorf("%q missing or out of order in\n%s", want, got)
		}
		last = i
	}
	if strings.Contains(got, "started") {
		t.Errorf("another period:\n%s", got)
	}
	if !strings.HasPrefix(v.Subtitle(), "2026-10-13") {
		t.Errorf("subtitle %q", v.Subtitle())
	}

	// new entries, then the next period
	f, err := os.OpenFile(filepath.Join(dir, "2026-10-13.2.log"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("2026-10-13T23:59:00.000 INFO  late one\n")
	f.Close()
	writeFile(t, dir, "2026-10-14.log", "2026-10-14T00:01:00.000 INFO  tomorrow\n")
	v.Update(ctx, v.tick(ctx)())
	got = view(v)
	if !strings.Contains(got, "late one") || !strings.Contains(got, "tomorrow") {
		t.Errorf("followed:\n%s", got)
	}
	if !strings.HasPrefix(v.Subtitle(), "2026-10-14") {
		t.Errorf("subtitle %q", v.Subtitle())
	}
}

func TestPeriods(t *testing.T) {
	dir := periodDir(t)
	writeFile(t, dir, "2026-10-11.log", "2026-10-11T10:00:00.000 INFO  two days ago\n")
	v := newMain(dir, nil)
	v.Resize(base.Size{Width: 80, Height: 20})
	run(v, v.Init(ctx))
	if _, ok := v.stepPeriod(1); ok {
		t.Error("next period on the current one")
	}

	// a find stays across the periods
	run(v, v.Update(ctx, press("/")))
	typeIn(v, "o")
	run(v, v.Update(ctx, press("enter")))

	run(v, v.Update(ctx, press("[")))
	got := view(v)
	if v.Title().Text != "Logs · 2026-10-12" || v.Key() != "logs" || v.follow || v.followable {
		t.Errorf("title %q, key %q, follow %v", v.Title().Text, v.Key(), v.follow)
	}
	if !strings.Contains(got, "not found") || strings.Contains(got, "page 1") || v.matcher == nil {
		t.Errorf("[:\n%s", got)
	}
	run(v, v.Update(ctx, press("[")))
	if v.Title().Text != "Logs · 2026-10-11" || !strings.Contains(view(v), "two days ago") {
		t.Errorf("[ [: %q\n%s", v.Title().Text, view(v))
	}
	// the oldest
	run(v, v.Update(ctx, press("[")))
	if v.Title().Text != "Logs · 2026-10-11" || !v.loaded {
		t.Errorf("before the oldest: %q", v.Title().Text)
	}
	run(v, v.Update(ctx, press("]")))
	run(v, v.Update(ctx, press("]")))
	if v.Title().Text != "Logs" || !v.follow || !strings.Contains(view(v), "in the third file") {
		t.Errorf("] ]: %q\n%s", v.Title().Text, view(v))
	}

	// V: the files, past ones too
	msgs := run(v, v.Update(ctx, press("V")))
	if s, ok := msgs[0].(*files); !ok || s.Key() != "logs/files" || s.dir != dir {
		t.Errorf("V: %#v", msgs)
	}
}

func TestSessionFallback(t *testing.T) {
	log.Info("logs test: a fallback line")
	dir := periodDir(t)
	v := newMain(dir, errors.New("permission denied"))
	v.Resize(base.Size{Width: 80, Height: 10})
	run(v, v.Init(ctx))
	if v.Key() != "logs" || v.Title().Text != "Logs" || v.kind != kindSession {
		t.Fatalf("key %q, title %q, kind %v", v.Key(), v.Title().Text, v.kind)
	}
	if !strings.Contains(view(v), "a fallback line") || !strings.Contains(v.Subtitle(), "no log file: permission denied") {
		t.Errorf("%q\n%s", v.Subtitle(), view(v))
	}
	run(v, v.Update(ctx, press("[")))
	if v.kind != kindSession {
		t.Error("[ on the session")
	}
	if msgs := run(v, v.Update(ctx, press("V"))); len(msgs) != 1 {
		t.Errorf("V: %v", msgs)
	}

	// a log dir that can't be read
	notDir := filepath.Join(t.TempDir(), "file")
	writeFile(t, filepath.Dir(notDir), "file", "")
	v = newMain(notDir, nil)
	v.Resize(base.Size{Width: 80, Height: 10})
	run(v, v.Init(ctx))
	if v.kind != kindSession || !strings.Contains(view(v), "a fallback line") || !strings.Contains(v.Subtitle(), "no log file") {
		t.Errorf("unreadable: %v %q\n%s", v.kind, v.Subtitle(), view(v))
	}
}
