// Package storage keeps mangal's data in SQLite: a cache database (deleting it only loses
// cache) and a data database (title bindings, logins, search history, the library index).
//
// Both are opened once per process in WAL mode, so several mangal processes can use them
// at the same time.
package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
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

// cacheMigrations are applied in order, the database's user_version is how many were.
var cacheMigrations = []string{
	kvTable(TableMangaSearches) + ";" +
		kvTable(TableVolumes) + ";" +
		kvTable(TableChapters) + ";" +
		kvTable(TableMetadataSearches) + ";" +
		kvTable(TableMetadata) + ";" +
		kvTable(TableLuaHTTP) + ";" +
		kvTable(TableOther),
}

var dataMigrations = []string{
	kvTable(TableTitleBindings) + ";" +
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
CREATE INDEX manga_dirs_provider ON manga_dirs (provider)`,
}

// DB is an open SQLite database.
type DB struct {
	*sql.DB
	Path string
}

// Open opens (creating it if needed) the database at path and applies the migrations.
func Open(path string, migrations []string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
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

func (db *DB) migrate(migrations []string) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("schema version %d is newer than this mangal's %d", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// OpenCache opens the cache database and removes its expired entries.
func OpenCache(path string) (*DB, error) {
	db, err := Open(path, cacheMigrations)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	for _, table := range CacheTables {
		if _, err := db.Exec(`DELETE FROM `+table+` WHERE expires != 0 AND expires < ?`, now); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("prune %s: %w", table, err)
		}
	}
	return db, nil
}

// OpenData opens the data database.
func OpenData(path string) (*DB, error) {
	return Open(path, dataMigrations)
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
