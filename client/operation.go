package client

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cache"
)

// BackupsDir is where the snapshots of the data database go.
func BackupsDir() string {
	return filepath.Join(path.Data(), "backups")
}

// Snapshot copies the data database into BackupsDir before the change named op,
// keeping the newest data.backups snapshots (the journaled operations of the deleted
// ones can't be undone any more and leave the journal). An error means there's no
// snapshot: the change must not go ahead.
func Snapshot(op string) (string, error) {
	snapshot, err := snapshot(op)
	if err != nil {
		return "", err
	}
	pruneSnapshots()
	return snapshot, nil
}

func snapshot(op string) (string, error) {
	st, err := cache.Storage()
	if err != nil {
		return "", err
	}
	snapshot, err := st.Data.Snapshot(BackupsDir(), op)
	if err != nil {
		return "", fmt.Errorf("snapshot the database before the %s: %w", op, err)
	}
	return snapshot, nil
}

// pruneSnapshots deletes the snapshots past data.backups, and their operations.
func pruneSnapshots() {
	st, err := cache.Storage()
	if err != nil {
		return
	}
	removed, err := storage.PruneSnapshots(BackupsDir(), config.Data.Backups.Get())
	if err != nil {
		log.Warn("delete old snapshots: %s", err)
	}
	if err := st.Journal().Forget(removed); err != nil {
		log.Warn("%s", err)
	}
}

// Operation runs do as one library operation that can be undone ("mangal library
// undo"): the data database is snapshotted first (if that fails nothing is done), and
// the file changes do makes through the journal are recorded. It returns the snapshot.
func Operation(kind, summary string, do func(library.Journal) error) (string, error) {
	st, err := cache.Storage()
	if err != nil {
		return "", err
	}
	// before the snapshot: a write by others between them is lost by an undo too
	mark, err := st.Data.Mark()
	if err != nil {
		return "", err
	}
	snapshot, err := Snapshot(kind)
	if err != nil {
		return "", err
	}
	op, err := st.Journal().BeginAfter(mark, kind, summary, snapshot)
	if err != nil {
		return snapshot, err
	}
	err = do(op)
	if finishErr := op.Finish(err); err == nil {
		err = finishErr
	}
	return snapshot, err
}

// LibraryUndoer undoes library operations, with the configured modes.
func LibraryUndoer() (library.Undoer, error) {
	st, err := cache.Storage()
	if err != nil {
		return library.Undoer{}, err
	}
	return library.Undoer{FS: afs.Afero, Journal: st.Journal(), Modes: LibraryModes()}, nil
}

// Undo undoes the plan (see library.Undoer.Apply), after a snapshot of the database as
// it is now, so an undo can be undone by hand. The old snapshots are deleted after: the
// one restored may be the oldest.
func Undo(u library.Undoer, plan library.UndoPlan, force bool) (library.UndoResult, string, error) {
	return UndoContext(context.Background(), u, plan, force)
}

// UndoContext is Undo, stopped between steps when ctx is done.
func UndoContext(ctx context.Context, u library.Undoer, plan library.UndoPlan, force bool) (library.UndoResult, string, error) {
	if err := plan.Refusal(force); err != nil {
		return library.UndoResult{}, "", err
	}
	snapshot, err := snapshot("undo")
	if err != nil {
		return library.UndoResult{}, "", err
	}
	defer pruneSnapshots()
	res, err := u.ApplyContext(ctx, plan, force)
	return res, snapshot, err
}
