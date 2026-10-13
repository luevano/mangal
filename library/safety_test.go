package library

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// moveFile never replaces what's at the target, a file or an empty folder
func TestMoveFileNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	fsys := afero.NewOsFs()
	for _, target := range []string{"file.cbz", "empty"} {
		from, to := filepath.Join(dir, "a.cbz"), filepath.Join(dir, target)
		touch(t, from)
		if target == "empty" {
			if err := os.Mkdir(to, 0o755); err != nil {
				t.Fatal(err)
			}
			from = filepath.Join(dir, "a")
			if err := os.Mkdir(from, 0o755); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(to, []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := moveFile(fsys, nil, from, to, DefaultModes); !errors.Is(err, fs.ErrExist) {
			t.Errorf("%s: %v", target, err)
		}
		if _, err := os.Stat(from); err != nil {
			t.Errorf("%s: source gone: %v", target, err)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "file.cbz")); string(got) != "keep" {
		t.Errorf("replaced: %q", got)
	}
}

// a copy across drives that fails because the target appeared never removes the target
func TestCopyAllKeepsExistingTarget(t *testing.T) {
	dir := t.TempDir()
	fsys := afero.NewOsFs()
	touch(t, filepath.Join(dir, "a", "1.cbz"))
	touch(t, filepath.Join(dir, "b", "mine.cbz"))
	created, err := copyAll(fsys, filepath.Join(dir, "a"), filepath.Join(dir, "b"))
	if err == nil || created {
		t.Fatalf("copied into an existing folder: %t, %v", created, err)
	}
	created, err = copyAll(fsys, filepath.Join(dir, "a", "1.cbz"), filepath.Join(dir, "b", "mine.cbz"))
	if err == nil || created {
		t.Fatalf("copied onto an existing file: %t, %v", created, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b", "mine.cbz")); err != nil {
		t.Error(err)
	}
}

// a merge's taken names: a folder chapter gets its number at the end, as in the trash
func TestPlanMergeFolderChapterName(t *testing.T) {
	root := t.TempDir()
	library := newLibrary(t)
	touch(t, filepath.Join(root, "Berserk", "[0001.0] One", "1.jpg"))
	touch(t, filepath.Join(root, "Berserk B", "[0001.0] One", "1.jpg"))
	touch(t, filepath.Join(root, "Berserk", "[0001.0] One.cbz"))
	record(t, library, root, "Berserk", "[0001.0] One.cbz", 1, "a")
	if err := library.RecordDownload(root, "Berserk B", storage.Download{Path: filepath.Join(root, "Berserk B", "[0001.0] One"), Provider: "b", MangaID: "b",
		MangaTitle: "Berserk B", Chapter: 1, Format: "IMAGES"}); err != nil {
		t.Fatal(err)
	}
	keep, _, _ := library.MangaByFolder(root, "Berserk")
	other, _, _ := library.MangaByFolder(root, "Berserk B")
	plan, err := (Files{FS: afero.NewOsFs(), Library: library, Root: root}).PlanMerge(keep, other)
	if err != nil || len(plan.Moves) != 1 {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	if got := filepath.Base(plan.Moves[0].To); got != "[0001.0] One (2)" {
		t.Errorf("to %q", got)
	}
}

// a download of the other manga after the plan: the merge refuses, nothing is forgotten
func TestApplyMergeRefusesNewDownloads(t *testing.T) {
	root := t.TempDir()
	library := newLibrary(t)
	touch(t, filepath.Join(root, "Berserk", "1.cbz"))
	touch(t, filepath.Join(root, "Berserk B", "2.cbz"))
	record(t, library, root, "Berserk", "1.cbz", 1, "a")
	record(t, library, root, "Berserk B", "2.cbz", 2, "b")
	keep, _, _ := library.MangaByFolder(root, "Berserk")
	other, _, _ := library.MangaByFolder(root, "Berserk B")
	files := Files{FS: afero.NewOsFs(), Library: library, Root: root, Modes: DefaultModes}
	plan, err := files.PlanMerge(keep, other)
	if err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(root, "Berserk B", "3.cbz"))
	record(t, library, root, "Berserk B", "3.cbz", 3, "b")
	if err := files.ApplyMerge(plan); err == nil || !strings.Contains(err.Error(), "3.cbz") {
		t.Fatalf("merged: %v", err)
	}
	if left, _ := library.Downloads(other.ID); len(left) != 2 {
		t.Errorf("other has %d downloads", len(left))
	}
	if _, err := os.Stat(filepath.Join(root, "Berserk B", "2.cbz")); err != nil {
		t.Error("moved before refusing")
	}
}

// a failed move of a folder without a .libmangal.json removes the one it wrote
func TestMoveUndoRemovesNewMarker(t *testing.T) {
	root := t.TempDir()
	m := newMover(t, root)
	dir := filepath.Join(root, "Asura", "Berserk")
	touch(t, filepath.Join(dir, "1.cbz"))
	record(t, m.Library, filepath.Dir(dir), "Berserk", "1.cbz", 1, "asura")
	manga, _, _ := m.Library.MangaBySource("asura", "berserk")
	plan, err := m.PlanMove(context.Background(), manga, libmangal.ProviderInfo{ID: "pill", Name: "Mangapill"}, &dirManga{info: mangadata.MangaInfo{ID: "1", Title: "Berserk"}}, MoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Old != nil || plan.Marker == nil {
		t.Fatalf("plan %+v", plan)
	}
	plan.Manga.ID = 999
	if err := m.ApplyMove(plan); err == nil {
		t.Fatal("no error")
	}
	if _, err := os.Stat(filepath.Join(dir, libmangal.FilenameMangaDirInfo)); !os.IsNotExist(err) {
		t.Errorf("marker left: %v", err)
	}
}

// two folders that would get the same name: the second is skipped in the plan
func TestPlanDuplicateTargets(t *testing.T) {
	fsys := afero.NewMemMapFs()
	n := Namer{
		FS:           fsys,
		ProviderName: func(p libmangal.ProviderInfo) string { return p.Name },
		MangaName:    func(_ libmangal.ProviderInfo, m mangadata.Manga) string { return m.Info().Title },
	}
	writeInfo(t, fsys, "/lib/Berserk 1", libmangal.MangaDirInfo{Provider: "p", Manga: mangadata.MangaInfo{ID: "1", Title: "Berserk"}})
	writeInfo(t, fsys, "/lib/Berserk 2", libmangal.MangaDirInfo{Provider: "q", Manga: mangadata.MangaInfo{ID: "2", Title: "Berserk"}})
	renames, err := n.Plan(context.Background(), "/lib", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(renames) != 2 || renames[0].Skip != "" || !strings.Contains(renames[1].Skip, "Berserk 1 would get that name too") {
		t.Fatalf("got %+v", renames)
	}
}

// links whose library change fails: the folder is renamed back and its marker restored
func TestApplyLinksRollsBack(t *testing.T) {
	root := t.TempDir()
	dex := filepath.Join(root, "mango-mangadex")
	l := newLinker(t, root, map[string]metadata.Metadata{"74347": testMeta("One-Punch Man", "74347", "44347")})
	folder := filepath.Join(dex, "OPM (2012) [alid-1]")
	touch(t, filepath.Join(folder, "1.cbz"))
	writeMarker(t, folder, libmangal.MangaDirInfo{Provider: "mango-mangadex", Manga: mangadata.MangaInfo{ID: "opm", Title: "OPM"}, MetadataIDs: []metadata.ID{alID("1")}})
	before, err := os.ReadFile(filepath.Join(folder, libmangal.FilenameMangaDirInfo))
	if err != nil {
		t.Fatal(err)
	}
	record(t, l.Library, dex, filepath.Base(folder), "1.cbz", 1, "mango-mangadex")
	manga, _, _ := l.Library.MangaByFolder(dex, filepath.Base(folder))
	plan, err := l.PlanLinks(context.Background(), manga, Links{metadata.IDCodeAnilist: "74347"}, PlanOptions{})
	if err != nil || plan.Marker == nil || plan.Rename == nil || plan.Rename.Skip != "" {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	plan.Info.Manga.ID = 999
	if err := l.ApplyLinks(plan, LinkOptions{Rename: true}); err == nil {
		t.Fatal("no error")
	}
	if _, err := os.Stat(plan.Rename.To); !os.IsNotExist(err) {
		t.Errorf("renamed folder left: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(folder, libmangal.FilenameMangaDirInfo)); err != nil || string(got) != string(before) {
		t.Errorf("marker %q, %v", got, err)
	}
}
