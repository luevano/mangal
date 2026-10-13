package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// journaled runs do as an operation of a fresh data database, with a snapshot.
func journaled(t *testing.T, do func(j Journal) error) Undoer {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.OpenData(filepath.Join(dir, storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	snap, err := db.Snapshot(filepath.Join(dir, "backups"), "test")
	if err != nil {
		t.Fatal(err)
	}
	j := storage.Journal{DB: db}
	op, err := j.Begin("test", "a test", snap)
	if err != nil {
		t.Fatal(err)
	}
	err = do(op)
	if err := op.Finish(err); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	return Undoer{FS: afero.NewOsFs(), Journal: j, Modes: DefaultModes}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestUndoRename(t *testing.T) {
	root := t.TempDir()
	fsys := afero.NewOsFs()
	touch(t, filepath.Join(root, "Old", "A", "1.cbz"))
	touch(t, filepath.Join(root, "Old", "B", "1.cbz"))
	if err := os.MkdirAll(filepath.Join(root, "New"), 0o755); err != nil {
		t.Fatal(err)
	}
	renames := []Rename{
		// into a new folder of a new parent
		{From: filepath.Join(root, "Old", "A"), To: filepath.Join(root, "Deep", "Er", "A")},
		// onto an empty folder (removed first)
		{From: filepath.Join(root, "Old", "B"), To: filepath.Join(root, "New")},
		{From: filepath.Join(root, "Old"), RemoveEmpty: true},
	}
	u := journaled(t, func(j Journal) error {
		return ApplyEach(fsys, j, renames, nil, func(_ int, err error) error { return err })
	})
	if !exists(filepath.Join(root, "Deep", "Er", "A", "1.cbz")) || exists(filepath.Join(root, "Old")) {
		t.Fatal("not renamed")
	}

	plan, err := u.Plan(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Refusal(false); err != nil {
		t.Fatalf("refused: %v\n%s", err, strings.Join(plan.Lines(func(s string) string { return s }), "\n"))
	}
	res, err := u.Apply(plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Restored || len(res.Skipped) != 0 {
		t.Errorf("result %+v", res)
	}
	for _, path := range []string{
		filepath.Join(root, "Old", "A", "1.cbz"),
		filepath.Join(root, "Old", "B", "1.cbz"),
		filepath.Join(root, "New"),
	} {
		if !exists(path) {
			t.Errorf("%s not back", path)
		}
	}
	if exists(filepath.Join(root, "Deep")) {
		t.Error("the folders the rename made are left")
	}
	if _, err := u.Plan(0); err != ErrNothingToUndo {
		t.Errorf("undone twice: %v", err)
	}
}

func TestUndoNeverOverwrites(t *testing.T) {
	root := t.TempDir()
	fsys := afero.NewOsFs()
	from, to := filepath.Join(root, "A"), filepath.Join(root, "B")
	touch(t, filepath.Join(from, "1.cbz"))
	u := journaled(t, func(j Journal) error { return moveFile(fsys, j, from, to, DefaultModes) })
	// something new where it was
	touch(t, filepath.Join(from, "new.cbz"))

	plan, err := u.Plan(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Blocked()) != 1 || plan.Refusal(false) == nil {
		t.Fatalf("not blocked: %+v", plan.Steps)
	}
	if _, err := u.Apply(plan, false); err == nil {
		t.Fatal("applied without force")
	}
	res, err := u.Apply(plan, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skipped) != 1 || !exists(filepath.Join(to, "1.cbz")) || !exists(filepath.Join(from, "new.cbz")) {
		t.Errorf("result %+v", res)
	}
}

func TestUndoMarker(t *testing.T) {
	dir := t.TempDir()
	fsys := afero.NewOsFs()
	marker := filepath.Join(dir, libmangal.FilenameMangaDirInfo)
	info := libmangal.MangaDirInfo{Provider: "p"}
	info.Manga.ID = "1"
	info.Manga.Title = "T"

	// a new marker is removed
	u := journaled(t, func(j Journal) error { return writeDirInfo(fsys, j, dir, info, 0o644) })
	plan, err := u.Plan(0)
	if err != nil || len(plan.Steps) != 1 || plan.Steps[0].Action != UndoRemove {
		t.Fatalf("plan %+v, %v", plan.Steps, err)
	}
	if _, err := u.Apply(plan, false); err != nil || exists(marker) {
		t.Fatalf("marker left: %v", err)
	}

	// a rewritten one gets its content back, unless it changed since
	if err := os.WriteFile(marker, []byte(`{"provider":"old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	u = journaled(t, func(j Journal) error { return writeDirInfo(fsys, j, dir, info, 0o644) })
	if err := os.WriteFile(marker, []byte(`{"provider":"download"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if plan, _ := u.Plan(0); len(plan.Blocked()) != 1 {
		t.Fatalf("changed marker not blocked: %+v", plan.Steps)
	}
	if err := libmangal.WriteMangaDirInfo(fsys, dir, info, 0o644); err != nil {
		t.Fatal(err)
	}
	plan, _ = u.Plan(0)
	if _, err := u.Apply(plan, false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(marker); string(data) != `{"provider":"old"}` {
		t.Errorf("marker %s", data)
	}
}

func TestUndoTrash(t *testing.T) {
	root := t.TempDir()
	fsys := afero.NewOsFs()
	touch(t, filepath.Join(root, "M", "Vol. 1", "a.cbz"))
	touch(t, filepath.Join(root, "M", "Vol. 1", "b.cbz"))
	u := journaled(t, func(j Journal) error {
		// the folder joins its chapter already in the batch
		if err := moveToTrash(fsys, j, root, "batch", filepath.Join(root, "M", "Vol. 1", "a.cbz"), DefaultModes); err != nil {
			return err
		}
		return moveToTrash(fsys, j, root, "batch", filepath.Join(root, "M", "Vol. 1"), DefaultModes)
	})
	plan, err := u.Plan(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Apply(plan, false); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(root, "M", "Vol. 1", "a.cbz")) || !exists(filepath.Join(root, "M", "Vol. 1", "b.cbz")) {
		t.Error("not back from the trash")
	}
	if exists(filepath.Join(root, TrashDir)) {
		t.Error("trash folders left")
	}
}

func TestNeedTrash(t *testing.T) {
	root := t.TempDir()
	fsys := afero.NewOsFs()
	touch(t, filepath.Join(root, "M", "a.cbz"))
	u := journaled(t, func(j Journal) error {
		return moveToTrash(fsys, j, root, "batch", filepath.Join(root, "M", "a.cbz"), DefaultModes)
	})
	need, err := u.NeedTrash(root)
	if err != nil || len(need) != 1 || need[0].Kind != "test" {
		t.Fatalf("need %+v, %v", need, err)
	}
	plan, _ := u.Plan(0)
	if _, err := u.Apply(plan, false); err != nil {
		t.Fatal(err)
	}
	if need, err := u.NeedTrash(root); err != nil || len(need) != 0 {
		t.Errorf("after the undo: %+v, %v", need, err)
	}
}
