package storage

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/luevano/libmangal"
)

func TestMigrateHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), FilenameData)
	// a data database from mangal 5.15 (schema 3)
	old, err := Open(path, dataMigrations[:3])
	if err != nil {
		t.Fatal(err)
	}
	if err := (Library{DB: old}).RecordDownload("/lib", "Berserk", Download{Path: "/lib/Berserk/1.cbz", Provider: "p", MangaID: "berserk", MangaTitle: "Berserk", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	old.Close()

	data, err := OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	var version int
	if err := data.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != len(dataMigrations) {
		t.Fatalf("version %d, %v", version, err)
	}
	if mangas, _ := (Library{DB: data}).Mangas(); len(mangas) != 1 {
		t.Errorf("library lost: %+v", mangas)
	}
	if reads, err := (History{DB: data}).List("", ""); err != nil || len(reads) != 0 {
		t.Errorf("reads %+v, %v", reads, err)
	}
}

func TestHistory(t *testing.T) {
	data := openData(t)
	history, library := History{DB: data}, Library{DB: data}
	if err := library.RecordDownload("/lib", "Berserk", Download{Path: "/lib/Berserk/1.cbz", Provider: "mango-weebcentral", MangaID: "berserk", MangaTitle: "Berserk", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	berserk, _, _ := library.MangaBySource("mango-weebcentral", "berserk")

	read := func(provider, manga string, chapter float32, libraryManga int64, at int64) {
		t.Helper()
		err := history.Record(Read{Provider: provider, MangaID: manga, MangaTitle: manga, Chapter: chapter, ChapterTitle: "t", LibraryManga: libraryManga, Last: time.Unix(at, 0)})
		if err != nil {
			t.Fatal(err)
		}
	}
	read("mango-weebcentral", "berserk", 1, berserk.ID, 10)
	read("mango-weebcentral", "berserk", 1, 0, 20) // read again, the library manga is kept
	read("mango-weebcentral", "berserk", 2, berserk.ID, 15)
	read("mango-mangadex", "berserk-md", 3, berserk.ID, 30) // another provider, same library manga
	read("mango-mangasee", "berserk", 4, 0, 5)              // the old id
	read("mango-weebcentral", "vagabond", 1, 0, 40)

	reads, err := history.List("mango-weebcentral", "berserk")
	if err != nil || len(reads) != 2 {
		t.Fatalf("got %+v, %v", reads, err)
	}
	if r := reads[0]; r.Chapter != 1 || r.Count != 2 || r.First.Unix() != 10 || r.Last.Unix() != 20 || r.LibraryManga != berserk.ID {
		t.Errorf("chapter 1: %+v", r)
	}
	if all, _ := history.List("", ""); len(all) != 5 || all[0].MangaID != "vagabond" {
		t.Errorf("all, last read first: %+v", all)
	}

	got, err := history.ReadChapters([]string{"mango-weebcentral", "mango-mangasee"}, "berserk", berserk.ID)
	if err != nil || len(got) != 4 || !got[1] || !got[2] || !got[3] || !got[4] {
		t.Errorf("read chapters %v, %v", got, err)
	}
	if got, _ := history.ReadChapters([]string{"mango-weebcentral"}, "berserk", 0); len(got) != 2 {
		t.Errorf("without the library manga: %v", got)
	}

	// deleting the library manga keeps the reads
	if err := library.RemoveManga(berserk.ID); err != nil {
		t.Fatal(err)
	}
	if reads, _ := history.List("mango-mangadex", ""); len(reads) != 1 || reads[0].LibraryManga != 0 {
		t.Errorf("after removing the library manga: %+v", reads)
	}

	if n, err := history.Clear("mango-weebcentral", "vagabond"); err != nil || n != 1 {
		t.Errorf("clear vagabond: %d, %v", n, err)
	}
	if n, err := history.Clear("", ""); err != nil || n != 4 {
		t.Errorf("clear all: %d, %v", n, err)
	}
}

func TestMergeKeepsReads(t *testing.T) {
	data := openData(t)
	history, library := History{DB: data}, Library{DB: data}
	for _, dir := range []string{"Berserk", "Berserk (1989)"} {
		if err := library.RecordDownload("/lib", dir, Download{Path: filepath.Join("/lib", dir, "1.cbz"), Provider: "p", MangaID: dir, MangaTitle: dir, Chapter: 1, Format: "CBZ"}); err != nil {
			t.Fatal(err)
		}
	}
	keep, _, _ := library.MangaByFolder("/lib", "Berserk")
	other, _, _ := library.MangaByFolder("/lib", "Berserk (1989)")
	if err := history.Record(Read{Provider: "p", MangaID: "Berserk (1989)", Chapter: 5, LibraryManga: other.ID, Last: time.Unix(1, 0)}); err != nil {
		t.Fatal(err)
	}
	if err := library.Merge(keep.ID, other.ID, nil, []string{"/lib/Berserk (1989)/1.cbz"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := history.ReadChapters(nil, "", keep.ID); !got[5] {
		t.Errorf("read of the merged manga not under the kept one: %v", got)
	}
}

func TestMigrateOldIDsReads(t *testing.T) {
	cache, data := openBoth(t)
	st := Storage{Cache: cache, Data: data}
	history := st.History()
	loaders := []libmangal.ProviderLoader{loader{weebCentral}}
	for _, r := range []Read{
		{Provider: "mango-mangasee", MangaID: "berserk", Chapter: 1, Last: time.Unix(10, 0)},
		{Provider: "mango-weebcentral", MangaID: "berserk", Chapter: 1, Last: time.Unix(20, 0)},
		{Provider: "mango-mangasee", MangaID: "berserk", Chapter: 2, Last: time.Unix(30, 0)},
	} {
		if err := history.Record(r); err != nil {
			t.Fatal(err)
		}
	}

	oldIDs, err := st.FindOldIDs(loaders)
	if err != nil || len(oldIDs) != 1 || oldIDs[0].Reads != 2 {
		t.Fatalf("got %+v, %v", oldIDs, err)
	}
	if err := st.MigrateOldIDs(oldIDs); err != nil {
		t.Fatal(err)
	}
	if oldIDs, _ := st.FindOldIDs(loaders); len(oldIDs) != 0 {
		t.Errorf("still %+v", oldIDs)
	}
	reads, _ := history.List("", "")
	if len(reads) != 2 || reads[0].Provider != "mango-weebcentral" || reads[1].Provider != "mango-weebcentral" {
		t.Fatalf("got %+v", reads)
	}
	// chapter 1 was read under both ids: merged
	if r := reads[1]; r.Chapter != 1 || r.Count != 2 || r.First.Unix() != 10 || r.Last.Unix() != 20 {
		t.Errorf("chapter 1: %+v", r)
	}
}
