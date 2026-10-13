package library

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

func TestMark(t *testing.T) {
	root := t.TempDir()
	lib := newLibrary(t)
	files := Files{FS: afero.NewOsFs(), Library: lib, Root: root, Modes: DefaultModes}
	see := filepath.Join(root, "mango-mangasee")
	// a source under an old provider id, no marker
	touch(t, filepath.Join(see, "Berserk", "1.cbz"))
	record(t, lib, see, "Berserk", "1.cbz", 1, "mango-mangasee")
	al := metadata.ID{Raw: "30002", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}
	if err := lib.RememberMangaDir(see, "Berserk", libmangal.MangaDirInfo{MetadataIDs: []metadata.ID{al}}); err != nil {
		t.Fatal(err)
	}
	// adopted from guesses: no source
	touch(t, filepath.Join(see, "Guessed", "1.cbz"))
	if err := lib.RememberMangaDir(see, "Guessed", libmangal.MangaDirInfo{Manga: mangadata.MangaInfo{Title: "Guessed"}}); err != nil {
		t.Fatal(err)
	}
	// has its marker already
	writeMarker(t, filepath.Join(see, "Vagabond"), libmangal.MangaDirInfo{Provider: "x", Manga: mangadata.MangaInfo{ID: "v"}})
	if err := lib.RememberMangaDir(see, "Vagabond", libmangal.MangaDirInfo{Provider: "mango-weebcentral", Manga: mangadata.MangaInfo{ID: "v2"}}); err != nil {
		t.Fatal(err)
	}

	weeb := libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central", History: []libmangal.ProviderAlias{{ID: "mango-mangasee"}}}
	var told []Marked
	var progress int
	files.OnProgress = func(Progress) { progress++ }
	res, err := files.Mark(MarkOptions{Loaders: []libmangal.ProviderLoader{loader{weeb}}, OnMarked: func(m Marked) { told = append(told, m) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Marked) != 1 || len(res.Unknown) != 1 || res.Unknown[0].Dir != "Guessed" {
		t.Fatalf("result %+v", res)
	}
	if len(told) != 1 || told[0].Manga.Dir != res.Marked[0].Manga.Dir || progress != 3 {
		t.Errorf("told %+v, %d progress reports", told, progress)
	}
	info, err := libmangal.ReadMangaDirInfo(files.FS, filepath.Join(see, "Berserk"))
	if err != nil || info.Provider != "mango-weebcentral" || info.Manga.ID != "berserk" || info.Manga.Title != "Berserk" || len(info.MetadataIDs) != 1 || info.MetadataIDs[0] != al {
		t.Errorf("marker %+v, %v", info, err)
	}
	if st, _ := os.Stat(filepath.Join(see, "Berserk", libmangal.FilenameMangaDirInfo)); st.Mode().Perm() != DefaultModes.File {
		t.Errorf("mode %v", st.Mode())
	}
	if info, _ := libmangal.ReadMangaDirInfo(files.FS, filepath.Join(see, "Vagabond")); info.Provider != "x" {
		t.Errorf("replaced a marker: %+v", info)
	}
	if _, err := os.Stat(filepath.Join(see, "Guessed", libmangal.FilenameMangaDirInfo)); !os.IsNotExist(err) {
		t.Error("marked a folder without a source")
	}

	// the scan relocates it by the marker, rename knows the provider directory by it
	if err := os.Rename(filepath.Join(see, "Berserk"), filepath.Join(see, "Berserk (1989)")); err != nil {
		t.Fatal(err)
	}
	scan, err := Scanner{FS: files.FS, Library: lib}.Scan(root, true)
	if err != nil || scan.Relocated != 1 {
		t.Errorf("scan %+v, %v", scan, err)
	}
	if res, _ := files.Mark(MarkOptions{}); len(res.Marked) != 0 {
		t.Errorf("marked twice: %+v", res.Marked)
	}
}

// TestMarkFromFolder: folders adopted by a scan without a source are marked from their
// provider directory and name: the provider (no manga id), the title from ComicInfo.xml,
// the id tag; a PDF (no ComicInfo.xml) one in no provider's directory with its ids only.
func TestMarkFromFolder(t *testing.T) {
	root := t.TempDir()
	lib := newLibrary(t)
	files := Files{FS: afero.NewOsFs(), Library: lib, Root: root, Modes: DefaultModes}
	dex := libmangal.ProviderInfo{ID: "mango-mangadex", Name: "MangaDex"}
	weeb := libmangal.ProviderInfo{ID: "mango-weebcentral", Name: "Weeb Central", History: []libmangal.ProviderAlias{{ID: "mango-mangasee", Name: "MangaSee"}}}
	opts := MarkOptions{Loaders: []libmangal.ProviderLoader{loader{dex}, loader{weeb}}, ProviderDirs: true, ProviderName: func(p libmangal.ProviderInfo) string { return p.ID }}

	writeSeriesCBZ(t, filepath.Join(root, "mango-mangadex", "Auto Hunting (2022) [alid-155950]", "[0001.0] Ch 1.cbz"))
	touch(t, filepath.Join(root, "mango-mangasee", "Clones (2021) [alid-7]", "[0002.0] Ch 2.pdf"))
	touch(t, filepath.Join(root, "batch", "Pluto [alid-8]", "[0003.0] Ch 3.pdf"))
	touch(t, filepath.Join(root, "batch", "Unknown", "[0004.0] Ch 4.pdf"))
	if _, err := (Scanner{FS: files.FS, Library: lib}).Scan(root, true); err != nil {
		t.Fatal(err)
	}
	res, err := files.Mark(opts)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Marked{}
	for _, m := range res.Marked {
		got[m.Manga.Dir] = m
	}
	al := func(id string) []metadata.ID {
		return []metadata.ID{{Raw: id, Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}}
	}
	if m := got["Auto Hunting (2022) [alid-155950]"]; m.Evidence != MarkedFromDir || m.Info.Provider != "mango-mangadex" || m.Info.Manga.ID != "" ||
		m.Info.Manga.Title != "Auto Hunting With My Clones" || !slices.Equal(m.Info.MetadataIDs, al("155950")) {
		t.Errorf("cbz %+v", m)
	}
	if m := got["Clones (2021) [alid-7]"]; m.Evidence != MarkedFromDir || m.Info.Provider != "mango-weebcentral" || m.Info.Manga.Title != "Clones" {
		t.Errorf("old directory name %+v", m)
	}
	if m := got["Pluto [alid-8]"]; m.Evidence != MarkedFromIDs || m.Info.Provider != "" || !slices.Equal(m.Info.MetadataIDs, al("8")) {
		t.Errorf("ids only %+v", m)
	}
	if len(res.Unknown) != 1 || res.Unknown[0].Dir != "Unknown" {
		t.Errorf("unknown %+v", res.Unknown)
	}
	// a provider-less marker matches a download only by metadata id, never by provider
	have, _ := libmangal.ReadMangaDirInfo(files.FS, filepath.Join(root, "mango-mangadex", "Auto Hunting (2022) [alid-155950]"))
	other := libmangal.MangaDirInfo{Provider: "mango-mangadex", Manga: mangadata.MangaInfo{ID: "x"}}
	if libmangal.MatchMangaDir(dex, other, have) != libmangal.MangaDirNoMatch {
		t.Error("an id-less marker matched another manga of its provider")
	}
	other.MetadataIDs = al("155950")
	if libmangal.MatchMangaDir(dex, other, have) != libmangal.MangaDirMetadataMatch {
		t.Error("the marker's metadata id doesn't match")
	}
}

// writeSeriesCBZ writes a cbz with a ComicInfo.xml like libmangal's: the series, and the
// chapter's url (which doesn't tell the manga id on MangaDex).
func writeSeriesCBZ(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	page, _ := w.Create("0001.jpg")
	_, _ = page.Write([]byte("img"))
	ci, _ := w.Create(metadata.FilenameComicInfoXML)
	_, _ = ci.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ComicInfo><Title>Chapter 1</Title><Series>Auto Hunting With My Clones</Series><Number>1</Number>
<Web>https://mangadex.org/chapter/0b7c5b4a-21ed-4d3e-9a06-bc02b4f4c1f1</Web><Year>2022</Year></ComicInfo>`))
	_ = w.Close()
	_ = f.Close()
}

// TestMarkManga: one folder is marked, a marker that disagrees with the library source or
// is broken is replaced (keeping its other ids), one that agrees is left.
func TestMarkManga(t *testing.T) {
	root := t.TempDir()
	lib := newLibrary(t)
	files := Files{FS: afero.NewOsFs(), Library: lib, Root: root, Modes: DefaultModes}
	dir := filepath.Join(root, "mango-weebcentral")
	kt := metadata.ID{Raw: "7", Source: metadata.IDSourceKitsu, Code: metadata.IDCodeKitsu}
	for _, name := range []string{"Unmarked", "Wrong", "Broken", "Right"} {
		touch(t, filepath.Join(dir, name, "1.cbz"))
		if err := lib.RecordDownload(dir, name, storage.Download{Path: filepath.Join(dir, name, "1.cbz"), Provider: "mango-weebcentral",
			MangaID: strings.ToLower(name), MangaTitle: name, Chapter: 1, Format: libmangal.FormatCBZ.String()}); err != nil {
			t.Fatal(err)
		}
	}
	writeMarker(t, filepath.Join(dir, "Wrong"), libmangal.MangaDirInfo{Provider: "mango-mangadex", Manga: mangadata.MangaInfo{ID: "md"}, MetadataIDs: []metadata.ID{kt}})
	if err := os.WriteFile(filepath.Join(dir, "Broken", libmangal.FilenameMangaDirInfo), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMarker(t, filepath.Join(dir, "Right"), libmangal.MangaDirInfo{Provider: "mango-mangasee", Manga: mangadata.MangaInfo{ID: "right"}})
	loaders := []libmangal.ProviderLoader{loader{libmangal.ProviderInfo{ID: "mango-weebcentral", History: []libmangal.ProviderAlias{{ID: "mango-mangasee"}}}}}

	folders, err := Folders(files.FS, root, true)
	if err != nil {
		t.Fatal(err)
	}
	broken := 0
	for _, f := range folders {
		if f.MarkerErr != nil {
			broken++
			if filepath.Base(f.Path) != "Broken" {
				t.Errorf("broken %s", f.Path)
			}
		}
	}
	if broken != 1 || len(folders) != 4 {
		t.Errorf("folders %+v", folders)
	}

	if res, err := files.Mark(MarkOptions{Loaders: loaders, DryRun: true}); err != nil || len(res.Broken) != 1 || len(res.Marked) != 1 {
		t.Errorf("mark with a broken marker: %+v, %v", res, err)
	}
	for _, c := range []struct {
		dir  string
		done bool
	}{{"Unmarked", true}, {"Wrong", true}, {"Broken", true}, {"Right", false}} {
		m, _, _ := lib.MangaByFolder(dir, c.dir)
		marked, done, err := files.MarkManga(m, MarkOptions{Loaders: loaders})
		if err != nil || done != c.done {
			t.Errorf("%s: done %v, %v", c.dir, done, err)
			continue
		}
		info, err := libmangal.ReadMangaDirInfo(files.FS, m.Path())
		if c.done && (err != nil || info.Provider != "mango-weebcentral" || info.Manga.ID != strings.ToLower(c.dir) || marked.Evidence != MarkedFromSource) {
			t.Errorf("%s: marker %+v, %v", c.dir, info, err)
		}
		if c.dir == "Wrong" && !slices.Contains(info.MetadataIDs, kt) {
			t.Errorf("lost the Kitsu id: %+v", info.MetadataIDs)
		}
		if c.dir == "Right" && info.Provider != "mango-mangasee" {
			t.Errorf("replaced an agreeing marker: %+v", info)
		}
	}
	m, _, _ := lib.MangaByFolder(dir, "Right")
	if what, _ := MarkerDisagreement(m, libmangal.MangaDirInfo{Provider: "mango-weebcentral"}, loaders); what == "" {
		t.Error("a marker without a manga id agrees")
	}
}
