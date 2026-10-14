package library

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

func newLibrary(t *testing.T) storage.Library {
	t.Helper()
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	return storage.Library{DB: db}
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeCBZ writes a cbz with one page and a ComicInfo.xml with the number.
func writeCBZ(t *testing.T, path, number string) {
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
	_, _ = ci.Write([]byte("<ComicInfo><Title>Found</Title><Number>" + number + "</Number></ComicInfo>"))
	_ = w.Close()
	_ = f.Close()
}

func writeMarker(t *testing.T, dir string, info libmangal.MangaDirInfo) {
	t.Helper()
	data, _ := json.Marshal(info)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, libmangal.FilenameMangaDirInfo), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func record(t *testing.T, library storage.Library, parent, dir, file string, number float32, provider string) {
	t.Helper()
	err := library.RecordDownload(parent, dir, storage.Download{
		Path: filepath.Join(parent, dir, file), Provider: provider, MangaID: "berserk", MangaTitle: "Berserk",
		Chapter: number, Format: libmangal.FormatCBZ.String(), Downloaded: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestScanManga(t *testing.T) {
	root := t.TempDir()
	library := newLibrary(t)
	scanner := Scanner{FS: afero.NewOsFs(), Library: library}
	folder := filepath.Join(root, "Berserk")
	writeMarker(t, folder, libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: "berserk", Title: "Berserk"}})
	for _, f := range []string{"[0001.0] One.cbz", "[0002.0] Two.cbz", "[0003.0] Three.cbz"} {
		touch(t, filepath.Join(folder, f))
		record(t, library, root, "Berserk", f, map[string]float32{"[0001.0] One.cbz": 1, "[0002.0] Two.cbz": 2, "[0003.0] Three.cbz": 3}[f], "p")
	}

	// moved into a volume folder, deleted, and a new one copied by hand
	if err := os.MkdirAll(filepath.Join(folder, "Vol. 1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(folder, "[0001.0] One.cbz"), filepath.Join(folder, "Vol. 1", "[0001.0] One.cbz")); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(filepath.Join(folder, "[0002.0] Two.cbz"))
	writeCBZ(t, filepath.Join(folder, "manual copy.cbz"), "4")
	touch(t, filepath.Join(folder, "no number.cbz"))

	m, _, _ := library.MangaByFolder(root, "Berserk")
	var phases []string
	scanner.OnProgress = func(p Progress) { phases = append(phases, p.String()) }
	res, err := scanner.ScanManga(m, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Moved != 1 || res.Removed != 1 || res.Added != 1 || len(res.Unrecognized) != 1 {
		t.Errorf("got %+v", res)
	}
	if want := []string{"reading the chapter files (Berserk)", "checking the recorded chapters: 1/3 (Berserk)", "checking the recorded chapters: 2/3 (Berserk)",
		"checking the recorded chapters: 3/3 (Berserk)", "adding the new chapters (Berserk)"}; !slices.Equal(phases, want) {
		t.Errorf("progress %q, want %q", phases, want)
	}
	downloads, _ := library.Downloads(m.ID)
	numbers := map[float32]string{}
	for _, dl := range downloads {
		numbers[dl.Chapter] = dl.Path
	}
	if len(downloads) != 3 || numbers[1] != filepath.Join(folder, "Vol. 1", "[0001.0] One.cbz") || numbers[4] != filepath.Join(folder, "manual copy.cbz") {
		t.Errorf("downloads %v", numbers)
	}

	// the folder renamed outside of mangal is found again by its .libmangal.json
	if err := os.Rename(folder, filepath.Join(root, "Berserk (1989)")); err != nil {
		t.Fatal(err)
	}
	res, err = scanner.ScanManga(m, root, false)
	if err != nil || res.Relocated != 1 || len(res.Missing) != 0 {
		t.Fatalf("got %+v, %v", res, err)
	}
	if moved, ok, _ := library.MangaByFolder(root, "Berserk (1989)"); !ok || moved.ID != m.ID {
		t.Errorf("not relocated")
	}

	// gone for good: reported missing, not dropped
	if err := os.RemoveAll(filepath.Join(root, "Berserk (1989)")); err != nil {
		t.Fatal(err)
	}
	m, _, _ = library.MangaByID(m.ID)
	res, _ = scanner.ScanManga(m, root, false)
	if len(res.Missing) != 1 {
		t.Errorf("got %+v", res)
	}
	if _, ok, _ := library.MangaByID(m.ID); !ok {
		t.Error("missing manga dropped")
	}
}

func TestScanAddsMarkedFolders(t *testing.T) {
	root := t.TempDir()
	library := newLibrary(t)
	folder := filepath.Join(root, "Vagabond")
	writeMarker(t, folder, libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: "vagabond", Title: "Vagabond"}})
	writeCBZ(t, filepath.Join(folder, "[0001.0] x.cbz"), "1")

	var progress []string
	scanner := Scanner{FS: afero.NewOsFs(), Library: library, OnProgress: func(p Progress) { progress = append(progress, p.String()) }}
	res, err := scanner.Scan(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 1 || res.Added != 1 {
		t.Errorf("got %+v", res)
	}
	if want := []string{"looking for new manga folders", "looking for new manga folders: 1/1 (Vagabond)"}; !slices.Equal(progress, want) {
		t.Errorf("progress %q, want %q", progress, want)
	}
	progress = nil
	if _, err := scanner.Scan(root, false); err != nil || len(progress) == 0 || progress[0] != "scanning library: 1/1 (Vagabond)" {
		t.Errorf("progress of the second scan %q, %v", progress, err)
	}
	mangas, _ := library.Mangas()
	if len(mangas) != 1 || mangas[0].Title != "Vagabond" || mangas[0].Chapters != 1 || len(mangas[0].Sources) != 1 {
		t.Errorf("got %+v", mangas)
	}
}

func TestMergeAndTrash(t *testing.T) {
	root := t.TempDir()
	library := newLibrary(t)
	fsys := afero.NewOsFs()
	for _, f := range []string{"[0001.0] One.cbz", "[0002.0] Two.cbz"} {
		touch(t, filepath.Join(root, "Berserk", f))
	}
	record(t, library, root, "Berserk", "[0001.0] One.cbz", 1, "a")
	record(t, library, root, "Berserk", "[0002.0] Two.cbz", 2, "a")
	for _, f := range []string{"Ch 2.cbz", "Ch 3.cbz"} {
		touch(t, filepath.Join(root, "Berserk B", f))
	}
	writeMarker(t, filepath.Join(root, "Berserk B"), libmangal.MangaDirInfo{Provider: "b"})
	record(t, library, root, "Berserk B", "Ch 2.cbz", 2, "b")
	record(t, library, root, "Berserk B", "Ch 3.cbz", 3, "b")

	keep, _, _ := library.MangaByFolder(root, "Berserk")
	other, _, _ := library.MangaByFolder(root, "Berserk B")
	files := Files{FS: fsys, Library: library, Root: root, Modes: DefaultModes}
	plan, err := files.PlanMerge(keep, other)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Trashed) != 1 || len(plan.Moves) != 1 || plan.Moves[0].To != filepath.Join(root, "Berserk", "Ch 3.cbz") {
		t.Fatalf("plan %+v", plan)
	}
	if err := files.ApplyMerge(plan); err != nil {
		t.Fatal(err)
	}

	mangas, _ := library.Mangas()
	if len(mangas) != 1 || mangas[0].Chapters != 3 || len(mangas[0].Sources) != 2 {
		t.Errorf("after merge %+v", mangas)
	}
	if _, err := os.Stat(filepath.Join(root, "Berserk B")); !os.IsNotExist(err) {
		t.Error("merged folder left")
	}
	trash, err := Trash(fsys, root)
	if err != nil || len(trash) != 1 || trash[0].Files != 2 { // the duplicate chapter and the marker
		t.Fatalf("trash %+v %v", trash, err)
	}
	if err := EmptyTrash(fsys, root); err != nil {
		t.Fatal(err)
	}
	if trash, _ := Trash(fsys, root); len(trash) != 0 {
		t.Errorf("trash %+v after emptying", trash)
	}
}

func TestMoveToTrashMergesFolders(t *testing.T) {
	root := t.TempDir()
	fsys := afero.NewOsFs()
	touch(t, filepath.Join(root, "M", "Vol. 1", "a.cbz"))
	touch(t, filepath.Join(root, "M", "Vol. 1", "b.cbz"))
	if err := moveToTrash(fsys, nil, root, "batch", filepath.Join(root, "M", "Vol. 1", "a.cbz"), DefaultModes); err != nil {
		t.Fatal(err)
	}
	if err := moveToTrash(fsys, nil, root, "batch", filepath.Join(root, "M", "Vol. 1"), DefaultModes); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.cbz", "b.cbz"} {
		if _, err := os.Stat(filepath.Join(root, TrashDir, "batch", "M", "Vol. 1", f)); err != nil {
			t.Error(err)
		}
	}
}

func TestScanUnmarked(t *testing.T) {
	root := t.TempDir()
	library := newLibrary(t)
	scanner := Scanner{FS: afero.NewOsFs(), Library: library}

	// downloaded before the markers: name from the default template, a series.json
	claymore := filepath.Join(root, "mango-mangadex", "Claymore (2001) [alid-30583]")
	writeCBZ(t, filepath.Join(claymore, "[0001.0] Silver-eyed Slayer.cbz"), "1")
	writeCBZ(t, filepath.Join(claymore, "[0002.0] Claws in the Sky.cbz"), "2")
	// only the series.json knows the id
	vagabond := filepath.Join(root, "mango-mangasee", "Vagabond")
	writeCBZ(t, filepath.Join(vagabond, "Vol. 1", "[0001.0] One.cbz"), "1")
	series := `{"metadata":{"name":"Vagabond","comicid":30656,"comic_image":"https://s4.anilist.co/file/x.png"}}`
	if err := os.WriteFile(filepath.Join(vagabond, metadata.FilenameSeriesJSON), []byte(series), 0o644); err != nil {
		t.Fatal(err)
	}
	// not mangas: an empty folder, and the provider directories themselves
	if err := os.MkdirAll(filepath.Join(root, "mango-mangadex", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := scanner.Scan(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 2 || res.Added != 3 {
		t.Fatalf("got %+v, want 2 new mangas with 3 chapters", res)
	}

	m, ok, err := library.MangaByFolder(filepath.Dir(claymore), filepath.Base(claymore))
	if err != nil || !ok || m.Title != "Claymore" || len(m.MetadataIDs) != 1 || m.MetadataIDs[0].Raw != "30583" || len(m.Sources) != 0 {
		t.Fatalf("claymore: %+v, %t, %v", m, ok, err)
	}
	downloads, _ := library.Downloads(m.ID)
	if len(downloads) != 2 || downloads[0].Provider != "mango-mangadex" {
		t.Errorf("claymore downloads %+v", downloads)
	}
	m, ok, _ = library.MangaByFolder(filepath.Dir(vagabond), "Vagabond")
	if !ok || len(m.MetadataIDs) != 1 || m.MetadataIDs[0].Raw != "30656" {
		t.Errorf("vagabond: %+v, %t", m, ok)
	}

	// the next download from a provider finds the folder by the metadata id
	want := libmangal.MangaDirInfo{Provider: "mango-mangadex", Manga: mangadata.MangaInfo{ID: "uuid"}, MetadataIDs: []metadata.ID{{Raw: "30583", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}}}
	dir, found, err := library.FindMangaDir(filepath.Dir(claymore), libmangal.ProviderInfo{ID: "mango-mangadex"}, want)
	if err != nil || !found || dir != filepath.Base(claymore) {
		t.Errorf("find manga dir: %q, %t, %v", dir, found, err)
	}

	// scanning again changes nothing
	if res, err := scanner.Scan(root, true); err != nil || res.New != 0 || res.Added != 0 {
		t.Errorf("second scan: %+v, %v", res, err)
	}
}

func TestScanUnmarkedProviderDirsOff(t *testing.T) {
	root := t.TempDir()
	library := newLibrary(t)
	scanner := Scanner{FS: afero.NewOsFs(), Library: library}
	writeCBZ(t, filepath.Join(root, "mango-mangadex", "Claymore (2001) [alid-30583]", "[0001.0] One.cbz"), "1")
	touch(t, filepath.Join(root, "mango-mangadex", "Claymore (2001) [alid-30583]", metadata.FilenameCoverJPG))

	// the provider directory isn't a manga
	res, err := scanner.Scan(root, false)
	if err != nil || res.New != 0 {
		t.Errorf("got %+v, %v, want nothing added", res, err)
	}
}

func TestScanUnmarkedKitsuTag(t *testing.T) {
	root := t.TempDir()
	library := newLibrary(t)
	scanner := Scanner{FS: afero.NewOsFs(), Library: library}
	// named from Kitsu metadata (Anilist found nothing)
	folder := filepath.Join(root, "mango-mangadex", "Some Manga (2010) [ktid-12345]")
	writeCBZ(t, filepath.Join(folder, "[0001.0] One.cbz"), "1")

	if res, err := scanner.Scan(root, true); err != nil || res.New != 1 {
		t.Fatalf("got %+v, %v, want 1 new manga", res, err)
	}
	m, ok, err := library.MangaByFolder(filepath.Dir(folder), filepath.Base(folder))
	kitsuID := metadata.ID{Raw: "12345", Source: metadata.IDSourceKitsu, Code: metadata.IDCodeKitsu}
	if err != nil || !ok || m.Title != "Some Manga" || len(m.MetadataIDs) != 1 || m.MetadataIDs[0] != kitsuID {
		t.Fatalf("got %+v, %t, %v", m, ok, err)
	}

	provider := libmangal.ProviderInfo{ID: "mango-mangadex"}
	want := libmangal.MangaDirInfo{Provider: "mango-mangadex", Manga: mangadata.MangaInfo{ID: "uuid"}, MetadataIDs: []metadata.ID{kitsuID}}
	if dir, found, err := library.FindMangaDir(filepath.Dir(folder), provider, want); err != nil || !found || dir != filepath.Base(folder) {
		t.Errorf("find by the kitsu id: %q, %t, %v", dir, found, err)
	}
	// the same number on Anilist is another manga
	want.MetadataIDs = []metadata.ID{{Raw: "12345", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}}
	if dir, found, err := library.FindMangaDir(filepath.Dir(folder), provider, want); err != nil || found {
		t.Errorf("found by an anilist id: %q, %t, %v", dir, found, err)
	}
}
