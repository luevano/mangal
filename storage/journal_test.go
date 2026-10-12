package storage

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestSnapshotRestore(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenData(filepath.Join(dir, FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := Storage{Data: db}
	lib := st.Library()
	if _, err := db.Exec(`INSERT INTO library_mangas (title, parent, dir) VALUES ('A', '/lib', 'A')`); err != nil {
		t.Fatal(err)
	}
	auth := KV{DB: db, Table: TableAuthTokens, Owner: "x"}
	if err := auth.Set("k", 1); err != nil {
		t.Fatal(err)
	}

	backups := filepath.Join(dir, "backups")
	snap, err := db.Snapshot(backups, "rename")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(snap); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot %v, %v", info, err)
	}
	check, err := sql.Open("sqlite", snap)
	if err != nil {
		t.Fatal(err)
	}
	var result string
	if err := check.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil || result != "ok" {
		t.Fatalf("integrity %q, %v", result, err)
	}
	check.Close()

	j := st.Journal()
	op, err := j.Begin("rename", "rename 1 folder", snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := op.Record(Step{Kind: StepMove, Path: "/lib/A", Dest: "/lib/B"}); err != nil {
		t.Fatal(err)
	}
	if err := op.Record(Step{Kind: StepWrite, Path: "/lib/B/x", Old: []byte{}, New: []byte("n")}); err != nil {
		t.Fatal(err)
	}
	if err := lib.Moved("/lib/A", "/lib/B"); err != nil {
		t.Fatal(err)
	}
	if err := op.Finish(nil); err != nil {
		t.Fatal(err)
	}

	last, ok, err := j.Last()
	if err != nil || !ok || last.ID != op.ID || last.Steps != 2 || last.Finished.IsZero() {
		t.Fatalf("last %+v, %t, %v", last, ok, err)
	}
	steps, err := j.Steps(last)
	if err != nil || len(steps) != 2 || steps[0].Old != nil || steps[1].Old == nil || string(steps[1].New) != "n" {
		t.Fatalf("steps %+v, %v", steps, err)
	}
	if changed, err := j.Changed(last); err != nil || len(changed) != 0 {
		t.Fatalf("changed %v, %v", changed, err)
	}

	// a login since is kept, a change to the library is told
	if err := auth.Set("k", 2); err != nil {
		t.Fatal(err)
	}
	if changed, err := j.Changed(last); err != nil || len(changed) != 0 {
		t.Fatalf("changed after a login %v, %v", changed, err)
	}
	if _, err := db.Exec(`INSERT INTO library_mangas (title, parent, dir) VALUES ('C', '/lib', 'C')`); err != nil {
		t.Fatal(err)
	}
	if changed, err := j.Changed(last); err != nil || !slices.Contains(changed, "library_mangas") {
		t.Fatalf("changed after an add %v, %v", changed, err)
	}

	if err := j.Restore(last); err != nil {
		t.Fatal(err)
	}
	mangas, err := lib.Mangas()
	if err != nil || len(mangas) != 1 || mangas[0].Path() != "/lib/A" {
		t.Fatalf("restored %+v, %v", mangas, err)
	}
	var v int
	if found, err := auth.Get("k", &v); err != nil || !found || v != 2 {
		t.Errorf("login %d, %t, %v: should be kept", v, found, err)
	}
	if _, ok, _ := j.Last(); ok {
		t.Error("the operation isn't undone")
	}
	if op, _, _ := j.Op(last.ID); op.Undone.IsZero() {
		t.Error("no undone time")
	}
}

func TestNotFinished(t *testing.T) {
	db, err := OpenData(filepath.Join(t.TempDir(), FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	j := Journal{DB: db}
	if _, err := j.Begin("merge", "merge", "/x.sqlite"); err != nil {
		t.Fatal(err)
	}
	op, _, _ := j.Last()
	if _, err := j.Changed(op); !errors.Is(err, ErrNotFinished) {
		t.Errorf("changed of an unfinished operation: %v", err)
	}
	if err := j.Restore(op); err == nil {
		t.Error("restored a missing snapshot")
	}
}

func TestPruneSnapshots(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenData(filepath.Join(dir, FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	backups := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	// not a snapshot, stays
	if err := os.WriteFile(filepath.Join(backups, "notes.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var made []string
	for range 4 {
		snap, err := db.Snapshot(backups, "merge")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := PruneSnapshots(backups, 2); err != nil {
			t.Fatal(err)
		}
		made = append(made, snap)
		// the same second: numbered, and in order by time
		time.Sleep(10 * time.Millisecond)
	}
	snaps, err := Snapshots(backups)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(snaps, made[2:]) {
		t.Errorf("kept %v, want %v", snaps, made[2:])
	}
	if _, err := os.Stat(filepath.Join(backups, "notes.txt")); err != nil {
		t.Error(err)
	}
}

func TestSetSummary(t *testing.T) {
	db, err := OpenData(filepath.Join(t.TempDir(), FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	j := Journal{DB: db}
	op, err := j.Begin("rename", "rename 14 folders", "/x.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	op.SetSummary("rename 12 folders")
	if err := op.Finish(nil); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := j.Last(); got.Summary != "rename 12 folders" {
		t.Errorf("summary %q", got.Summary)
	}
}
