package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

func TestDelete(t *testing.T) {
	root := t.TempDir()
	lib := newLibrary(t)
	fsys := afero.NewOsFs()
	berserk := filepath.Join(root, "Berserk")
	touch(t, filepath.Join(berserk, "Vol. 1", "[0001.0] One.cbz"))
	touch(t, filepath.Join(berserk, "[0002.0] Two.cbz"))
	record(t, lib, root, "Berserk", filepath.Join("Vol. 1", "[0001.0] One.cbz"), 1, "p")
	record(t, lib, root, "Berserk", "[0002.0] Two.cbz", 2, "p")
	m, _, _ := lib.MangaByFolder(root, "Berserk")
	downloads, _ := lib.Downloads(m.ID)

	// a chapter: to the trash, and its now empty volume folder goes
	files := Files{FS: fsys, Library: lib, Root: root, Modes: DefaultModes}
	if err := files.DeleteDownload(m, downloads[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(berserk, "Vol. 1")); !os.IsNotExist(err) {
		t.Errorf("empty volume folder left: %v", err)
	}
	if left, _ := lib.Downloads(m.ID); len(left) != 1 {
		t.Errorf("downloads left %v, want 1", left)
	}

	// the manga: its folder to the trash, gone from the library
	if err := files.DeleteManga(m); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(berserk); !os.IsNotExist(err) {
		t.Errorf("folder left: %v", err)
	}
	if _, ok, _ := lib.MangaByID(m.ID); ok {
		t.Error("still in the library")
	}
	batches, err := Trash(fsys, root)
	if err != nil || len(batches) == 0 {
		t.Fatalf("trash: %v, %v", batches, err)
	}
	trashed := 0
	for _, b := range batches {
		trashed += b.Files
	}
	if trashed != 2 {
		t.Errorf("%d files in the trash, want 2", trashed)
	}
}
