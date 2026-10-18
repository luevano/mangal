package logs

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/search"
	"github.com/luevano/mangal/v5/tui/util"
)

func TestMatcher(t *testing.T) {
	for _, tc := range []struct {
		query, s string
		want     bool
	}{
		// fuzzy: a subsequence
		{"mdx", "mangadex: timeout", true},
		{"xdm", "mangadex: timeout", false},
		// words in any order
		{"tmout mdx", "mangadex: timeout", true},
		{"tmout mdx", "mangadex: rate limited", false},
		// case-insensitive unless the query has a capital
		{"timeout", "TIMEOUT", true},
		{"Timeout", "timeout", false},
		{"Timeout", "a Timeout", true},
		// regexp
		{"re:time.ut$", "mangadex: timeout", true},
		{"re:^time", "mangadex: timeout", false},
		{"re:TIME", "timeout", false},
		{"re:time", "TIMEOUT", true},
		// an invalid regexp is text
		{"re:get(", "get(x)", true},
	} {
		if got := newMatcher(tc.query).match(tc.s); got != tc.want {
			t.Errorf("%q in %q: %v", tc.query, tc.s, got)
		}
	}
	for _, q := range []string{"", "re:", "re: "} {
		if newMatcher(q) != nil {
			t.Errorf("%q: a matcher", q)
		}
	}
}

func TestMatcherSpans(t *testing.T) {
	for _, tc := range []struct {
		query, s string
		want     [][2]int
	}{
		{"timeout", "mangadex: timeout", [][2]int{{10, 17}}},
		// the characters, adjacent ones together
		{"mdx", "md x", [][2]int{{0, 2}, {3, 4}}},
		{"tmout mdx", "md x tmout", [][2]int{{0, 2}, {3, 4}, {5, 10}}},
		// multi-byte runes
		{"ñé", "año é", [][2]int{{1, 3}, {5, 7}}},
		// a capital: the right case, not the best folded one
		{"Ab", "ab Ab", [][2]int{{3, 5}}},
		{"re:o+", "foo bar oo", [][2]int{{1, 3}, {8, 10}}},
	} {
		if got := newMatcher(tc.query).spans(tc.s); !slices.Equal(got, tc.want) {
			t.Errorf("%q in %q: %v, want %v", tc.query, tc.s, got, tc.want)
		}
	}
}

// marked makes the highlights visible without colors: [match], {current match}.
func marked(t *testing.T) {
	old, oldCurrent := matchStyle, currentMatchStyle
	matchStyle = lipgloss.NewStyle().Transform(func(s string) string { return "[" + s + "]" })
	currentMatchStyle = lipgloss.NewStyle().Transform(func(s string) string { return "{" + s + "}" })
	t.Cleanup(func() { matchStyle, currentMatchStyle = old, oldCurrent })
}

func find(v *viewer, query string) {
	run(v, v.Update(ctx, search.SearchMsg(query)))
}

