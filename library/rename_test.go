package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/spf13/afero"
)

type loader struct{ info libmangal.ProviderInfo }

func (l loader) String() string                                   { return l.info.Name }
func (l loader) Info() libmangal.ProviderInfo                     { return l.info }
func (l loader) Load(context.Context) (libmangal.Provider, error) { return nil, nil }

func writeInfo(t *testing.T, fsys afero.Fs, dir string, info libmangal.MangaDirInfo) {
	t.Helper()
	data, _ := json.Marshal(info)
	if err := afero.WriteFile(fsys, filepath.Join(dir, libmangal.FilenameMangaDirInfo), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPlanAndApply(t *testing.T) {
	fsys := afero.NewMemMapFs()
	weebCentral := libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central", History: []libmangal.ProviderAlias{{ID: "mango-mangasee", Name: "MangaSee"}}}
	n := Namer{
		FS:           fsys,
		Loaders:      []libmangal.ProviderLoader{loader{weebCentral}},
		ProviderName: func(p libmangal.ProviderInfo) string { return p.Name },
		MangaName: func(_ libmangal.ProviderInfo, m mangadata.Manga) string {
			if m.Metadata() != nil {
				return m.Metadata().Title()
			}
			return m.Info().Title
		},
	}

	writeInfo(t, fsys, "/lib/MangaSee/Only I Level Up", libmangal.MangaDirInfo{Provider: "mango-mangasee", Manga: mangadata.MangaInfo{ID: "1", Title: "Solo Leveling"}})
	writeInfo(t, fsys, "/lib/MangaSee/Berserk", libmangal.MangaDirInfo{Provider: "mango-mangasee", Manga: mangadata.MangaInfo{ID: "2", Title: "Berserk"}})
	_ = fsys.MkdirAll("/lib/MangaSee/no info", 0o755)

	renames, err := n.Plan(context.Background(), "/lib", true)
	if err != nil {
		t.Fatal(err)
	}
	want := []Rename{
		{From: "/lib/MangaSee/Only I Level Up", To: "/lib/MangaSee/Solo Leveling"},
		{From: "/lib/MangaSee", To: "/lib/Weeb Central"},
	}
	if len(renames) != len(want) || renames[0] != want[0] || renames[1] != want[1] {
		t.Fatalf("got %v, want %v", renames, want)
	}

	for i, err := range Apply(fsys, renames) {
		if err != nil {
			t.Errorf("rename %d: %v", i, err)
		}
	}
	for _, dir := range []string{"/lib/Weeb Central/Solo Leveling", "/lib/Weeb Central/Berserk", "/lib/Weeb Central/no info"} {
		if ok, _ := afero.DirExists(fsys, dir); !ok {
			t.Errorf("%s missing", dir)
		}
	}

	// existing targets are skipped, empty ones replaced
	_ = fsys.MkdirAll("/lib/x", 0o755)
	_ = afero.WriteFile(fsys, "/lib/y/1.cbz", []byte("x"), 0o644)
	if errs := Apply(fsys, []Rename{{From: "/lib/x", To: "/lib/y"}}); errs[0] == nil {
		t.Error("renamed over an existing directory")
	}
	_ = fsys.MkdirAll("/lib/z", 0o755)
	if errs := Apply(fsys, []Rename{{From: "/lib/x", To: "/lib/z"}}); errs[0] != nil {
		t.Errorf("onto an empty directory: %v", errs[0])
	}
}

func TestOldMarkers(t *testing.T) {
	fsys := afero.NewMemMapFs()
	weebCentral := libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central", History: []libmangal.ProviderAlias{{ID: "mango-mangasee"}}}
	loaders := []libmangal.ProviderLoader{loader{weebCentral}}
	writeInfo(t, fsys, "/lib/Berserk", libmangal.MangaDirInfo{Provider: "mango-mangasee", Manga: mangadata.MangaInfo{ID: "1"}})
	writeInfo(t, fsys, "/lib/Vagabond", libmangal.MangaDirInfo{Provider: "mango-weebcentral", Manga: mangadata.MangaInfo{ID: "2"}})

	markers, err := OldMarkers(fsys, "/lib", false, loaders)
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 1 || markers[0] != (OldMarker{Dir: "/lib/Berserk", Old: "mango-mangasee", New: "mango-weebcentral"}) {
		t.Fatalf("got %+v", markers)
	}
	if err := UpdateMarker(fsys, markers[0], 0o644); err != nil {
		t.Fatal(err)
	}
	if info, _ := libmangal.ReadMangaDirInfo(fsys, "/lib/Berserk"); info.Provider != "mango-weebcentral" || info.Manga.ID != "1" {
		t.Errorf("got %+v", info)
	}
	if markers, _ := OldMarkers(fsys, "/lib", false, loaders); len(markers) != 0 {
		t.Errorf("still old: %+v", markers)
	}
}

// TestPlanOldProviderDirs: a provider directory with an old name is renamed without marked
// mangas in it; onto an existing directory it's skipped.
func TestPlanOldProviderDirs(t *testing.T) {
	fsys := afero.NewMemMapFs()
	weebCentral := libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central", History: []libmangal.ProviderAlias{{ID: "mango-mangasee", Name: "MangaSee"}}}
	n := Namer{
		FS:           fsys,
		Loaders:      []libmangal.ProviderLoader{loader{weebCentral}},
		ProviderName: func(p libmangal.ProviderInfo) string { return p.ID },
		MangaName:    func(_ libmangal.ProviderInfo, m mangadata.Manga) string { return m.Info().Title },
	}
	_ = fsys.MkdirAll("/lib/mango-mangasee/Berserk", 0o755)
	_ = afero.WriteFile(fsys, "/lib/mango-mangasee/Berserk/1.cbz", []byte("x"), 0o644)

	renames, err := n.Plan(context.Background(), "/lib", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(renames) != 1 || renames[0] != (Rename{From: "/lib/mango-mangasee", To: "/lib/mango-weebcentral"}) {
		t.Fatalf("got %+v", renames)
	}
	if renames, _ := n.Plan(context.Background(), "/lib", false); len(renames) != 0 {
		t.Errorf("without provider directories: %+v", renames)
	}

	// an empty directory of the current name is replaced
	_ = fsys.MkdirAll("/lib/mango-weebcentral", 0o755)
	renames, _ = n.Plan(context.Background(), "/lib", true)
	if len(renames) != 1 || renames[0].To != "/lib/mango-weebcentral" || renames[0].Skip != "" {
		t.Fatalf("empty target: %+v", renames)
	}
	for _, err := range Apply(fsys, renames) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if ok, _ := afero.Exists(fsys, "/lib/mango-weebcentral/Berserk/1.cbz"); !ok {
		t.Error("not renamed onto the empty directory")
	}
}

// TestPlanIntoExisting: an old provider directory's mangas move into the existing one of
// its current name one by one; the same manga (by id) or a taken name is skipped,
// unknown folders stay, the old directory goes only once empty.
func TestPlanIntoExisting(t *testing.T) {
	root := t.TempDir()
	fsys := afero.NewOsFs()
	lib := newLibrary(t)
	weebCentral := libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central", History: []libmangal.ProviderAlias{{ID: "mango-mangasee", Name: "MangaSee"}}}
	n := Namer{
		FS:           fsys,
		Loaders:      []libmangal.ProviderLoader{loader{weebCentral}},
		ProviderName: func(p libmangal.ProviderInfo) string { return p.ID },
		MangaName:    func(_ libmangal.ProviderInfo, m mangadata.Manga) string { return m.Info().Title },
		Library:      lib,
	}
	see, weeb := filepath.Join(root, "mango-mangasee"), filepath.Join(root, "mango-weebcentral")
	al := metadata.ID{Raw: "30002", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	// moves: in the library under the old id, nothing like it in the target
	touch(t, filepath.Join(see, "Vagabond", "1.cbz"))
	record(t, lib, see, "Vagabond", "1.cbz", 1, "mango-mangasee")
	// same manga as the target's Berserk (AniList id), by another folder name
	touch(t, filepath.Join(see, "Berserk (old)", "1.cbz"))
	_ = lib.RememberMangaDir(see, "Berserk (old)", libmangal.MangaDirInfo{Manga: mangadata.MangaInfo{Title: "Berserk"}, MetadataIDs: []metadata.ID{al}})
	writeMarker(t, filepath.Join(weeb, "Berserk"), libmangal.MangaDirInfo{Provider: "mango-weebcentral", Manga: mangadata.MangaInfo{ID: "b", Title: "Berserk"}, MetadataIDs: []metadata.ID{al}})
	_ = lib.RememberMangaDir(weeb, "Berserk", libmangal.MangaDirInfo{Provider: "mango-weebcentral", Manga: mangadata.MangaInfo{ID: "b", Title: "Berserk"}, MetadataIDs: []metadata.ID{al}})
	// a name taken by a different manga
	writeMarker(t, filepath.Join(see, "Monster"), libmangal.MangaDirInfo{Provider: "mango-mangasee", Manga: mangadata.MangaInfo{ID: "m1", Title: "Monster"}})
	writeMarker(t, filepath.Join(weeb, "Monster"), libmangal.MangaDirInfo{Provider: "mango-weebcentral", Manga: mangadata.MangaInfo{ID: "m2", Title: "Monster"}})
	// unknown, and a file: they stay
	touch(t, filepath.Join(see, "stuff", "x.txt"))
	touch(t, filepath.Join(see, "notes.txt"))

	renames, err := n.Plan(context.Background(), root, true)
	if err != nil {
		t.Fatal(err)
	}
	byFrom := map[string]Rename{}
	for _, r := range renames {
		byFrom[filepath.Base(r.From)] = r
	}
	berserk, _, _ := lib.MangaByFolder(weeb, "Berserk")
	old, _, _ := lib.MangaByFolder(see, "Berserk (old)")
	if r := byFrom["Vagabond"]; r.To != filepath.Join(weeb, "Vagabond") || r.Skip != "" {
		t.Errorf("vagabond %+v", r)
	}
	if r := byFrom["Berserk (old)"]; !strings.Contains(r.Skip, fmt.Sprintf("mangal library merge %d %d", berserk.ID, old.ID)) {
		t.Errorf("same manga %+v", r)
	}
	if r := byFrom["Monster"]; !strings.Contains(r.Skip, "is another manga's folder") {
		t.Errorf("name clash %+v", r)
	}
	if r := byFrom["stuff"]; r.To != "" || !strings.Contains(r.Skip, "no known manga") {
		t.Errorf("unknown %+v", r)
	}
	if r := byFrom["mango-mangasee"]; !r.RemoveEmpty {
		t.Errorf("old directory %+v", r)
	}
	errs := Apply(fsys, renames)
	for i, r := range renames {
		if r.Skip == "" && !r.RemoveEmpty && errs[i] != nil {
			t.Errorf("%s: %v", r.From, errs[i])
		}
		if r.RemoveEmpty && !errors.Is(errs[i], ErrNotEmpty) {
			t.Errorf("removed a directory with leftovers: %v", errs[i])
		}
	}
	if ok, _ := afero.Exists(fsys, filepath.Join(weeb, "Vagabond", "1.cbz")); !ok {
		t.Error("vagabond not moved")
	}
	for _, left := range []string{"Berserk (old)", "Monster", "stuff", "notes.txt"} {
		if ok, _ := afero.Exists(fsys, filepath.Join(see, left)); !ok {
			t.Errorf("%s didn't stay", left)
		}
	}

	// once only movable mangas are left, the old directory goes
	_ = os.RemoveAll(filepath.Join(see, "Berserk (old)"))
	_ = os.RemoveAll(filepath.Join(see, "Monster"))
	_ = os.RemoveAll(filepath.Join(see, "stuff"))
	_ = os.Remove(filepath.Join(see, "notes.txt"))
	touch(t, filepath.Join(see, "Pluto", "1.cbz"))
	writeMarker(t, filepath.Join(see, "Pluto"), libmangal.MangaDirInfo{Provider: "mango-mangasee", Manga: mangadata.MangaInfo{ID: "p", Title: "Pluto"}})
	renames, _ = n.Plan(context.Background(), root, true)
	for i, err := range Apply(fsys, renames) {
		if err != nil {
			t.Errorf("%+v: %v", renames[i], err)
		}
	}
	if ok, _ := afero.Exists(fsys, see); ok {
		t.Error("the emptied old directory is still there")
	}
}

// TestPlanMixedProviderDir: a provider directory with mangas of several providers isn't
// taken for any of theirs.
func TestPlanMixedProviderDir(t *testing.T) {
	fsys := afero.NewMemMapFs()
	weebCentral := libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central"}
	mangadex := libmangal.ProviderInfo{ID: "mango-mangadex", Name: "MangaDex"}
	n := Namer{
		FS:           fsys,
		Loaders:      []libmangal.ProviderLoader{loader{weebCentral}, loader{mangadex}},
		ProviderName: func(p libmangal.ProviderInfo) string { return p.ID },
		MangaName:    func(_ libmangal.ProviderInfo, m mangadata.Manga) string { return m.Info().Title },
	}
	writeInfo(t, fsys, "/lib/MangaDex/A", libmangal.MangaDirInfo{Provider: "mango-mangadex", Manga: mangadata.MangaInfo{ID: "a", Title: "A"}})
	writeInfo(t, fsys, "/lib/MangaDex/B", libmangal.MangaDirInfo{Provider: "mango-weebcentral", Manga: mangadata.MangaInfo{ID: "b", Title: "B"}})
	if renames, err := n.Plan(context.Background(), "/lib", true); err != nil || len(renames) != 0 {
		t.Errorf("mixed directory: %+v, %v", renames, err)
	}
	// only another provider's manga in it: still that provider's directory by its name
	_ = fsys.RemoveAll("/lib/MangaDex/A")
	writeInfo(t, fsys, "/lib/mango-mangadex/C", libmangal.MangaDirInfo{Provider: "mango-weebcentral", Manga: mangadata.MangaInfo{ID: "c", Title: "C"}})
	if renames, err := n.Plan(context.Background(), "/lib", true); err != nil || len(renames) != 1 || renames[0].From != "/lib/MangaDex" {
		t.Errorf("another provider's directory: %+v, %v", renames, err)
	}
}

// TestPlanMetadataBatched: the metadata of every folder is looked up in one call, before
// planning (AniList rate limits a lookup per folder).
func TestPlanMetadataBatched(t *testing.T) {
	fsys := afero.NewMemMapFs()
	al := func(raw string) metadata.ID {
		return metadata.ID{Raw: raw, Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	}
	var calls [][][]metadata.ID
	n := Namer{
		FS:           fsys,
		ProviderName: func(p libmangal.ProviderInfo) string { return p.ID },
		MangaName: func(_ libmangal.ProviderInfo, m mangadata.Manga) string {
			if m.Metadata() != nil {
				return m.Metadata().Title()
			}
			return m.Info().Title
		},
		Metadata: func(_ context.Context, ids [][]metadata.ID) ([]metadata.Metadata, error) {
			calls = append(calls, ids)
			metas := make([]metadata.Metadata, len(ids))
			for i, mangaIDs := range ids {
				if len(mangaIDs) != 0 && mangaIDs[0].Raw == "30002" {
					metas[i] = fakeMeta{title: "Berserk", id: mangaIDs[0]}
				}
			}
			return metas, nil
		},
	}
	writeInfo(t, fsys, "/lib/Berserk (1989) [alid-30002]", libmangal.MangaDirInfo{Manga: mangadata.MangaInfo{Title: "x"}, MetadataIDs: []metadata.ID{al("30002")}})
	writeInfo(t, fsys, "/lib/One Piece", libmangal.MangaDirInfo{Manga: mangadata.MangaInfo{Title: "One Piece"}, MetadataIDs: []metadata.ID{al("30013")}})
	writeInfo(t, fsys, "/lib/No ids", libmangal.MangaDirInfo{Manga: mangadata.MangaInfo{Title: "No ids"}})

	renames, err := n.Plan(context.Background(), "/lib", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || len(calls[0]) != 3 {
		t.Fatalf("metadata calls %v, want one with the 3 folders", calls)
	}
	if want := (Rename{From: "/lib/Berserk (1989) [alid-30002]", To: "/lib/Berserk"}); len(renames) != 1 || renames[0] != want {
		t.Errorf("got %v, want %v", renames, want)
	}
}
