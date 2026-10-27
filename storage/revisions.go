package storage

import "fmt"

// The topics of the revisions: what changed in the data database, whichever process
// changed it.
const (
	// RevAuth: the users, their sessions ending, the API tokens.
	RevAuth = "auth"
	// RevSettings: the settings stored in the database.
	RevSettings = "settings"
	// RevLibrary: the library's mangas and downloads.
	RevLibrary = "library"
	// RevHistory: the reads.
	RevHistory = "history"
	// RevJournal: the library operations (started, finished, undone).
	RevJournal = "journal"
	// RevWatches: the watches.
	RevWatches = "watches"
)

// revisionsTable: a counter per topic that triggers move on every change, in any process
// (SQLite tells no other process): a reader compares them to know what changed.
const revisionsTable = `CREATE TABLE revisions (
	topic TEXT PRIMARY KEY,
	rev   INTEGER NOT NULL DEFAULT 0
);
INSERT INTO revisions (topic) VALUES ('auth'), ('settings'), ('library'), ('history'), ('journal'), ('watches');
CREATE TRIGGER rev_users_insert AFTER INSERT ON serve_users BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'auth'; END;
CREATE TRIGGER rev_users_delete AFTER DELETE ON serve_users BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'auth'; END;
CREATE TRIGGER rev_users_update AFTER UPDATE OF name, hash, perms, generation ON serve_users BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'auth'; END;
CREATE TRIGGER rev_sessions_delete AFTER DELETE ON serve_sessions BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'auth'; END;
CREATE TRIGGER rev_tokens_insert AFTER INSERT ON auth_tokens WHEN NEW.owner = 'serve' BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'auth'; END;
CREATE TRIGGER rev_tokens_update AFTER UPDATE ON auth_tokens WHEN NEW.owner = 'serve' BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'auth'; END;
CREATE TRIGGER rev_tokens_delete AFTER DELETE ON auth_tokens WHEN OLD.owner = 'serve' BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'auth'; END;
CREATE TRIGGER rev_settings_insert AFTER INSERT ON settings BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'settings'; END;
CREATE TRIGGER rev_settings_update AFTER UPDATE ON settings BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'settings'; END;
CREATE TRIGGER rev_settings_delete AFTER DELETE ON settings BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'settings'; END;
CREATE TRIGGER rev_mangas_insert AFTER INSERT ON library_mangas BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'library'; END;
CREATE TRIGGER rev_mangas_update AFTER UPDATE ON library_mangas BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'library'; END;
CREATE TRIGGER rev_mangas_delete AFTER DELETE ON library_mangas BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'library'; END;
CREATE TRIGGER rev_downloads_insert AFTER INSERT ON downloads BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'library'; END;
CREATE TRIGGER rev_downloads_update AFTER UPDATE ON downloads BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'library'; END;
CREATE TRIGGER rev_downloads_delete AFTER DELETE ON downloads BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'library'; END;
CREATE TRIGGER rev_reads_insert AFTER INSERT ON reads BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'history'; END;
CREATE TRIGGER rev_reads_update AFTER UPDATE ON reads BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'history'; END;
CREATE TRIGGER rev_reads_delete AFTER DELETE ON reads BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'history'; END;
CREATE TRIGGER rev_journal_insert AFTER INSERT ON journal BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'journal'; END;
CREATE TRIGGER rev_journal_update AFTER UPDATE ON journal BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'journal'; END;
CREATE TRIGGER rev_watches_insert AFTER INSERT ON watches BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'watches'; END;
CREATE TRIGGER rev_watches_update AFTER UPDATE ON watches BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'watches'; END;
CREATE TRIGGER rev_watches_delete AFTER DELETE ON watches BEGIN UPDATE revisions SET rev = rev + 1 WHERE topic = 'watches'; END`

// Revisions are the data database's change counters by topic (Rev*).
func (db *DB) Revisions() (map[string]int64, error) {
	rows, err := db.Query(`SELECT topic, rev FROM revisions`)
	if err != nil {
		return nil, fmt.Errorf("read the revisions: %w", err)
	}
	defer rows.Close()
	revs := map[string]int64{}
	for rows.Next() {
		var topic string
		var rev int64
		if err := rows.Scan(&topic, &rev); err != nil {
			return nil, fmt.Errorf("read the revisions: %w", err)
		}
		revs[topic] = rev
	}
	return revs, rows.Err()
}

// DataVersion is SQLite's data_version: it moves when another connection (another
// process) commits, not on this process's own commits.
func (db *DB) DataVersion() (int64, error) {
	var v int64
	err := db.QueryRow(`PRAGMA data_version`).Scan(&v)
	return v, err
}
