package storage

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
)

func TestMigrateBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), FilenameData)
	// a data database with the watches' anilist_id (schema 5)
	old, err := Open(path, dataMigrations[:5])
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO watches (provider, manga_id, title, anilist_id, added) VALUES ('mango-mangaplus', '100171', 'Dandadan', 132029, 10)`,
		`INSERT INTO watches (provider, manga_id, title, anilist_id, added) VALUES ('mango-mangapill', 'x', 'X', 0, 10)`,
	} {
		if _, err := old.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	old.Close()

	data, err := OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	bindings, err := (Bindings{DB: data}).List("", "")
	if err != nil || len(bindings) != 1 {
		t.Fatalf("bindings %+v, %v", bindings, err)
	}
	if b := bindings[0]; b.Provider != "mango-mangaplus" || b.MangaID != "100171" || b.Source != metadata.IDCodeAnilist || b.ID != "132029" || !b.Default || b.Updated.Unix() != 10 {
		t.Errorf("binding %+v", b)
	}
	if watches, err := (Watches{DB: data}).List(); err != nil || len(watches) != 2 {
		t.Errorf("watches %+v, %v", watches, err)
	}
}

func TestBindings(t *testing.T) {
	bindings := Bindings{DB: openData(t)}
	const p, m = "mango-mangapill", "2"

	if _, found, err := bindings.Default(p, m); err != nil || found {
		t.Fatalf("default of nothing: %v, %v", found, err)
	}
	// the first one is the default
	if err := bindings.Set(Binding{Provider: p, MangaID: m, Source: metadata.IDCodeKitsu, ID: "11"}); err != nil {
		t.Fatal(err)
	}
	if err := bindings.Set(Binding{Provider: p, MangaID: m, Source: metadata.IDCodeAnilist, ID: "30013"}); err != nil {
		t.Fatal(err)
	}
	if d, found, err := bindings.Default(p, m); err != nil || !found || d.Source != metadata.IDCodeKitsu {
		t.Fatalf("default %+v, %v, %v", d, found, err)
	}
	// rebinding keeps the default
	if err := bindings.Set(Binding{Provider: p, MangaID: m, Source: metadata.IDCodeKitsu, ID: "12"}); err != nil {
		t.Fatal(err)
	}
	got, _ := bindings.Get(p, m)
	if len(got) != 2 || got[0].Source != metadata.IDCodeKitsu || got[0].ID != "12" || !got[0].Default || got[1].Source != metadata.IDCodeAnilist || got[1].Default {
		t.Fatalf("bindings %+v", got)
	}

	// one default
	if err := bindings.Set(Binding{Provider: p, MangaID: m, Source: metadata.IDCodeMyAnimeList, ID: "13", Default: true}); err != nil {
		t.Fatal(err)
	}
	if err := bindings.SetDefault(p, m, metadata.IDCodeAnilist); err != nil {
		t.Fatal(err)
	}
	got, _ = bindings.Get(p, m)
	defaults := 0
	for _, b := range got {
		if b.Default {
			defaults++
		}
	}
	if defaults != 1 || got[0].Source != metadata.IDCodeAnilist {
		t.Fatalf("bindings %+v", got)
	}
	if _, err := bindings.DB.Exec(`UPDATE metadata_bindings SET is_default = 1 WHERE source = 'kt'`); err == nil {
		t.Error("two defaults")
	}
	if err := bindings.SetDefault(p, m, "mu"); !errors.Is(err, ErrBindingNotFound) {
		t.Errorf("default of an unbound source: %v", err)
	}

	// the next one becomes the default
	if removed, err := bindings.Remove(p, m, metadata.IDCodeAnilist); err != nil || !removed {
		t.Fatalf("remove: %v, %v", removed, err)
	}
	if removed, err := bindings.Remove(p, m, metadata.IDCodeAnilist); err != nil || removed {
		t.Fatalf("removed twice: %v, %v", removed, err)
	}
	if d, _, _ := bindings.Default(p, m); d.Source != metadata.IDCodeKitsu {
		t.Errorf("default after removing it: %+v", d)
	}
	if all, _ := bindings.List("", ""); len(all) != 2 {
		t.Errorf("all %+v", all)
	}
}

func TestBindingsOfLibraryManga(t *testing.T) {
	data := openData(t)
	library, bindings := Library{DB: data}, Bindings{DB: data}
	info := libmangal.MangaDirInfo{Provider: "mango-mangapill", Manga: mangadata.MangaInfo{ID: "2", Title: "One Piece"},
		MetadataIDs: []metadata.ID{{Raw: "30013", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}, {Raw: "21", Source: metadata.IDSourceMyAnimeList, Code: metadata.IDCodeMyAnimeList}}}
	if err := library.RememberMangaDir("/lib", "One Piece", info); err != nil {
		t.Fatal(err)
	}
	if err := bindings.Set(Binding{Provider: "mango-mangapill", MangaID: "2", Source: metadata.IDCodeAnilist, ID: "30013"}); err != nil {
		t.Fatal(err)
	}
	if err := bindings.Set(Binding{Provider: "other", MangaID: "2", Source: metadata.IDCodeKitsu, ID: "1"}); err != nil {
		t.Fatal(err)
	}
	m, _, err := library.MangaBySource("mango-mangapill", "2")
	if err != nil {
		t.Fatal(err)
	}
	got, err := bindings.OfLibraryManga(m.ID)
	if err != nil || len(got) != 2 {
		t.Fatalf("bindings %+v, %v", got, err)
	}
	if got[0].Provider != "mango-mangapill" || !got[0].Default || got[1].Provider != "" || got[1].Source != metadata.IDCodeMyAnimeList || got[1].ID != "21" || got[1].Default {
		t.Errorf("bindings %+v", got)
	}
}

func TestMigrateOldIDsBindings(t *testing.T) {
	cache, data := openBoth(t)
	st := Storage{Cache: cache, Data: data}
	bindings := st.Bindings()
	at := time.Unix(1, 0)
	for _, b := range []Binding{
		{Provider: "mango-mangasee", MangaID: "berserk", Source: metadata.IDCodeAnilist, ID: "1", Default: true},
		{Provider: "mango-mangasee", MangaID: "berserk", Source: metadata.IDCodeKitsu, ID: "2"},
		{Provider: "mango-weebcentral", MangaID: "berserk", Source: metadata.IDCodeKitsu, ID: "3", Default: true},
		{Provider: "mango-mangasee", MangaID: "vagabond", Source: metadata.IDCodeAnilist, ID: "4"},
	} {
		b.Updated = at
		if err := bindings.Set(b); err != nil {
			t.Fatal(err)
		}
	}
	loaders := []libmangal.ProviderLoader{loader{weebCentral}}
	oldIDs, err := st.FindOldIDs(loaders)
	if err != nil || len(oldIDs) != 1 || oldIDs[0].Bindings != 3 {
		t.Fatalf("got %+v, %v", oldIDs, err)
	}
	if err := st.MigrateOldIDs(oldIDs); err != nil {
		t.Fatal(err)
	}
	berserk, _ := bindings.Get("mango-weebcentral", "berserk")
	if len(berserk) != 2 || berserk[0].Source != metadata.IDCodeKitsu || berserk[0].ID != "3" || !berserk[0].Default ||
		berserk[1].Source != metadata.IDCodeAnilist || berserk[1].Default {
		t.Errorf("berserk %+v", berserk)
	}
	if vagabond, _ := bindings.Get("mango-weebcentral", "vagabond"); len(vagabond) != 1 || !vagabond[0].Default {
		t.Errorf("vagabond %+v", vagabond)
	}
	if left, _ := bindings.List("mango-mangasee", ""); len(left) != 0 {
		t.Errorf("left %+v", left)
	}
}
