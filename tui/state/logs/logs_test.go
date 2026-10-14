package logs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/util"
)

var ctx = context.Background()

func press(k string) tea.KeyMsg {
	switch k {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+n":
		return tea.KeyMsg{Type: tea.KeyCtrlN}
	case "ctrl+p":
		return tea.KeyMsg{Type: tea.KeyCtrlP}
	case "G":
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// run gives the state the msgs of cmd, and of what they start; the rest are returned.
func run(s base.State, cmd tea.Cmd) []tea.Msg {
	var other []tea.Msg
	for _, msg := range base.Drain(cmd) {
		switch msg.(type) {
		case entriesMsg, filesMsg, search.SearchMsg, search.SearchCancelMsg:
			other = append(other, run(s, s.Update(ctx, msg))...)
		default:
			other = append(other, msg)
		}
	}
	return other
}

func typeIn(s base.State, text string) {
	for _, r := range text {
		run(s, s.Update(ctx, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
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

func TestFilesOpenTheNewest(t *testing.T) {
	dir := logDir(t)
	s := newFiles(dir)
	s.Resize(base.Size{Width: 80, Height: 20})
	msgs := run(s, s.Init(ctx))
	var titles []string
	for _, i := range s.List.Items() {
		titles = append(titles, i.(*item).Title())
	}
	if strings.Join(titles, "|") != "This session|2026-10-13.log|2026-10-12.log" {
		t.Errorf("items %q", titles)
	}
	if len(msgs) != 1 {
		t.Fatalf("msgs %v", msgs)
	}
	v, ok := msgs[0].(*viewer)
	if !ok || v.Key() != "logs/2026-10-13.log" || !v.follow {
		t.Fatalf("pushed %#v", msgs[0])
	}
	// shown again: no second viewer
	if msgs := run(s, s.Update(ctx, base.RestoredMsg{})); len(msgs) != 0 {
		t.Errorf("restored: %v", msgs)
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
	typeIn(s, "not found|timeout")
	msgs = run(s, s.Update(ctx, press("enter")))
	v, ok = msgs[0].(*viewer)
	if !ok || v.Key() != "logs-search/not found|timeout" {
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
	if v.pattern != nil || !v.Backable() {
		t.Errorf("esc: pattern %v", v.pattern)
	}
}

func TestViewerScrollAndWrap(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	for i := range 30 {
		sb.WriteString(time.Date(2026, 10, 13, 10, 0, i, 0, time.Local).Format("2006-01-02T15:04:05.000") + " INFO  line " + strings.Repeat("x", i) + "\n")
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
	v := newViewer(kindFile)
	v.pattern = log.Pattern("x", false)
	var all []key.Binding
	for _, b := range util.Bindings(v.KeyMap()) {
		// esc clears the find while there's one: back is off then (Backable)
		if b.Help().Desc != "clear find" {
			all = append(all, b)
		}
	}
	if bad := base.ViewerKeyCollisions(util.KeyMap{Short: all}); len(bad) != 0 {
		t.Errorf("viewer: %v", bad)
	}
}
