package health

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
)

type loader struct{ info libmangal.ProviderInfo }

func (l loader) String() string                                   { return l.info.Name }
func (l loader) Info() libmangal.ProviderInfo                     { return l.info }
func (l loader) Load(context.Context) (libmangal.Provider, error) { return nil, nil }

type fakeManager struct {
	watch.Manager
	list []watch.Watch
	err  error
}

func (f fakeManager) List(context.Context) ([]watch.Watch, error) { return f.list, f.err }

func al(raw string) metadata.ID {
	return metadata.ID{Raw: raw, Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
}

var now = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

// newChecker is a library shaped like David's (provider directories, [alid-N] folders,
// adopted folders without a source) with watches that have every kind of problem.
func newChecker(t *testing.T) Checker {
	t.Helper()
	data, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	lib := storage.Library{DB: data}
	record := func(parent, dir, provider, mangaID, title string, numbers ...float32) {
		for _, n := range numbers {
			dl := storage.Download{Path: filepath.Join(parent, dir, fmt.Sprintf("%g.cbz", n)), Provider: provider, MangaID: mangaID,
				MangaTitle: title, Chapter: n, Format: "CBZ"}
			if err := lib.RecordDownload(parent, dir, dl); err != nil {
				t.Fatal(err)
			}
		}
	}
	adopt := func(parent, dir, title string, ids ...metadata.ID) {
		if err := lib.RememberMangaDir(parent, dir, libmangal.MangaDirInfo{MetadataIDs: ids}); err != nil {
			t.Fatal(err)
		}
		if _, err := data.Exec(`UPDATE library_mangas SET title = ? WHERE dir = ?`, title, dir); err != nil {
			t.Fatal(err)
		}
		record(parent, dir, "", "", title, 1)
	}
	bind := func(provider, mangaID, id string) {
		if err := (storage.Bindings{DB: data}).Set(storage.Binding{Provider: provider, MangaID: mangaID, Source: metadata.IDCodeAnilist, ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	md := "/dl/mango-mangadex"
	// 1: watched, gaps 3, 4 and 6 (6 skipped by the watch), a .5 that isn't a gap
	record(md, "One-Punch Man (2012) [alid-74347]", "mango-mangadex", "opm", "One-Punch Man", 1, 2, 5, 5.5, 7)
	bind("mango-mangadex", "opm", "74347")
	// 2: the same AniList id, adopted
	adopt(md, "Onepunch-Man [alid-74347]", "Onepunch-Man", al("74347"))
	// 3: bound to another id than its folder tag, watched on two providers
	record(md, "Berserk (1989) [alid-30002]", "mango-mangadex", "berserk", "Berserk", 1, 2)
	bind("mango-mangadex", "berserk", "21")
	// a Kitsu id to pick (the backups found two)
	if _, err := lib.SetLinkPick(storage.LinkPick{Manga: 3, Source: metadata.IDCodeKitsu, AniList: "21", Found: now,
		Candidates: []storage.LinkCandidate{{ID: "30", From: "mango-mangadex"}, {ID: "31", From: storage.FromKitsu}}}); err != nil {
		t.Fatal(err)
	}
	// 4: its provider is gone, no AniList link
	record("/dl/mango-old", "Vagabond", "mango-old", "vag", "Vagabond", 1)
	// 5: in another provider's directory than its watch's
	adopt("/dl/AsuraScans", "Dungeon Odyssey [alid-555]", "Dungeon Odyssey", al("555"))

	if err := lib.SetStatuses(map[int64]string{2: string(metadata.StatusFinished), 4: string(metadata.StatusHiatus)}, now); err != nil {
		t.Fatal(err)
	}
	f32 := func(n float32) *float32 { return &n }
	watches := []watch.Watch{
		{Watch: storage.Watch{ID: 1, Provider: "mango-mangadex", MangaID: "opm", Title: "One-Punch Man", LibraryManga: 1, Enabled: true,
			Skip: []float32{6}, Failures: 3, LastError: "boom", Added: now.Add(-24 * time.Hour)}},
		{Watch: storage.Watch{ID: 2, Provider: "mango-weebcentral", MangaID: "bk", Title: "Berserk", LibraryManga: 3, Enabled: true,
			Retry: []float32{3}, Added: now}},
		{Watch: storage.Watch{ID: 3, Provider: "mango-mangadex", MangaID: "berserk", Title: "Berserk", Enabled: true, Added: now}},
		{Watch: storage.Watch{ID: 4, Provider: "mango-mangadex", MangaID: "doro", Title: "Dorohedoro", NeedsRelink: true,
			RelinkReason: "moved from mango-mangasee", Added: now}},
		{Watch: storage.Watch{ID: 5, Provider: "mango-gone", MangaID: "k", Title: "Kingdom", Enabled: true, Added: now}, ProviderMissing: true},
		{Watch: storage.Watch{ID: 6, Provider: "mango-mangasee", MangaID: "blame", Title: "Blame", Enabled: true, Schedule: "bogus",
			Added: now.Add(-200 * 24 * time.Hour), From: f32(10)}},
		{Watch: storage.Watch{ID: 7, Provider: "mango-mangadex", MangaID: "dg", Title: "Dungeon Odyssey", Enabled: true, Added: now},
			Bindings:     []storage.Binding{{Source: metadata.IDCodeAnilist, ID: "555"}},
			LibraryIssue: "in the library under AsuraScans (Dungeon Odyssey): move it there (X in the library)", LibraryElsewhere: true},
	}
	return Checker{
		Library: lib,
		Watches: fakeManager{list: watches},
		Loaders: []libmangal.ProviderLoader{
			loader{libmangal.ProviderInfo{ID: "mango-mangadex", Name: "MangaDex"}},
			loader{libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central", History: []libmangal.ProviderAlias{{ID: "mango-mangasee", Name: "MangaSee"}}}},
		},
		OldIDs: func() ([]storage.OldID, error) {
			return []storage.OldID{{Old: "mango-mangasee", New: "mango-weebcentral", Name: "Weeb Central", Watches: 1}}, nil
		},
		Accounts: func() (Accounts, error) {
			return Accounts{
				Notices: []string{"old cache converted"},
				Logins:  []Login{{Service: "Kitsu", Message: "the login of \"x\" expired", Command: "mangal kitsu"}},
				Skipped: []error{errors.New("provider /p/broken: bad bundle")},
				Notify:  watch.NotifyBoth,
			}, nil
		},
		Root:         "/dl",
		ProviderDirs: true,
		Backoff:      3,
		Now:          func() time.Time { return now },
	}
}

type want struct {
	check    string
	severity Severity
	kind     string
	id       int64
	// command is one of its fix's commands, message part of its message ("" for any)
	command, message string
}

func find(report Report, w want) (Finding, bool) {
	for _, f := range report.Findings {
		if f.Check == w.check && f.Subject.Kind == w.kind && f.Subject.ID == w.id && strings.Contains(f.Message, w.message) {
			return f, true
		}
	}
	return Finding{}, false
}

func TestRun(t *testing.T) {
	report, err := newChecker(t).Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) != 0 {
		t.Errorf("errors: %v", report.Errors)
	}
	wants := []want{
		{"W1", Error, "watch", 4, "mangal watch move 4", "moved from mango-mangasee"},
		{"W2", Error, "watch", 7, "mangal library move 5 mango-mangadex mid:dg", "not in library"},
		{"W3", Info, "watch", 4, "mangal watch resume 4", "paused"},
		{"W4", Error, "watch", 1, "mangal watch check 1", "backing off"},
		{"W5", Info, "watch", 6, "mangal watch pause 6", "200 days"},
		{"W6", Error, "watch", 5, "mangal watch move 5 <provider>", "mango-gone"},
		{"W8", Warning, "watch", 6, "mangal migrate", "now mango-weebcentral"},
		{"W9", Warning, "watch", 6, "mangal watch set 6 --schedule ''", "bogus"},
		{"W10", Info, "library", 0, "mangal health --only W10", "3 mangas aren't watched: 1 ongoing/unknown, 1 on hiatus, 1 finished"},
		{"W11", Warning, "manga", 3, "mangal watch pause 2", "2 providers"},
		{"G1", Warning, "manga", 1, "mangal watch check 1 --gaps", "2 chapters missing: 3-4"},
		{"G3", Warning, "watch", 2, "mangal watch check 2", "3"},
		{"M1", Warning, "manga", 4, "mangal library scan --check", "no AniList link"},
		{"M3", Warning, "manga", 3, "mangal library bind 3 --rename", "AniList links disagree: 21"},
		{"M3", Warning, "manga", 3, "mangal library bind 3 --kitsu 31 --apply", "Kitsu: pick between 30 (mango-mangadex) and 31 (Kitsu mapping)"},
		{"M6", Warning, "manga", 1, "mangal library merge 1 2", "AniList 74347 is in 2 folders, keep 1"},
		{"L3", Warning, "provider", 0, "mangal migrate", "mango-mangasee"},
		{"L7", Warning, "manga", 4, "mangal library move 4 <provider>", "mango-old"},
		{"A1", Info, "account", 0, "mangal kitsu", "expired"},
	}
	for _, w := range wants {
		f, ok := find(report, w)
		switch {
		case !ok:
			t.Errorf("%s on %s %d: missing", w.check, w.kind, w.id)
		case f.Severity != w.severity:
			t.Errorf("%s on %s %d: severity %s, want %s", w.check, w.kind, w.id, f.Severity, w.severity)
		case w.command != "" && !slices.Contains(f.Fix.Commands, w.command):
			t.Errorf("%s on %s %d: commands %q, want %q", w.check, w.kind, w.id, f.Fix.Commands, w.command)
		case !strings.Contains(f.Message, w.message):
			t.Errorf("%s on %s %d: message %q, want %q in it", w.check, w.kind, w.id, f.Message, w.message)
		}
	}
	// not problems: the linked manga isn't unwatched, OPM's folder tag agrees, the watch
	// with a .5 has no gap at 5.5, a watched manga with a from has no gap below it
	for _, w := range []want{{check: "W10", kind: "manga", id: 1}, {check: "M3", kind: "manga", id: 1}, {check: "M1", kind: "manga", id: 2},
		{check: "W10", kind: "manga", id: 3}, {check: "G1", kind: "manga", id: 3}} {
		if f, ok := find(report, w); ok {
			t.Errorf("unexpected %+v", f)
		}
	}
	actions := map[string]string{}
	for _, f := range report.Findings {
		if a := f.Fix.Action; a != nil {
			actions[fmt.Sprintf("%s %s %d", f.Check, f.Subject.Kind, f.Subject.ID)+" "+a.Kind] = fmt.Sprintln(a.Watch, a.Source, a.IDs, a.Mangas)
		}
	}
	for k, v := range map[string]string{
		"W3 watch 4 resume": "4  [] []\n",
		"M6 manga 1 merge":  "0  [] [1 2]\n",
		"M3 manga 3 pick":   "0 kt [30 31] []\n",
		"M3 manga 3 link":   "0 al [21 30002] []\n",
	} {
		if actions[k] != v {
			t.Errorf("action %s: %q, want %q (all %v)", k, actions[k], v, actions)
		}
	}
	w10, _ := find(report, want{check: "W10", kind: "library"})
	var items []string
	for _, i := range w10.Items {
		items = append(items, fmt.Sprintf("%d %s %s", i.Subject.ID, i.Message, i.Fix.Commands))
	}
	if want := []string{
		"5 status unknown, not watched [mangal watch add <provider> 'Dungeon Odyssey']",
		"4 on hiatus, not watched [mangal watch add <provider> Vagabond]",
		"2 finished, not watched (fine) [mangal watch add <provider> Onepunch-Man]",
	}; !slices.Equal(items, want) {
		t.Errorf("W10 items %q, want %q", items, want)
	}
	var a1 []string
	for _, f := range report.Findings {
		if f.Check == "A1" {
			a1 = append(a1, f.Subject.Name)
		}
	}
	if !slices.Equal(a1, []string{"Discord", "Lua providers", "Kitsu", "storage"}) {
		t.Errorf("A1 %v", a1)
	}
	// errors first, by category
	if f := report.Findings[0]; f.Category != Watches || f.Severity != Error {
		t.Errorf("first finding %+v", f)
	}
	if report.Counts.Total() != len(report.Findings) || report.Counts.Error != 4 || report.Counts.ByCategory[Gaps] != 2 {
		t.Errorf("counts %+v", report.Counts)
	}
}

func TestRunNetwork(t *testing.T) {
	c := newChecker(t)
	lib := c.Library
	for _, dl := range []storage.Download{
		{Path: "/dl/mango-mangadex/Kagurabachi/1.cbz", Provider: "mango-mangadex", MangaID: "kagura", MangaTitle: "Kagurabachi", Chapter: 1, Format: "CBZ"},
		{Path: "/dl/mango-mangadex/Kagurabachi/2.cbz", Provider: "mango-mangadex", MangaID: "kagura", MangaTitle: "Kagurabachi", Chapter: 2, Format: "CBZ"},
		{Path: "/dl/mango-mangadex/Gone/1.cbz", Provider: "mango-mangadex", MangaID: "gone", MangaTitle: "Gone", Chapter: 1, Format: "CBZ"},
		{Path: "/dl/mango-mangadex/Done/1.cbz", Provider: "mango-mangadex", MangaID: "done", MangaTitle: "Done", Chapter: 1, Format: "CBZ"},
	} {
		if err := lib.RecordDownload("/dl/mango-mangadex", filepath.Base(filepath.Dir(dl.Path)), dl); err != nil {
			t.Fatal(err)
		}
	}
	kagura, _, _ := lib.MangaBySource("mango-mangadex", "kagura")
	done, _, _ := lib.MangaBySource("mango-mangadex", "done")
	_ = lib.SetStatuses(map[int64]string{kagura.ID: string(metadata.StatusReleasing), done.ID: string(metadata.StatusFinished)}, now)
	var asked []string
	var fetched bool
	c.Statuses = func(_ context.Context, _ []storage.Manga, _ []storage.Binding, fetch bool) error {
		fetched = fetch
		return nil
	}
	c.NewChapters = func(_ context.Context, provider string, mangas []ProviderManga, _ func(string), done func(int, []float32, error)) error {
		for i, m := range mangas {
			asked = append(asked, fmt.Sprintf("%s %s %v", provider, m.MangaID, m.Have))
			switch m.MangaID {
			case "kagura":
				done(i, []float32{3, 4, 5}, nil)
			case "gone":
				done(i, nil, fmt.Errorf("manga %q: %w", m.MangaID, watch.ErrMangaNotFound))
			default:
				done(i, nil, nil)
			}
		}
		return nil
	}
	if _, err := c.Run(context.Background(), Options{Only: []string{"W12"}}); err == nil || !strings.Contains(err.Error(), "--network") {
		t.Errorf("W12 without --network: %v", err)
	}
	report, err := c.Run(context.Background(), Options{Network: true, Only: []string{"watches"}})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(asked)
	// not the finished or hiatus ones, nor those without an installed source
	if want := []string{"mango-mangadex gone [1]", "mango-mangadex kagura [1 2]"}; !slices.Equal(asked, want) || !fetched || !report.Network {
		t.Errorf("asked %q, want %q (statuses fetched %v)", asked, want, fetched)
	}
	if _, ok := find(report, want{check: "W12", kind: "manga", id: kagura.ID, message: "3 new chapters on mango-mangadex (up to 5): watch it?"}); !ok {
		t.Errorf("no W12 for Kagurabachi: %+v", report.Findings)
	}
	gone, _, _ := lib.MangaBySource("mango-mangadex", "gone")
	if f, ok := find(report, want{check: "W12", kind: "manga", id: gone.ID, message: "not found on mango-mangadex"}); !ok || f.Fix.Commands[0] != fmt.Sprintf("mangal library move %d mango-mangadex", gone.ID) {
		t.Errorf("no W12 for Gone: %+v", f)
	}
	// offline: no W12, nothing asked
	asked = nil
	report, _ = newChecker(t).Run(context.Background(), Options{})
	if slices.ContainsFunc(report.Findings, func(f Finding) bool { return f.Check == "W12" }) || len(asked) != 0 {
		t.Errorf("offline run asked %v", asked)
	}
}

func TestRunOnly(t *testing.T) {
	c := newChecker(t)
	report, err := c.Run(context.Background(), Options{Only: []string{"gaps", "m6"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Category != Gaps && f.Check != "M6" {
			t.Errorf("not asked for: %+v", f)
		}
	}
	if report.Counts.Total() != 3 {
		t.Errorf("got %d findings, want G1, G3, M6", report.Counts.Total())
	}
	if _, err := c.Run(context.Background(), Options{Only: []string{"nope"}}); err == nil || !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("unknown name: %v", err)
	}
}

// a part that doesn't load skips only the checks that need it
func TestRunFailedPart(t *testing.T) {
	c := newChecker(t)
	c.Watches = fakeManager{err: errors.New("server down")}
	report, err := c.Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]bool{}
	for _, f := range report.Findings {
		checks[f.Check] = true
	}
	for _, id := range []string{"W1", "W10", "G1", "G3"} {
		if checks[id] {
			t.Errorf("%s ran without the watches", id)
		}
	}
	for _, id := range []string{"M1", "M3", "M6", "L3", "L7", "A1"} {
		if !checks[id] {
			t.Errorf("%s didn't run", id)
		}
	}
	if len(report.Errors) == 0 || !strings.Contains(report.Errors[0], "server down") || !slices.ContainsFunc(report.Errors, func(e string) bool { return strings.HasPrefix(e, "W1 skipped") }) {
		t.Errorf("errors %q", report.Errors)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Run(ctx, Options{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestMissingNumbers(t *testing.T) {
	for _, tt := range []struct {
		have  []float32
		from  float32
		skip  []float32
		want  []int
		label string
	}{
		{[]float32{1, 2, 3}, -1, nil, nil, "none"},
		{[]float32{1, 4}, -1, nil, []int{2, 3}, "between"},
		{[]float32{0.5, 2.5}, -1, nil, []int{1, 2}, "from halves"},
		{[]float32{1, 1.5, 3}, -1, nil, []int{2}, "a .5 isn't one"},
		{[]float32{1, 6}, 3, []float32{5}, []int{4}, "from and skip"},
	} {
		if got := missingNumbers(tt.have, tt.from, tt.skip); !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.label, got, tt.want)
		}
	}
	if got := formatRanges([]int{2, 3, 4, 7, 9, 10}); got != "2-4, 7, 9-10" {
		t.Errorf("ranges %q", got)
	}
}
