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
	"github.com/luevano/mangal/v5/util/cache"
)

// BackupsDir is where the snapshots of the data database go.
func BackupsDir() string {
	return filepath.Join(path.Data(), "backups")
}

// Ops runs the library operations of a storage: snapshots, journal and the library's
// lock. The zero BackupsDir and Keep are BackupsDir() and data.backups.
type Ops struct {
	Storage    storage.Storage
	BackupsDir string
	// Keep is how many snapshots are kept.
	Keep int
}

// DefaultOps are the operations of mangal's databases.
func DefaultOps() (Ops, error) {
	st, err := cache.Storage()
	return Ops{Storage: st}, err
}

func (o Ops) backupsDir() string {
	if o.BackupsDir != "" {
		return o.BackupsDir
	}
	return BackupsDir()
}

func (o Ops) keep() int {
	if o.Keep > 0 {
		return o.Keep
	}
	return config.Data.Backups.Get()
}

// Lock takes the library's lock for the operation kind, waiting for it (LockLibrary).
func (o Ops) Lock(ctx context.Context, kind string, wait bool, onWait func(LibraryBusy)) (context.Context, func(), error) {
	return LockLibrary(ctx, o.Storage.Watches(), kind, wait, onWait)
}

// Snapshot copies the data database into the backups directory before the change named
// op, keeping the newest snapshots (the journaled operations of the deleted ones can't be
// undone any more and leave the journal). An error means there's no snapshot: the change
// must not go ahead.
func (o Ops) Snapshot(op string) (string, error) {
	snapshot, err := o.snapshot(op)
	if err != nil {
		return "", err
	}
	o.prune()
	return snapshot, nil
}

func (o Ops) snapshot(op string) (string, error) {
	snapshot, err := o.Storage.Data.Snapshot(o.backupsDir(), op)
	if err != nil {
		return "", fmt.Errorf("snapshot the database before the %s: %w", op, err)
	}
	return snapshot, nil
}

// prune deletes the snapshots past Keep, and their operations.
func (o Ops) prune() {
	removed, err := storage.PruneSnapshots(o.backupsDir(), o.keep())
	if err != nil {
		log.Warn("delete old snapshots: %s", err)
	}
	if err := o.Storage.Journal().Forget(removed); err != nil {
		log.Warn("%s", err)
	}
}

// OpInfo is an operation that ran: its journal id and the snapshot taken before it.
type OpInfo struct {
	ID       int64
	Snapshot string
}

// Run runs do as one library operation that can be undone ("mangal library undo"),
// holding the library's lock (waited for, unless ctx has it): the data database is
// snapshotted first (if that fails nothing is done), and the file changes do makes
// through the journal are recorded.
func (o Ops) Run(ctx context.Context, kind, summary string, do func(library.Journal) error) (OpInfo, error) {
	ctx, unlock, err := o.Lock(ctx, kind, true, nil)
	if err != nil {
		return OpInfo{}, err
	}
	defer unlock()
	// before the snapshot: a write by others between them is lost by an undo too
	mark, err := o.Storage.Data.Mark()
	if err != nil {
		return OpInfo{}, err
	}
	snapshot, err := o.Snapshot(kind)
	if err != nil {
		return OpInfo{}, err
	}
	info := OpInfo{Snapshot: snapshot}
	op, err := o.Storage.Journal().BeginAfter(mark, kind, summary, snapshot)
	if err != nil {
		return info, err
	}
	info.ID = op.ID
	err = do(op)
	if finishErr := op.Finish(err); err == nil {
		err = finishErr
	}
	return info, err
}

// Undo undoes the plan (see library.Undoer.Apply) holding the library's lock, after a
// snapshot of the database as it is now, so an undo can be undone by hand. The old
// snapshots are deleted after: the one restored may be the oldest.
func (o Ops) Undo(ctx context.Context, u library.Undoer, plan library.UndoPlan, force bool) (library.UndoResult, string, error) {
	if err := plan.Refusal(force); err != nil {
		return library.UndoResult{}, "", err
	}
	ctx, unlock, err := o.Lock(ctx, "undo", true, nil)
	if err != nil {
		return library.UndoResult{}, "", err
	}
	defer unlock()
	snapshot, err := o.snapshot("undo")
	if err != nil {
		return library.UndoResult{}, "", err
	}
	defer o.prune()
	res, err := u.ApplyContext(ctx, plan, force)
	return res, snapshot, err
}

// Operation is Ops.Run of mangal's databases (the library's lock waited for). It returns
// the snapshot.
func Operation(kind, summary string, do func(library.Journal) error) (string, error) {
	o, err := DefaultOps()
	if err != nil {
		return "", err
	}
	info, err := o.Run(context.Background(), kind, summary, do)
	return info.Snapshot, err
}
