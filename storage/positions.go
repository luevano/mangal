package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// readerPositionsTable: the page the web reader is on per chapter, by the reader's chapter
// key, so another device resumes there.
const readerPositionsTable = `CREATE TABLE reader_positions (
	chapter TEXT PRIMARY KEY,
	page    INTEGER NOT NULL,
	updated INTEGER NOT NULL
)`

// ReaderPosition is the page a chapter was left on in the web reader.
type ReaderPosition struct {
	// Chapter is the reader's chapter key.
	Chapter string
	// Page is the page index, 0 the first.
	Page    int
	Updated time.Time
}

// ReaderPositions are the web reader's page positions of a user (empty: the server's own,
// as History).
type ReaderPositions struct {
	DB   *DB
	User string
}

// Get is the chapter's position, found false if none was saved.
func (p ReaderPositions) Get(chapter string) (pos ReaderPosition, found bool, err error) {
	var updated int64
	err = p.DB.QueryRow(`SELECT page, updated FROM reader_positions WHERE user = ? AND chapter = ?`, p.User, chapter).Scan(&pos.Page, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ReaderPosition{}, false, nil
	}
	if err != nil {
		return ReaderPosition{}, false, fmt.Errorf("reader position of %q: %w", chapter, err)
	}
	pos.Chapter, pos.Updated = chapter, time.UnixMilli(updated)
	return pos, true, nil
}

// Set saves the position unless a newer one is saved (another device moved on since).
func (p ReaderPositions) Set(pos ReaderPosition) error {
	if pos.Updated.IsZero() {
		pos.Updated = time.Now()
	}
	_, err := p.DB.Exec(`INSERT INTO reader_positions (user, chapter, page, updated) VALUES (?, ?, ?, ?)
ON CONFLICT (user, chapter) DO UPDATE SET page = excluded.page, updated = excluded.updated WHERE excluded.updated >= reader_positions.updated`,
		p.User, pos.Chapter, pos.Page, pos.Updated.UnixMilli())
	if err != nil {
		return fmt.Errorf("save the reader position of %q: %w", pos.Chapter, err)
	}
	p.DB.touch()
	return nil
}
