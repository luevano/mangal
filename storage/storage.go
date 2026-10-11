// Package storage keeps mangal's data in SQLite: a cache database (deleting it only loses
// cache) and a data database (title bindings, logins, search history, the library index).
//
// Both are opened once per process in WAL mode, so several mangal processes can use them
// at the same time.
package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	// FilenameCache is the cache database, in the cache directory.
	FilenameCache = "cache.sqlite"
	// FilenameData is the data database, in the data directory.
	FilenameData = "mangal.sqlite"
)

// Cache tables, one per kind of data. They all have the kv shape (see kvTable).
const (
	TableMangaSearches    = "manga_searches"
	TableVolumes          = "volumes"
	TableChapters         = "chapters"
	TableMetadataSearches = "metadata_searches"
	TableMetadata         = "metadata"
	TableLuaHTTP          = "lua_http"
	TableOther            = "other"
)

// Data tables with the kv shape.
const (
	TableTitleBindings = "title_bindings"
	TableAuthTokens    = "auth_tokens"
	TableAuthHistory   = "auth_history"
	TableSearchHistory = "search_history"
)

// CacheTables are every cache table.
var CacheTables = []string{
	TableMangaSearches, TableVolumes, TableChapters,
	TableMetadataSearches, TableMetadata, TableLuaHTTP, TableOther,
}

// kvTable is a table of gob encoded values by owner (a provider id, a metadata provider, ...)
// and key. expires is a unix time, 0 for never.
func kvTable(name string) string {
	return `CREATE TABLE ` + name + ` (
	owner   TEXT NOT NULL,
	key     TEXT NOT NULL,
	value   BLOB NOT NULL,
	expires INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (owner, key)
) WITHOUT ROWID`
}

// Migration changes the schema of a database, in a transaction.
type Migration func(tx *sql.Tx) error

// sqlMigration runs the statements.
func sqlMigration(statements string) Migration {
	return func(tx *sql.Tx) error {
		_, err := tx.Exec(statements)
		return err
	}
}

// cacheMigrations are applied in order, the database's user_version is how many were.
var cacheMigrations = []Migration{sqlMigration(
	kvTable(TableMangaSearches) + ";" +
		kvTable(TableVolumes) + ";" +
		kvTable(TableChapters) + ";" +
		kvTable(TableMetadataSearches) + ";" +
		kvTable(TableMetadata) + ";" +
		kvTable(TableLuaHTTP) + ";" +
		kvTable(TableOther)),
}

var dataMigrations = []Migration{
	sqlMigration(kvTable(TableTitleBindings) + ";" +
		kvTable(TableAuthTokens) + ";" +
		kvTable(TableAuthHistory) + ";" +
		kvTable(TableSearchHistory) + ";" +
		`CREATE TABLE manga_dirs (
	parent   TEXT NOT NULL,
	name     TEXT NOT NULL,
	provider TEXT NOT NULL,
	manga_id TEXT NOT NULL,
	info     BLOB NOT NULL,
	updated  INTEGER NOT NULL,
	PRIMARY KEY (parent, name)
);
CREATE INDEX manga_dirs_provider ON manga_dirs (provider)`),
	sqlMigration(`CREATE TABLE downloads (
	path          TEXT PRIMARY KEY,
	provider      TEXT NOT NULL,
	manga_id      TEXT NOT NULL,
	manga_title   TEXT NOT NULL,
	volume        REAL NOT NULL,
	chapter       REAL NOT NULL,
	chapter_title TEXT NOT NULL,
	chapter_url   TEXT NOT NULL,
	format        TEXT NOT NULL,
	pages         INTEGER NOT NULL,
	downloaded    INTEGER NOT NULL
);
CREATE INDEX downloads_manga ON downloads (provider, manga_id)`),
	migrateLibrary,
}

// DB is an open SQLite database.
type DB struct {
	*sql.DB
	Path string
}

// Open opens (creating it if needed) the database at path and applies the migrations.
func Open(path string, migrations []Migration) (*DB, error) {
	return open(path, migrations, "NORMAL")
}

