package library

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/afero"
)

// otherDrive fails every rename like across drives.
type otherDrive struct {
	afero.Fs
}

func (f otherDrive) Rename(from, to string) error {
	return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
}

func TestMoveFileOtherDrive(t *testing.T) {
	dir := t.TempDir()
	fsys := otherDrive{Fs: afero.NewOsFs()}
	touch(t, filepath.Join(dir, "a", "Vol. 1", "x.cbz"))
	if err := moveFile(fsys, filepath.Join(dir, "a"), filepath.Join(dir, "b", "a"), DefaultModes); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b", "a", "Vol. 1", "x.cbz")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); !os.IsNotExist(err) {
		t.Errorf("original left: %v", err)
	}
}

func TestTrashNeverOverwrites(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	outside := t.TempDir()
	fsys := afero.NewOsFs()
	// the same name twice in a batch, and two outside-root files with the same name
	for i, path := range []string{
		filepath.Join(root, "M", "a.cbz"),
		filepath.Join(root, "M", "a.cbz"),
		filepath.Join(outside, "X", "c.cbz"),
		filepath.Join(outside, "Y", "c.cbz"),
	} {
		touch(t, path)
		if err := os.WriteFile(path, []byte{byte('0' + i)}, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := moveToTrash(fsys, root, "batch", path, DefaultModes); err != nil {
			t.Fatal(err)
		}
	}
	batch := filepath.Join(root, TrashDir, "batch")
	for path, want := range map[string]string{
		filepath.Join(batch, "M", "a.cbz"):     "0",
		filepath.Join(batch, "M", "a (2).cbz"): "1",
		filepath.Join(batch, "X", "c.cbz"):     "2",
		filepath.Join(batch, "Y", "c.cbz"):     "3",
	} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("%s: %q, %v, want %q", path, got, err, want)
		}
	}
}

func TestApplyMergeFailsMidway(t *testing.T) {
	root := t.TempDir()
	library := newLibrary(t)
	for _, f := range []string{"Ch 2.cbz", "Ch 3.cbz", "Ch 4.cbz"} {
		touch(t, filepath.Join(root, "Berserk B", f))
	}
	touch(t, filepath.Join(root, "Berserk", "[0001.0] One.cbz"))
	record(t, library, root, "Berserk", "[0001.0] One.cbz", 1, "a")
	record(t, library, root, "Berserk B", "Ch 2.cbz", 2, "b")
	record(t, library, root, "Berserk B", "Ch 3.cbz", 3, "b")
	record(t, library, root, "Berserk B", "Ch 4.cbz", 4, "b")
	keep, _, _ := library.MangaByFolder(root, "Berserk")
	other, _, _ := library.MangaByFolder(root, "Berserk B")

	files := Files{FS: afero.NewOsFs(), Library: library, Root: root, Modes: DefaultModes}
	plan, err := files.PlanMerge(keep, other)
	if err != nil || len(plan.Moves) != 3 {
		t.Fatalf("plan %+v, %v", plan, err)
	}
	// moves go in order, the second one fails
	files.FS = failingRename{Fs: afero.NewOsFs(), fail: "Ch 3.cbz"}
	err = files.ApplyMerge(plan)
	if err == nil || !strings.Contains(err.Error(), "library scan") {
		t.Fatalf("got %v, want an error saying to scan", err)
	}
	// the library matches the files: chapter 2 moved, 3 and 4 still in the other manga
	if ok, _ := library.HasDownload(filepath.Join(root, "Berserk", "Ch 2.cbz")); !ok {
		t.Error("the moved file isn't recorded where it is")
	}
	if ok, _ := library.HasDownload(filepath.Join(root, "Berserk B", "Ch 3.cbz")); !ok {
		t.Error("the file that didn't move isn't recorded where it is")
	}
	if left, _ := library.Downloads(other.ID); len(left) != 2 {
		t.Errorf("other has %d downloads, want 2", len(left))
	}
}

// failingRename fails renames from a path containing fail.
type failingRename struct {
	afero.Fs
	fail string
}

func (f failingRename) Rename(from, to string) error {
	if strings.Contains(from, f.fail) {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EACCES}
	}
	return f.Fs.Rename(from, to)
}

func TestPlanMergeKeepInsideOther(t *testing.T) {
	root := t.TempDir()
	library := newLibrary(t)
	touch(t, filepath.Join(root, "Berserk", "Extra", "Ch 1.cbz"))
	touch(t, filepath.Join(root, "Berserk", "Ch 2.cbz"))
	record(t, library, filepath.Join(root, "Berserk"), "Extra", "Ch 1.cbz", 1, "a")
	record(t, library, root, "Berserk", "Ch 2.cbz", 2, "b")
	keep, _, _ := library.MangaByFolder(filepath.Join(root, "Berserk"), "Extra")
	other, _, _ := library.MangaByFolder(root, "Berserk")
	if _, err := (Files{FS: afero.NewOsFs(), Library: library, Root: root}).PlanMerge(keep, other); err == nil {
		t.Error("planned trashing the folder keep is in")
	}
}