func TestHighlightAll(t *testing.T) {
	marked(t)
	v := loadedFile(t, logDir(t), "2026-10-13.log", false, base.Size{Width: 80, Height: 10})
	find(v, "mdx")
	got := view(v)
	// every match line, the current one stronger; not the others
	for _, want := range []string{"{m}anga{d}e{x}: rate limited", "[m]anga[d]e[x]: timeout", "DEBUG page 1", "INFO  downloaded chapter 3"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	if !strings.Contains(v.Subtitle(), "match 1/2 · all lines") {
		t.Errorf("subtitle %q", v.Subtitle())
	}
	run(v, v.Update(ctx, press("ctrl+n")))
	got = view(v)
	if !strings.Contains(got, "[m]anga[d]e[x]: rate") || !strings.Contains(got, "{m}anga{d}e{x}: timeout") {
		t.Errorf("next:\n%s", got)
	}

	find(v, "re:a[dt]")
	got = view(v)
	for _, want := range []string{"mang{ad}ex: r{at}e limited", "downlo[ad]ed", "mang[ad]ex: timeout", "DEBUG page 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
}

func TestNarrow(t *testing.T) {
	v := loadedFile(t, logDir(t), "2026-10-13.log", false, base.Size{Width: 80, Height: 10})
	if len(withNarrow(v)) != 0 {
		t.Fatal("narrow without a find")
	}
	find(v, "mdx")
	run(v, v.Update(ctx, press("ctrl+n")))
	if len(withNarrow(v)) != 1 {
		t.Fatal("no narrow key")
	}
	run(v, v.Update(ctx, press("&")))
	got := view(v)
	if strings.Contains(got, "page 1") || strings.Contains(got, "chapter 3") || !strings.Contains(got, "rate limited") || !strings.Contains(got, "timeout") {
		t.Errorf("narrowed:\n%s", got)
	}
	// time order, the current match kept
	if strings.Index(got, "rate limited") > strings.Index(got, "timeout") || v.current != 1 {
		t.Errorf("current %d:\n%s", v.current, got)
	}
	if !strings.Contains(v.Subtitle(), "match 2/2 · narrowed") {
		t.Errorf("subtitle %q", v.Subtitle())
	}
	run(v, v.Update(ctx, press("ctrl+n")))
	if v.current != 0 {
		t.Errorf("next wraps narrowed: %d", v.current)
	}

	// the level too: error and up
	for range 3 {
		run(v, v.Update(ctx, press("tab")))
	}
	got = view(v)
	if strings.Contains(got, "rate limited") || !strings.Contains(got, "timeout") || len(v.shown) != 1 {
		t.Errorf("error and up, narrowed:\n%s", got)
	}
	run(v, v.Update(ctx, press("tab")))

	// a new find stays narrowed
	find(v, "chapter")
	if got := view(v); strings.Contains(got, "timeout") || !strings.Contains(got, "chapter 3") {
		t.Errorf("new find:\n%s", got)
	}
	run(v, v.Update(ctx, press("&")))
	if got := view(v); !strings.Contains(got, "page 1") || !strings.Contains(v.Subtitle(), "all lines") {
		t.Errorf("all lines %q:\n%s", v.Subtitle(), got)
	}
	run(v, v.Update(ctx, press("&")))
	// esc clears the find and the narrowing
	run(v, v.Update(ctx, press("esc")))
	if v.narrow || len(v.shown) != 4 {
		t.Errorf("esc: narrow %v, shown %d", v.narrow, len(v.shown))
	}
}

func withNarrow(v *viewer) []string {
	var keys []string
	for _, k := range util.Bindings(v.KeyMap()) {
		if k.Enabled() && k.Help().Desc == "narrow" {
			keys = append(keys, k.Help().Key)
		}
	}
	return keys
}

func appendLine(t *testing.T, dir, name, line string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

func TestFollowNarrow(t *testing.T) {
	dir := logDir(t)
	v := loadedFile(t, dir, "2026-10-13.log", true, base.Size{Width: 80, Height: 10})
	followEvery = time.Millisecond
	t.Cleanup(func() { followEvery = 500 * time.Millisecond })
	find(v, "mdx")
	run(v, v.Update(ctx, press("&")))
	run(v, v.Update(ctx, press("G")))
	appendLine(t, dir, "2026-10-13.log", "2026-10-13T11:00:00.000 INFO  something else")
	appendLine(t, dir, "2026-10-13.log", "2026-10-13T11:01:00.000 WARN  mangadex: again")
	v.Update(ctx, v.tick(ctx)())
	got := view(v)
	if strings.Contains(got, "something else") || !strings.Contains(got, "mangadex: again") || !v.follow || len(v.matches) != 3 {
		t.Errorf("follow narrowed, %d matches:\n%s", len(v.matches), got)
	}
	// not narrowed: shown, a match counted
	run(v, v.Update(ctx, press("&")))
	run(v, v.Update(ctx, press("G")))
	appendLine(t, dir, "2026-10-13.log", "2026-10-13T11:02:00.000 INFO  another thing")
	appendLine(t, dir, "2026-10-13.log", "2026-10-13T11:03:00.000 INFO  mangadex: once more")
	v.Update(ctx, v.tick(ctx)())
	got = view(v)
	if !strings.Contains(got, "another thing") || !strings.Contains(got, "once more") || len(v.matches) != 4 || len(v.shown) != 8 {
		t.Errorf("follow, %d matches:\n%s", len(v.matches), got)
	}
	// what was added piecewise is what a whole filter makes
	shown, matches, starts := slices.Clone(v.shown), slices.Clone(v.matches), slices.Clone(v.starts)
	v.filter()
	if !slices.Equal(shown, v.shown) || !slices.Equal(matches, v.matches) || !slices.Equal(starts, v.starts) {
		t.Errorf("incremental %v %v %v, whole %v %v %v", shown, matches, starts, v.shown, v.matches, v.starts)
	}
}

func TestPeriodKeepsNarrow(t *testing.T) {
	dir := periodDir(t)
	v := newMain(dir, nil)
	v.Resize(base.Size{Width: 80, Height: 20})
	run(v, v.Init(ctx))
	find(v, "nt fnd")
	run(v, v.Update(ctx, press("&")))
	run(v, v.Update(ctx, press("[")))
	got := view(v)
	if v.Title().Text != "Logs · 2026-10-12" || !v.narrow || v.matcher == nil || strings.Contains(got, "started") || !strings.Contains(got, "not found") {
		t.Errorf("[ %q narrow %v:\n%s", v.Title().Text, v.narrow, got)
	}
	if !strings.Contains(v.Subtitle(), "narrowed") {
		t.Errorf("subtitle %q", v.Subtitle())
	}
}

func TestSearchFuzzy(t *testing.T) {
	marked(t)
	dir := logDir(t)
	s := newFiles(dir)
	s.Resize(base.Size{Width: 80, Height: 20})
	run(s, s.Init(ctx))
	msgs := run(s, s.Update(ctx, search.SearchMsg("mngdx")))
	v := msgs[0].(*viewer)
	v.Resize(base.Size{Width: 80, Height: 20})
	run(v, v.Init(ctx))
	got := view(v)
	// in time order, highlighted; the last one current
	rate, timeout := strings.Index(got, "rate limited"), strings.Index(got, "timeout")
	if rate < 0 || timeout < rate || strings.Contains(got, "not found") || strings.Contains(got, "started") {
		t.Errorf("search:\n%s", got)
	}
	if !strings.Contains(got, "[m]a[ng]a[d]e[x]: rate") || !strings.Contains(got, "{m}a{ng}a{d}e{x}: timeout") {
		t.Errorf("highlights:\n%s", got)
	}
	if v.Subtitle() != "2 entries in every file · match 2/2 · all lines" {
		t.Errorf("subtitle %q", v.Subtitle())
	}
	// another find in the results
	find(v, "tmout")
	run(v, v.Update(ctx, press("&")))
	if got := view(v); strings.Contains(got, "rate limited") || !strings.Contains(got, "10:30:00 ERROR") {
		t.Errorf("find in a search, narrowed:\n%s", got)
	}
}

// lines are n entries, one in 10 a match of "rate lmt".
func lines(n int) []log.Entry {
	entries := make([]log.Entry, n)
	t0 := time.Date(2026, 10, 13, 0, 0, 0, 0, time.Local)
	for i := range entries {
		msg := fmt.Sprintf("download chapter %d of manga %d: page %d done in %dms", i, i%97, i%31, i%500)
		if i%10 == 0 {
			msg = fmt.Sprintf("mangadex: rate limited, waiting %dms", i%1000)
		}
		entries[i] = log.Entry{Time: t0.Add(time.Duration(i) * time.Millisecond), Level: log.LevelInfo, Message: msg}
	}
	return entries
}

func TestManyLines(t *testing.T) {
	marked(t)
	v := newViewer(kindFile)
	v.Resize(base.Size{Width: 100, Height: 30})
	v.loaded = true
	v.setEntries(lines(5000), true)
	start := time.Now()
	v.setMatcher(newMatcher("rate lmt"))
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("find took %s", took)
	}
	if len(v.matches) != 500 || v.current != 0 {
		t.Fatalf("%d matches, current %d", len(v.matches), v.current)
	}
	if got := strings.Count(view(v), "{rate} {l}i{m}i{t}"); got != 1 {
		t.Errorf("%d current matches in\n%s", got, view(v))
	}
	if got := strings.Count(view(v), "[rate] [l]i[m]i[t]"); got < 2 {
		t.Errorf("%d other matches in\n%s", got, view(v))
	}
	run(v, v.Update(ctx, press("&")))
	if len(v.shown) != 500 {
		t.Errorf("narrowed: %d", len(v.shown))
	}
	// follow: only the new ones are matched
	v.setEntries(lines(10), false)
	if len(v.hits) != 5010 || len(v.shown) != 501 || len(v.matches) != 501 {
		t.Errorf("added: %d hits, %d shown, %d matches", len(v.hits), len(v.shown), len(v.matches))
	}
}

func BenchmarkFind(b *testing.B) {
	entries := lines(maxEntries)
	v := newViewer(kindFile)
	v.Resize(base.Size{Width: 100, Height: 30})
	v.loaded = true
	v.setEntries(entries, true)
	b.Run("fuzzy", func(b *testing.B) {
		for b.Loop() {
			v.setMatcher(newMatcher("rate lmt"))
		}
	})
	b.Run("regexp", func(b *testing.B) {
		for b.Loop() {
			v.setMatcher(newMatcher("re:rate lim"))
		}
	})
	more := lines(10)
	b.Run("follow", func(b *testing.B) {
		v.setEntries(entries[:maxEntries/2], true)
		v.setMatcher(newMatcher("rate lmt"))
		for b.Loop() {
			v.setEntries(more, false)
			if len(v.entries) >= maxEntries {
				b.StopTimer()
				v.setEntries(entries[:maxEntries/2], true)
				b.StartTimer()
			}
		}
	})
	// the first ones dropped: all is shown again
	b.Run("load and drop", func(b *testing.B) {
		v.setMatcher(newMatcher("rate lmt"))
		for b.Loop() {
			v.setEntries(entries, true)
			v.setEntries(entries[:maxEntries/10+1], false)
		}
	})
	b.Run("view", func(b *testing.B) {
		v.setMatcher(newMatcher("rate lmt"))
		for b.Loop() {
			_ = v.View()
		}
	})
}

// TestKeepTopEntry: a scrolled view keeps its top entry when the first entries are
// dropped, and when the session's entries are replaced by a later window of them.
func TestKeepTopEntry(t *testing.T) {
	marked(t)
	all := lines(maxEntries + maxEntries/10 + 20)
	v := newViewer(kindSession)
	v.Resize(base.Size{Width: 100, Height: 30})
	v.loaded = true
	v.setEntries(all[:maxEntries], true)
	v.follow = false
	v.top = v.starts[slices.Index(v.shown, 40000)]
	top := func() log.Entry { return v.entries[v.shown[v.entryAt(v.top)]] }
	want := all[40000]
	if top() != want {
		t.Fatalf("top %q", top().Message)
	}
	v.setEntries(all[maxEntries:], false)
	if v.dropped == 0 || top() != want {
		t.Errorf("dropped %d: top %q, want %q", v.dropped, top().Message, want.Message)
	}
	v.setEntries(all[7:maxEntries+7], true)
	if top() != want {
		t.Errorf("replaced: top %q, want %q", top().Message, want.Message)
	}
}