// open with the synchronous mode: NORMAL can lose the last commits on an OS crash (never
// corrupts), FULL syncs every commit.
func open(path string, migrations []Migration, synchronous string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// immediate transactions take the write lock when they start, so two processes can't both
	// read a version (or row) and then write based on it
	dsn := "file:" + escapeURIPath(path) + "?_txlock=immediate&_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(" + synchronous + ")&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// writes are serialized anyway, one connection avoids busy errors inside the process
	sqlDB.SetMaxOpenConns(1)

	db := &DB{DB: sqlDB, Path: path}
	if err := db.migrate(migrations); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return db, nil
}

// escapeURIPath escapes what SQLite's URI file names would read as the query, the fragment
// or an escape.
func escapeURIPath(path string) string {
	return strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
}

// InTx runs f in a transaction, committed when f returns nil, rolled back otherwise.
func (db *DB) InTx(f func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// querier is a *DB or a *sql.Tx.
type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// queryAll runs the query and scans every row.
func queryAll[T any](q querier, scan func(*sql.Rows) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, v)
	}
	return all, rows.Err()
}

func (db *DB) migrate(migrations []Migration) error {
	busy := 0
	for {
		done, err := db.migrateOne(migrations)
		// a brand new file can be busy without waiting (busy_timeout doesn't apply) while
		// another process switches it to WAL; retrying is safe, the version is read again
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && sqliteErr.Code()&0xff == sqlite3.SQLITE_BUSY && busy < 50 {
			busy++
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if err != nil || done {
			return err
		}
	}
}

// migrateOne applies the next migration, if any. The version is read inside the
// transaction, another process may have migrated while this one waited for the lock.
func (db *DB) migrateOne(migrations []Migration) (done bool, err error) {
	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil || done {
			_ = tx.Rollback()
		}
	}()
	var version int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return false, err
	}
	if version > len(migrations) {
		return false, fmt.Errorf("schema version %d is newer than this mangal's %d", version, len(migrations))
	}
	if version == len(migrations) {
		return true, nil
	}
	if err := migrations[version](tx); err != nil {
		return false, fmt.Errorf("migration %d: %w", version+1, err)
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version+1)); err != nil {
		return false, err
	}
	return false, tx.Commit()
}

// OpenCache opens the cache database and removes its expired entries.
func OpenCache(path string) (*DB, error) {
	db, err := Open(path, cacheMigrations)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	err = db.InTx(func(tx *sql.Tx) error {
		for _, table := range CacheTables {
			if _, err := tx.Exec(`DELETE FROM `+table+` WHERE expires != 0 AND expires < ?`, now); err != nil {
				return fmt.Errorf("prune %s: %w", table, err)
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// OpenData opens the data database, only readable by the user: it has login tokens.
func OpenData(path string) (*DB, error) {
	if err := makePrivate(path); err != nil {
		return nil, err
	}
	// logins and the library: worth a sync per commit, WSL crashes do happen
	return open(path, dataMigrations, "FULL")
}

// makePrivate makes the database (and its directory) only accessible by the user,
// SQLite creates the -wal and -shm files with the database's permissions.
func makePrivate(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return err
	}
	_ = f.Close()
	for _, file := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(file, 0o600); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// ClearCache deletes the cache entries of a provider (or metadata provider) id, or all of
// them if the id is empty. It returns how many were deleted.
func ClearCache(cache *DB, id string) (int64, error) {
	var total int64
	for _, table := range CacheTables {
		query, args := `DELETE FROM `+table, []any{}
		if id != "" {
			// other's owners are "<id>/<bucket>"
			query, args = query+` WHERE owner = ? OR owner LIKE ?`, []any{id, id + "/%"}
		}
		res, err := cache.Exec(query, args...)
		if err != nil {
			return total, fmt.Errorf("clear %s: %w", table, err)
		}
		n, _ := res.RowsAffected()
		total += n
	}
	return total, nil
}
