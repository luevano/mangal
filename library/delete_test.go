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
	if err := DeleteDownload(fsys, lib, root, m, downloads[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(berserk, "Vol. 1")); !os.IsNotExist(err) {
		t.Errorf("empty volume folder left: %v", err)
	}
	if left, _ := lib.Downloads(m.ID); len(left) != 1 {
		t.Errorf("downloads left %v, want 1", left)
	}

	// the manga: its folder to the trash, gone from the library
	if err := DeleteManga(fsys, lib, root, m); err != nil {
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
	files := 0
	for _, b := range batches {
		files += b.Files
	}
	if files != 2 {
		t.Errorf("%d files in the trash, want 2", files)
	}
}
