package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

func alID(raw string) metadata.ID {
	return metadata.ID{Raw: raw, Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
}

func testMeta(title, al, mal string) metadata.Metadata {
	m := &mangadata.Metadata{EnglishTitle: title, AuthorList: []string{"ONE"}, DateStart: metadata.Date{Year: 2012, Month: 1, Day: 1},
		PublicationStatus: metadata.StatusReleasing, ProviderID: al, ProviderIDCode: string(metadata.IDCodeAnilist)}
	if mal != "" {
		m.OtherIDs = []metadata.ID{{Raw: mal, Source: metadata.IDSourceMyAnimeList, Code: metadata.IDCodeMyAnimeList}}
	}
	return m
}

func newLinker(t *testing.T, root string, metas map[string]metadata.Metadata) Linker {
	t.Helper()
	return Linker{
		Files: Files{FS: afero.NewOsFs(), Library: newLibrary(t), Root: root, Modes: DefaultModes},
		MangaName: func(_ libmangal.ProviderInfo, m mangadata.Manga) string {
			if metadata.Validate(m.Metadata()) == nil {
				return m.Metadata().String()
			}
			return m.Info().Title
		},
		Metadata: func(_ context.Context, ids []metadata.ID) (metadata.Metadata, error) {
			for _, id := range ids {
				if m, ok := metas[id.Raw]; ok && id.Code == metadata.IDCodeAnilist {
					return m, nil
				}
			}
			return nil, nil
		},
	}
}

func TestLinks(t *testing.T) {
	root := t.TempDir()
	dex := filepath.Join(root, "mango-mangadex")
	l := newLinker(t, root, map[string]metadata.Metadata{"74347": testMeta("One-Punch Man", "74347", "44347")})
	lib := l.Library
	ctx := context.Background()

	// linked to a wrong AniList id (1) everywhere, the folder tag too
	folder := filepath.Join(dex, "OPM (2012) [alid-1]")
	touch(t, filepath.Join(folder, "1.cbz"))
	writeMarker(t, folder, libmangal.MangaDirInfo{Provider: "mango-mangadex", Manga: mangadata.MangaInfo{ID: "berserk", Title: "OPM"},
		MetadataIDs: []metadata.ID{alID("1"), {Raw: "9", Source: metadata.IDSourceMyAnimeList, Code: metadata.IDCodeMyAnimeList}}})
	record(t, lib, dex, filepath.Base(folder), "1.cbz", 1, "mango-mangadex")
	if err := lib.RememberMangaDir(dex, filepath.Base(folder), libmangal.MangaDirInfo{MetadataIDs: []metadata.ID{alID("1")}}); err != nil {
		t.Fatal(err)
	}
	bindings, watches := storage.Bindings{DB: lib.DB}, storage.Watches{DB: lib.DB}
	_ = bindings.Set(storage.Binding{Provider: "mango-mangadex", MangaID: "berserk", Source: "al", ID: "1"})
	manga, _, _ := lib.MangaByFolder(dex, filepath.Base(folder))
	// a watch on another provider downloading into it, bound to the old id
	w, _ := watches.Add(storage.Watch{Provider: "mango-weebcentral", MangaID: "w1", Title: "OPM", LibraryManga: manga.ID})
	_ = bindings.Set(storage.Binding{Provider: "mango-weebcentral", MangaID: "w1", Source: "al", ID: "1"})
	// and an adopted folder with the new id
	if err := lib.RememberMangaDir(filepath.Join(root, "Weeb Central"), "One-Punch Man", libmangal.MangaDirInfo{Manga: mangadata.MangaInfo{Title: "One-Punch Man"}, MetadataIDs: []metadata.ID{alID("74347")}}); err != nil {
		t.Fatal(err)
	}

	info, err := l.LinkInfo(manga)
	if err != nil {
		t.Fatal(err)
	}
	if s := info.Source(metadata.IDCodeAnilist); s.Current() != "1" || s.Tag != "1" || s.Marker != "1" || s.Suggested() != "" {
		t.Errorf("AniList %+v", s)
	}

	plan, err := l.PlanLinks(ctx, manga, Links{metadata.IDCodeAnilist: "74347"}, PlanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].New != "74347" || plan.Marker == nil {
		t.Fatalf("plan %+v", plan)
	}
	if plan.Rename == nil || plan.Rename.To != filepath.Join(dex, "One-Punch Man (2012) [alid-74347]") || plan.Rename.Skip != "" {
		t.Errorf("rename %+v", plan.Rename)
	}
	if len(plan.Watches) != 1 || plan.Watches[0].Watch.ID != w.ID || plan.Watches[0].Has != "1" {
		t.Errorf("watches %+v", plan.Watches)
	}
	if len(plan.Others) != 1 || plan.Others[0].Manga.Title != "One-Punch Man" {
		t.Errorf("others %+v", plan.Others)
	}
	text := strings.Join(append(plan.Lines(), plan.ImpliedLines(map[string]string{"rename": "--rename", "watches": "--watches"})...), "\n")
	for _, want := range []string{
		"AniList: 1 -> 74347",
		"in: bindings of mango-mangadex berserk, library, .libmangal.json",
		"  from mango-mangadex/OPM (2012) [alid-1]\n  to   mango-mangadex/One-Punch Man (2012) [alid-74347]\n  --rename renames it",
		"watch 1 (mango-weebcentral w1) is bound to AniList 1, not 74347: --watches updates it",
		`has AniList 74347 too, merge them: mangal library merge 1 2`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("lines miss %q:\n%s", want, text)
		}
	}

	// without the implied changes: the folder and the watch stay
	if err := l.ApplyLinks(plan, LinkOptions{}); err != nil {
		t.Fatal(err)
	}
	marker, err := libmangal.ReadMangaDirInfo(l.FS, folder)
	if err != nil || len(marker.MetadataIDs) != 2 || marker.MetadataIDs[0].Raw != "74347" || marker.MetadataIDs[1].Raw != "9" {
		t.Errorf("marker %+v, %v", marker.MetadataIDs, err)
	}
	if b, _, _ := bindings.Default("mango-mangadex", "berserk"); b.ID != "74347" {
		t.Errorf("binding %+v", b)
	}
	if b, _, _ := bindings.Default("mango-weebcentral", "w1"); b.ID != "1" {
		t.Errorf("the watch's binding changed: %+v", b)
	}
	manga, _, _ = lib.MangaByID(manga.ID)
	if len(manga.MetadataIDs) != 1 || manga.MetadataIDs[0].Raw != "74347" {
		t.Errorf("ids %+v", manga.MetadataIDs)
	}

	// again: nothing changes, the rename and the watch are still to do
	plan, err = l.PlanLinks(ctx, manga, Links{metadata.IDCodeAnilist: "74347"}, PlanOptions{})
	if err != nil || len(plan.Changes) != 0 || plan.Marker != nil || plan.Rename == nil || len(plan.Watches) != 1 {
		t.Fatalf("again: %+v, %v", plan, err)
	}
	if err := l.ApplyLinks(plan, LinkOptions{Rename: true, Watches: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plan.Rename.To); err != nil {
		t.Errorf("not renamed: %v", err)
	}
	if b, _, _ := bindings.Default("mango-weebcentral", "w1"); b.ID != "74347" {
		t.Errorf("the watch's binding: %+v", b)
	}
	if m, ok, _ := lib.MangaByFolder(dex, "One-Punch Man (2012) [alid-74347]"); !ok || m.ID != manga.ID {
		t.Errorf("library folder: %+v", m)
	}
}

func TestBackups(t *testing.T) {
	root := t.TempDir()
	l := newLinker(t, root, nil)
	lib := l.Library
	for _, dir := range []string{"OPM", "Berserk", "Alone"} {
		if err := lib.RememberMangaDir(root, dir, libmangal.MangaDirInfo{Provider: "mango-mangadex", Manga: mangadata.MangaInfo{ID: dir, Title: dir}}); err != nil {
			t.Fatal(err)
		}
	}
	bindings := storage.Bindings{DB: lib.DB}
	_ = bindings.Set(storage.Binding{Provider: "mango-mangadex", MangaID: "OPM", Source: "al", ID: "74347"})
	_ = bindings.Set(storage.Binding{Provider: "mango-mangadex", MangaID: "Berserk", Source: "al", ID: "30002"})
	_ = bindings.Set(storage.Binding{Provider: "mango-mangadex", MangaID: "Berserk", Source: "mal", ID: "1"})
	mangas, _ := lib.Mangas()

	var batches int
	finder := BackupFinder{
		Files: l.Files,
		AniList: func(_ context.Context, ids [][]metadata.ID) ([]metadata.Metadata, error) {
			batches++
			metas := make([]metadata.Metadata, len(ids))
			for i, mangaIDs := range ids {
				switch mangaIDs[0].Raw {
				case "74347":
					metas[i] = testMeta("One-Punch Man", "74347", "44347")
				case "30002":
					metas[i] = testMeta("Berserk", "30002", "2")
				}
			}
			return metas, nil
		},
		ProviderIDs: func(_ context.Context, src storage.Source) ([]metadata.ID, error) {
			switch src.MangaID {
			case "OPM":
				return []metadata.ID{alID("74347"), {Raw: "24147", Source: metadata.IDSourceKitsu, Code: metadata.IDCodeKitsu}}, nil
			case "Berserk":
				// one provider failing doesn't stop the others
				return nil, errors.New("mangadex down")
			}
			return nil, nil
		},
	}
	backups, err := finder.Find(context.Background(), mangas)
	if err != nil {
		t.Fatal(err)
	}
	if batches != 1 || len(backups) != 2 {
		t.Fatalf("%d batches, backups %+v", batches, backups)
	}
	byTitle := map[string]Backup{}
	for _, b := range backups {
		byTitle[b.Manga.Title] = b
	}
	if b := byTitle["OPM"]; b.Fill[metadata.IDCodeKitsu] != "24147" || b.Fill[metadata.IDCodeMyAnimeList] != "44347" || b.NoKitsu {
		t.Errorf("OPM %+v", b)
	}
	if b := byTitle["Berserk"]; len(b.Fill) != 0 || len(b.Conflicts) != 1 || b.Conflicts[0].New != "2" || !b.NoKitsu {
		t.Errorf("Berserk %+v", b)
	}
	if got := strings.Join(byTitle["Berserk"].Lines(), "\n"); !strings.Contains(got, `"Berserk": MyAnimeList is 1, AniList 30002 says 2`) ||
		!strings.Contains(got, "couldn't get the links of mango-mangadex Berserk (mangadex down)") {
		t.Errorf("lines %q", got)
	}
	if fill, conflicts, noKitsu, noMAL := BackupSummary(backups); fill != 1 || conflicts != 1 || noKitsu != 1 || noMAL != 0 {
		t.Errorf("summary %d %d %d %d", fill, conflicts, noKitsu, noMAL)
	}
}

func TestBackupsKitsu(t *testing.T) {
	root := t.TempDir()
	l := newLinker(t, root, nil)
	lib := l.Library
	bindings := storage.Bindings{DB: lib.DB}
	for i, dir := range []string{"Agree", "Differs", "Disagree", "Unmapped"} {
		if err := lib.RememberMangaDir(root, dir, libmangal.MangaDirInfo{Provider: "mango-mangadex", Manga: mangadata.MangaInfo{ID: dir, Title: dir}}); err != nil {
			t.Fatal(err)
		}
		_ = bindings.Set(storage.Binding{Provider: "mango-mangadex", MangaID: dir, Source: "al", ID: strconv.Itoa(i + 1)})
	}
	_ = bindings.Set(storage.Binding{Provider: "mango-mangadex", MangaID: "Differs", Source: "kt", ID: "21"})
	mangas, _ := lib.Mangas()

	var asked [][]int
	finder := BackupFinder{
		Files: l.Files,
		AniList: func(_ context.Context, ids [][]metadata.ID) ([]metadata.Metadata, error) {
			return make([]metadata.Metadata, len(ids)), nil
		},
		ProviderIDs: func(_ context.Context, src storage.Source) ([]metadata.ID, error) {
			kt := map[string]string{"Agree": "10", "Disagree": "30"}[src.MangaID]
			if kt == "" {
				return nil, nil
			}
			return []metadata.ID{{Raw: kt, Source: metadata.IDSourceKitsu, Code: metadata.IDCodeKitsu}}, nil
		},
		KitsuIDs: func(_ context.Context, anilist []int) (map[int]int, error) {
			asked = append(asked, anilist)
			return map[int]int{1: 10, 2: 20, 3: 31}, nil
		},
	}
	backups, err := finder.Find(context.Background(), mangas)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || len(asked[0]) != 4 {
		t.Errorf("asked Kitsu %v, want the 4 AniList ids at once", asked)
	}
	byTitle := map[string]Backup{}
	for _, b := range backups {
		byTitle[b.Manga.Title] = b
	}
	if b := byTitle["Agree"]; b.Fill[metadata.IDCodeKitsu] != "10" || b.NoKitsu {
		t.Errorf("Agree %+v", b)
	}
	if b := byTitle["Differs"]; len(b.Conflicts) != 1 || b.Conflicts[0].Old[0] != "21" || b.Conflicts[0].New != "20" {
		t.Errorf("Differs %+v", b)
	}
	if b := byTitle["Disagree"]; b.Fill[metadata.IDCodeKitsu] != "" || b.NoKitsu || len(b.Disagree) != 1 {
		t.Errorf("Disagree %+v", b)
	} else if got := b.Lines()[len(b.Lines())-1]; !strings.Contains(got, "linked to AniList 3, but mango-mangadex links Kitsu 30 and Kitsu maps it to 31 (saved, pick one") {
		t.Errorf("Disagree line %q", got)
	}
	// saved for later, nothing linked
	picks, err := lib.LinkPicks()
	if err != nil || len(picks) != 1 || picks[0].Manga != byTitle["Disagree"].Manga.ID || picks[0].AniList != "3" ||
		len(picks[0].Candidates) != 2 || picks[0].Candidates[0] != (storage.LinkCandidate{ID: "30", From: "mango-mangadex"}) ||
		picks[0].Candidates[1] != (storage.LinkCandidate{ID: "31", From: storage.FromKitsu}) {
		t.Errorf("picks %+v, %v", picks, err)
	}
	// dismissed: found again, not asked again
	if _, err := lib.DismissLinkPick(picks[0].Manga, metadata.IDCodeKitsu); err != nil {
		t.Fatal(err)
	}
	backups, _ = finder.Find(context.Background(), mangas)
	for _, b := range backups {
		if b.Manga.Title == "Disagree" && (b.Pick != nil || len(b.Disagree) != 0) {
			t.Errorf("dismissed pick asked again: %+v", b)
		}
	}
	// one of them linked settles it
	_ = bindings.Set(storage.Binding{Provider: "mango-mangadex", MangaID: "Disagree", Source: "kt", ID: "31"})
	mangas, _ = lib.Mangas()
	backups, _ = finder.Find(context.Background(), mangas)
	for _, b := range backups {
		if b.Manga.Title == "Disagree" && (b.Pick != nil || len(b.Disagree) != 0) {
			t.Errorf("linked to a candidate, still a disagreement: %+v", b)
		}
	}
	if p, ok, _ := lib.LinkPick(picks[0].Manga, metadata.IDCodeKitsu); ok {
		t.Errorf("pick kept: %+v", p)
	}
	if b := byTitle["Unmapped"]; !b.NoKitsu || b.Fill[metadata.IDCodeKitsu] != "" {
		t.Errorf("Unmapped %+v", b)
	}
	if got := finder.Summary(backups); !strings.HasPrefix(got, "1 mangas to fill, 1 links differ; no Kitsu id for 1 (Kitsu maps none") {
		t.Errorf("summary %q", got)
	}
}
