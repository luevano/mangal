package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
)

// ErrMangaNotFound is a library manga that isn't there.
var ErrMangaNotFound = errors.New("library manga not found")

// The hold marks: a manga whose watches are paused because it isn't publishing.
const (
	// HoldHiatus is a manga on hiatus (no chapters for a long time, it may come back).
	HoldHiatus = "hiatus"
	// HoldEnded is a manga no longer releasing (finished, cancelled, dropped).
	HoldEnded = "ended"
)

// HoldMarks are the valid hold marks.
var HoldMarks = []string{HoldHiatus, HoldEnded}

// ValidHold is true for a hold mark, or "" (none).
func ValidHold(hold string) bool {
	return hold == "" || slices.Contains(HoldMarks, hold)
}

// RelinkProvider is the RelinkKind of a watch that needs another provider.
const RelinkProvider = "provider"

// holdColumns (migration 22): the hold marks of the library mangas and of the watches
// without one, the library mangas that need another provider, the kind of a watch's
// re-link and what its last check found missing on the provider.
const holdColumns = `ALTER TABLE library_mangas ADD COLUMN hold TEXT NOT NULL DEFAULT '';
ALTER TABLE library_mangas ADD COLUMN new_provider TEXT NOT NULL DEFAULT '';
ALTER TABLE watches ADD COLUMN hold TEXT NOT NULL DEFAULT '';
ALTER TABLE watches ADD COLUMN relink_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE watches ADD COLUMN coverage TEXT NOT NULL DEFAULT ''`

// relinkKindColumn is what's stored: the kind, empty when no re-link is needed.
func (w Watch) relinkKindColumn() string {
	if !w.NeedsRelink {
		return ""
	}
	return w.RelinkKind
}

// RelinkLabel is what the watch needs: "needs a new provider" or "needs re-link", ""
// when it needs neither.
func (w Watch) RelinkLabel() string {
	switch {
	case !w.NeedsRelink:
		return ""
	case w.RelinkKind == RelinkProvider:
		return "needs a new provider"
	}
	return "needs re-link"
}

// SetHold sets the hold mark of library mangas, "" clears it.
func (l Library) SetHold(ids []int64, hold string) error {
	if !ValidHold(hold) {
		return fmt.Errorf("hold mark %q: not one of %v", hold, HoldMarks)
	}
	err := l.DB.InTx(func(tx *sql.Tx) error {
		for _, id := range ids {
			res, err := tx.Exec(`UPDATE library_mangas SET hold = ? WHERE id = ?`, hold, id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fmt.Errorf("library manga %d: %w", id, ErrMangaNotFound)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("set the hold of %d library mangas: %w", len(ids), err)
	}
	return nil
}

// SetNewProvider marks the library manga as needing another provider (reason), ""
// clears it.
func (l Library) SetNewProvider(id int64, reason string) error {
	res, err := l.DB.Exec(`UPDATE library_mangas SET new_provider = ? WHERE id = ?`, reason, id)
	if err != nil {
		return fmt.Errorf("mark library manga %d as needing a new provider: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("library manga %d: %w", id, ErrMangaNotFound)
	}
	return nil
}
