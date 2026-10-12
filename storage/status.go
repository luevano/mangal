package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// libraryStatus: the publication status of each library manga (AniList's: FINISHED,
// RELEASING, HIATUS, ...), from its metadata, kept past the metadata cache.
const libraryStatus = `ALTER TABLE library_mangas ADD COLUMN status TEXT NOT NULL DEFAULT '';
ALTER TABLE library_mangas ADD COLUMN status_updated INTEGER NOT NULL DEFAULT 0`

// SetStatuses sets the publication status of library mangas (by id), "" for unknown.
func (l Library) SetStatuses(statuses map[int64]string, at time.Time) error {
	if len(statuses) == 0 {
		return nil
	}
	err := l.DB.InTx(func(tx *sql.Tx) error {
		for id, status := range statuses {
			if _, err := tx.Exec(`UPDATE library_mangas SET status = ?, status_updated = ? WHERE id = ?`, status, unix(at), id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("set the status of %d library mangas: %w", len(statuses), err)
	}
	return nil
}
