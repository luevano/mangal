package health

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
)

func setLinks(t *testing.T, c Checker, manga int64, links map[metadata.IDCode]string) {
	t.Helper()
	if err := c.Library.SetLinks(storage.LinkChange{Manga: manga, Links: links, At: now}); err != nil {
		t.Fatal(err)
	}
}

func TestProviderFailing(t *testing.T) {
	c := newChecker(t)
	w := func(id int64, provider string, failures int, enabled bool) watch.Watch {
		return watch.Watch{ID: id, Provider: provider, MangaID: fmt.Sprint("m", id), Title: fmt.Sprint("Manga ", id),
			Enabled: enabled, Failures: failures, LastError: "site down", Added: now}
	}
	c.Watches = fakeManager{list: []watch.Watch{
		// every enabled one failed (the paused one doesn't count), one backing off
		w(1, "mango-mangadex", 1, true), w(2, "mango-mangadex", 4, true), w(3, "mango-mangadex", 0, false),
		// one of them still works
		w(4, "mango-weebcentral", 2, true), w(5, "mango-weebcentral", 0, true),
		// only one watch: W4's
		w(6, "mango-other", 5, true),
	}}
	report, err := c.Run(context.Background(), Options{Only: []string{"W7"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings %+v", report.Findings)
	}
	f := report.Findings[0]
	if f.Subject.Name != "mango-mangadex" || f.Severity != Warning || f.Message != "all 2 of its watches failed their last checks" || f.Key != "W7/provider/mango-mangadex" ||
		!slices.Equal(f.Details, []string{"watch 1 Manga 1: 1 failed check: site down", "watch 2 Manga 2: 4 failed checks: site down"}) ||
		!slices.Contains(f.Fix.Commands, "mangal watch log --provider mango-mangadex") || !slices.Contains(f.Fix.Commands, "mangal health --network --only W7,A2") {
		t.Errorf("W7 %+v", f)
	}

	// all backing off: an error; the probe says it answers now
	c.Watches = fakeManager{list: []watch.Watch{w(1, "mango-mangadex", 3, true), w(2, "mango-mangadex", 4, true)}}
	c.Probe = func(context.Context, string, ProviderManga) Probe { return Probe{Took: time.Second} }
	report, err = c.Run(context.Background(), Options{Only: []string{"W7"}, Network: true})
	if err != nil || len(report.Findings) != 1 {
		t.Fatalf("network %+v %v", report, err)
	}
	f = report.Findings[0]
	if f.Severity != Error || !strings.HasSuffix(f.Message, "(backing off)") || !strings.HasPrefix(f.Details[len(f.Details)-1], "it answers now (A2)") ||
		slices.Contains(f.Fix.Commands, "mangal health --network --only W7,A2") {
		t.Errorf("W7 network %+v", f)
	}
	c.Probe = func(context.Context, string, ProviderManga) Probe { return Probe{Err: errors.New("site down")} }
	report, _ = c.Run(context.Background(), Options{Only: []string{"W7"}, Network: true})
	if f := report.Findings[0]; f.Details[len(f.Details)-1] != "it doesn't answer now either (A2): site down" {
		t.Errorf("W7 down %+v", f)
	}
}

// fakeBackups are the backups of the mangas asked: Kitsu and MyAnimeList ids from the
// manga id, a Kitsu id that differs for manga 2.
func fakeBackups(asked *[]int64) func(context.Context, []storage.Manga) ([]library.Backup, error) {
	return func(_ context.Context, mangas []storage.Manga) ([]library.Backup, error) {
		var out []library.Backup
		for _, m := range mangas {
			*asked = append(*asked, m.ID)
			b := library.Backup{Manga: m, AniList: "1", Fill: library.Links{}}
			if m.ID == 2 {
				b.Conflicts = []library.LinkChange{{Code: metadata.IDCodeKitsu, Old: []string{"5"}, New: "50"}}
			} else {
				b.Fill[metadata.IDCodeKitsu] = fmt.Sprint("k", m.ID)
			}
			b.Fill[metadata.IDCodeMyAnimeList] = fmt.Sprint("m", m.ID)
			out = append(out, b)
		}
		return out, nil
	}
}

func TestBackups(t *testing.T) {
	c := newChecker(t)
	var asked []int64
	c.Backups = fakeBackups(&asked)
	c.IDCheck = func(context.Context, []storage.Manga) ([]library.IDCheck, error) { return nil, nil }
	if _, err := c.Run(context.Background(), Options{Only: []string{"M2"}}); err == nil {
		t.Error("M2 without --network")
	}
	report, err := c.Run(context.Background(), Options{Only: []string{"M2"}, Network: true})
	if err != nil || len(report.Errors) != 0 {
		t.Fatalf("%v %v", err, report.Errors)
	}
	// the mangas linked to AniList
	slices.Sort(asked)
	if !slices.Equal(asked, []int64{1, 2, 3, 5, 6}) {
		t.Errorf("asked %v", asked)
	}
	sum, ok := find(report, want{check: "M2", kind: "library"})
	if !ok || sum.Severity != Info || sum.Message != "4 mangas linked to AniList can get backup ids" || len(sum.Fix.Commands) != 0 ||
		sum.Fix.Action == nil || sum.Fix.Action.Kind != ActionBackups || !slices.Equal(sum.Fix.Action.Mangas, []int64{6, 3, 5, 1}) {
		t.Fatalf("M2 %+v", sum)
	}
	// MyAnimeList ids aren't suggested unless it's enabled
	if i := sum.Items[3]; i.Message != "can get Kitsu k1" || i.Key != "M2/manga/1" || i.Fix.Commands[0] != "mangal library bind 1 --kitsu k1" ||
		fmt.Sprint(i.Fix.Action.Links) != "map[kt:k1]" || i.Fix.Action.Manga != 1 {
		t.Errorf("item %+v", i)
	}

	c.MyAnimeList = true
	report, _ = c.Run(context.Background(), Options{Only: []string{"M2"}, Network: true})
	sum, _ = find(report, want{check: "M2", kind: "library"})
	if len(sum.Items) != 5 || sum.Items[3].Message != "can get Kitsu k1 and MyAnimeList m1" || sum.Items[4].Message != "can get MyAnimeList m2" ||
		sum.Fix.Commands[0] != "mangal library bind --backups" {
		t.Fatalf("M2 with MyAnimeList %+v", sum)
	}

	// offline: kept while the mangas still have no id of them
	setLinks(t, c, 1, map[metadata.IDCode]string{metadata.IDCodeKitsu: "8"})
	setLinks(t, c, 3, map[metadata.IDCode]string{metadata.IDCodeKitsu: "9", metadata.IDCodeMyAnimeList: "3"})
	offline, err := c.Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	kept, ok := find(KeepNetwork(report, offline), want{check: "M2", kind: "library"})
	var items []string
	for _, i := range kept.Items {
		items = append(items, fmt.Sprint(i.Subject.ID, " ", i.Message))
	}
	if !ok || kept.Message != "4 mangas linked to AniList can get backup ids" || !slices.Equal(kept.Fix.Action.Mangas, []int64{6, 5, 1, 2}) ||
		!slices.Equal(items, []string{"6 can get Kitsu k6 and MyAnimeList m6", "5 can get Kitsu k5 and MyAnimeList m5", "1 can get MyAnimeList m1", "2 can get MyAnimeList m2"}) {
		t.Errorf("kept %+v", kept)
	}

	// a dismissed item leaves the summary
	c.Ignores = func() ([]storage.HealthIgnore, error) {
		return []storage.HealthIgnore{{Check: "M2", Subject: "manga/5"}}, nil
	}
	report, _ = c.Run(context.Background(), Options{Only: []string{"M2"}, Network: true})
	if sum, _ := find(report, want{check: "M2", kind: "library"}); slices.ContainsFunc(sum.Items, func(f Finding) bool { return f.Subject.ID == 5 }) {
		t.Errorf("dismissed item kept %+v", sum.Items)
	}
}

func TestAdoptedIDs(t *testing.T) {
	c, asked := networkChecker(t)
	var backups []int64
	c.Backups = fakeBackups(&backups)
	// manga 2 has a Kitsu id
	setLinks(t, c, 2, map[metadata.IDCode]string{metadata.IDCodeKitsu: "5"})
	kt := func(raw string) metadata.ID {
		return metadata.ID{Raw: raw, Source: metadata.IDSourceKitsu, Code: metadata.IDCodeKitsu}
	}
	c.IDCheck = func(_ context.Context, mangas []storage.Manga) ([]library.IDCheck, error) {
		byID := map[int64]storage.Manga{}
		for _, m := range mangas {
			byID[m.ID] = m
		}
		return []library.IDCheck{
			{Manga: byID[5], Issue: library.IDMismatch, ID: al("555"), Metadata: titled("Other"), Score: 0.2, Provider: "AniList"},
			{Manga: byID[6], Issue: library.IDNotFound, ID: al("136899"), Provider: "AniList"},
			{Manga: byID[2], Issue: library.IDNotFound, ID: kt("5"), Provider: "Kitsu"},
			// not its id any more: M3's
			{Manga: byID[3], Issue: library.IDNotFound, ID: al("999"), Provider: "AniList"},
		}, nil
	}
	search := c.SearchAnilist
	c.SearchAnilist = func(ctx context.Context, queries []metadata.Query) ([]metadata.Match, []bool, error) {
		matches, found, err := search(ctx, queries)
		for i, q := range queries {
			if q.Titles[0] == "Dungeon Odyssey" {
				matches[i] = metadata.Match{Metadata: idMeta{id: "777", title: "Dungeon Odyssey"}, Score: 1}
			}
		}
		return matches, found, err
	}
	report, err := c.Run(context.Background(), Options{Only: []string{"M5"}, Network: true})
	if err != nil || len(report.Errors) != 0 {
		t.Fatalf("%v %v", err, report.Errors)
	}
	if len(report.Findings) != 3 {
		t.Errorf("findings %+v", report.Findings)
	}
	// only M5's mangas are searched, only the Kitsu one's backups looked up
	if slices.Contains(*asked, "anilist Vagabond") || !slices.Contains(*asked, "anilist Dungeon Odyssey") || !slices.Equal(backups, []int64{2}) {
		t.Errorf("asked %q, backups of %v", *asked, backups)
	}
	do, ok := find(report, want{check: "M5", kind: "manga", id: 5})
	if !ok || do.Message != `AniList 555 is "Other", not like its title (score 0.20)` || do.Key != "M5/manga/5/al/555" || do.Fix.Action == nil ||
		do.Fix.Action.Kind != ActionLink || !slices.Equal(do.Fix.Action.IDs, []string{"777"}) ||
		!slices.Equal(do.Fix.Commands, []string{"mangal library bind 5 --anilist 777", "mangal library bind 5 --remove anilist"}) {
		t.Errorf("M5 5 %+v", do)
	}
	yomi, ok := find(report, want{check: "M5", kind: "manga", id: 6})
	if !ok || yomi.Message != "AniList 136899 isn't on AniList" || yomi.Fix.Action != nil || yomi.Fix.Commands[0] != "mangal library bind 6 --anilist <id>" {
		t.Errorf("M5 6 %+v", yomi)
	}
	opm, ok := find(report, want{check: "M5", kind: "manga", id: 2})
	if !ok || opm.Message != "Kitsu 5 isn't on Kitsu" || opm.Fix.Action == nil || opm.Fix.Action.Source != metadata.IDCodeKitsu ||
		!slices.Equal(opm.Fix.Action.IDs, []string{"50"}) || !strings.Contains(strings.Join(opm.Details, "\n"), "AniList 1 gives Kitsu 50") {
		t.Errorf("M5 2 %+v", opm)
	}

	// offline: kept while the manga has the id
	setLinks(t, c, 5, map[metadata.IDCode]string{metadata.IDCodeAnilist: "777"})
	offline, _ := c.Run(context.Background(), Options{})
	kept := KeepNetwork(report, offline)
	if _, ok := find(kept, want{check: "M5", kind: "manga", id: 5}); ok {
		t.Error("M5 kept for a manga linked anew")
	}
	if _, ok := find(kept, want{check: "M5", kind: "manga", id: 6}); !ok {
		t.Error("M5 not kept")
	}
}

func TestNoAnilistFromMAL(t *testing.T) {
	c, asked := networkChecker(t)
	setLinks(t, c, 4, map[metadata.IDCode]string{metadata.IDCodeMyAnimeList: "656"})
	var malAsked []int
	c.AnilistFromMAL = func(_ context.Context, ids []int) (map[int]int, error) {
		malAsked = append(malAsked, ids...)
		return map[int]int{656: 6560}, nil
	}
	report, err := c.Run(context.Background(), Options{Only: []string{"M1"}, Network: true})
	if err != nil || len(report.Errors) != 0 {
		t.Fatalf("%v %v", err, report.Errors)
	}
	vag, ok := find(report, want{check: "M1", kind: "manga", id: 4})
	if !ok || vag.Severity != Info || vag.Fix.Action == nil || !slices.Equal(vag.Fix.Action.IDs, []string{"6560"}) || !vag.Network ||
		vag.Details[0] != "AniList 6560 has its MyAnimeList id 656" {
		t.Errorf("M1 %+v", vag)
	}
	if !slices.Equal(malAsked, []int{656}) || slices.Contains(*asked, "anilist Vagabond") {
		t.Errorf("asked MyAnimeList %v, AniList %q", malAsked, *asked)
	}
}
