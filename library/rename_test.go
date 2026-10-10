package library

import (
	"context"
	"encoding/json"
	"path/filepath"
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
		Metadata: func(_ context.Context, ids []metadata.ID) (metadata.Metadata, error) {
			return nil, nil
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

	// existing targets are skipped
	_ = fsys.MkdirAll("/lib/x", 0o755)
	_ = fsys.MkdirAll("/lib/y", 0o755)
	if errs := Apply(fsys, []Rename{{From: "/lib/x", To: "/lib/y"}}); errs[0] == nil {
		t.Error("renamed over an existing directory")
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
	if err := UpdateMarker(fsys, markers[0]); err != nil {
		t.Fatal(err)
	}
	if info, _ := libmangal.ReadMangaDirInfo(fsys, "/lib/Berserk"); info.Provider != "mango-weebcentral" || info.Manga.ID != "1" {
		t.Errorf("got %+v", info)
	}
	if markers, _ := OldMarkers(fsys, "/lib", false, loaders); len(markers) != 0 {
		t.Errorf("still old: %+v", markers)
	}
}
