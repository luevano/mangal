package client

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
)

type readManga struct{ mangadata.Manga }

func (readManga) Info() mangadata.MangaInfo {
	return mangadata.MangaInfo{ID: "berserk", Title: "Berserk"}
}
func (readManga) Metadata() metadata.Metadata { return nil }

type readVolume struct{ mangadata.Volume }

func (readVolume) Info() mangadata.VolumeInfo { return mangadata.VolumeInfo{Number: 2} }
func (readVolume) Manga() mangadata.Manga     { return readManga{} }

type readChapter struct{ mangadata.Chapter }

func (readChapter) Info() mangadata.ChapterInfo {
	return mangadata.ChapterInfo{Number: 3, Title: "The Brand", URL: "https://x/3"}
}
func (readChapter) Volume() mangadata.Volume { return readVolume{} }

func TestRecordRead(t *testing.T) {
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lib, history := storage.Library{DB: db}, storage.History{DB: db}
	// downloaded under the old id
	if err := lib.RecordDownload("/dl", "Berserk", storage.Download{Path: "/dl/Berserk/1.cbz", Provider: "mango-old", MangaID: "berserk", MangaTitle: "Berserk", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	provider := libmangal.ProviderInfo{ID: "mango-new", History: []libmangal.ProviderAlias{{ID: "mango-old"}}}

	for range 2 {
		if err := record(history, lib, provider, readChapter{}, true, time.Unix(100, 0)); err != nil {
			t.Fatal(err)
		}
	}
	reads, err := history.List("", "")
	if err != nil || len(reads) != 1 {
		t.Fatalf("got %+v, %v", reads, err)
	}
	r := reads[0]
	if r.Provider != "mango-new" || r.MangaID != "berserk" || r.MangaTitle != "Berserk" || r.Volume != 2 || r.Chapter != 3 ||
		r.ChapterTitle != "The Brand" || r.ChapterURL != "https://x/3" || r.LibraryManga == 0 || r.Count != 2 || r.Last.Unix() != 100 {
		t.Errorf("got %+v", r)
	}
}

func TestOpenFailed(t *testing.T) {
	path := "/tmp/ch 1.pdf"
	for _, c := range []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("open %s: %w", path, errors.New("exit status 4")), true},
		{errors.Join(errors.New("set progress on anilist: unauthorized")), false},
	} {
		if got := openFailed(c.err, path); got != c.want {
			t.Errorf("%v: got %t", c.err, got)
		}
	}
}
