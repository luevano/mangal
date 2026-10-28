package core

import (
	"context"

	"github.com/luevano/mangal/v5/storage"
)

// Journal is the library's operations, the applied Changes; undoing one is a Change
// (UndoReq).
type Journal interface {
	// Ops are the operations, newest first (limit 0: all).
	Ops(ctx context.Context, limit int) ([]storage.Op, error)
}

type localJournal struct {
	core *Core
}

func (j *localJournal) Ops(_ context.Context, limit int) ([]storage.Op, error) {
	return j.core.env.Storage.Journal().Ops(limit)
}
