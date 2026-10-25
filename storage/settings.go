package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"
)

// settingsTables keep the settings stored in the database (mangal config's database keys and
// the providers' settings) with a history of their changes. The revision grows with every
// change, so a process re-reads them only when it moved; imported is set once mangal.toml's
// moved keys were copied in.
const settingsTables = `CREATE TABLE settings (
	key     TEXT PRIMARY KEY,
	value   TEXT NOT NULL,
	updated INTEGER NOT NULL
) WITHOUT ROWID;
CREATE TABLE settings_history (
	id  INTEGER PRIMARY KEY,
	key TEXT NOT NULL,
	old TEXT,
	new TEXT,
	who TEXT NOT NULL,
	at  INTEGER NOT NULL
);
CREATE INDEX settings_history_key ON settings_history (key);
CREATE TABLE settings_meta (
	name  TEXT PRIMARY KEY,
	value INTEGER NOT NULL
) WITHOUT ROWID;
INSERT INTO settings_meta (name, value) VALUES ('revision', 0), ('imported', 0)`

// secretMasked is a secret's value in the history.
const secretMasked = "••••••"

// inSecrets is a secret's value in the settings table, the value is in Secrets.
const inSecrets = ""

// Settings are the settings stored in the data database, values as text (mangal's
// config stores them as JSON). Secret values are kept in Secrets (the logins' store, the
// OS keyring with auth.keyring), never in the history.
type Settings struct {
	DB      *DB
	Secrets KV
}

// Settings of the data database, the secrets in the logins' store.
func (s Storage) Settings(keyring bool) Settings {
	return Settings{DB: s.Data, Secrets: s.AuthStore("settings", keyring)}
}

// SettingChange is a change in the history: Old or New nil when the key wasn't set or was
// unset, masked for a secret.
type SettingChange struct {
	ID       int64
	Key      string
	Old, New *string
	Who      string
	At       time.Time
}

// Revision grows with every change.
func (s Settings) Revision() (int64, error) {
	var rev int64
	if err := s.DB.QueryRow(`SELECT value FROM settings_meta WHERE name = 'revision'`).Scan(&rev); err != nil {
		return 0, fmt.Errorf("settings revision: %w", err)
	}
	return rev, nil
}

// All are the stored values by key and the revision they're at.
func (s Settings) All() (map[string]string, int64, error) {
	values := map[string]string{}
	var rev int64
	var secrets []string
	err := s.DB.InTx(func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT value FROM settings_meta WHERE name = 'revision'`).Scan(&rev); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT key, value FROM settings`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key, value string
			if err := rows.Scan(&key, &value); err != nil {
				return err
			}
			if value == inSecrets {
				secrets = append(secrets, key)
				continue
			}
			values[key] = value
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("read the settings: %w", err)
	}
	// outside the transaction: the keyring is slow, and the store has one connection
	for _, key := range secrets {
		var value string
		if _, err := s.Secrets.Get(key, &value); err != nil {
			return nil, 0, fmt.Errorf("read the setting %s: %w", key, err)
		}
		values[key] = value
	}
	return values, rev, nil
}

// Set stores the key's value, nil unsets it, recording who changed it. A secret's value
// goes to Secrets.
func (s Settings) Set(key string, value *string, secret bool, who string) error {
	return s.set(map[string]*string{key: value}, func(string) bool { return secret }, who, true)
}

// set with touch counts the change as this process's write (Mark); an operation's own
// writes (the import) don't.
func (s Settings) set(values map[string]*string, secret func(string) bool, who string, touch bool) error {
	for key, value := range values {
		if !secret(key) {
			continue
		}
		var err error
		if value == nil {
			err = s.Secrets.Delete(key)
		} else {
			err = s.Secrets.Set(key, *value)
		}
		if err != nil {
			return fmt.Errorf("set the setting %s: %w", key, err)
		}
	}
	now := time.Now().Unix()
	return s.DB.InTx(func(tx *sql.Tx) error {
		changed := false
		for key, value := range values {
			var old *string
			var stored string
			switch err := tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&stored); {
			case errors.Is(err, sql.ErrNoRows):
			case err != nil:
				return fmt.Errorf("set the setting %s: %w", key, err)
			default:
				old = &stored
			}
			row := value
			if secret(key) && value != nil {
				row = new(inSecrets)
			}
			if old == nil && row == nil || old != nil && row != nil && *old == *row && !secret(key) {
				continue
			}
			var err error
			if row == nil {
				_, err = tx.Exec(`DELETE FROM settings WHERE key = ?`, key)
			} else {
				_, err = tx.Exec(`INSERT INTO settings (key, value, updated) VALUES (?, ?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated = excluded.updated`, key, *row, now)
			}
			if err != nil {
				return fmt.Errorf("set the setting %s: %w", key, err)
			}
			if secret(key) {
				old, value = masked(old), masked(value)
			}
			if _, err := tx.Exec(`INSERT INTO settings_history (key, old, new, who, at) VALUES (?, ?, ?, ?, ?)`, key, old, value, who, now); err != nil {
				return fmt.Errorf("record the setting %s: %w", key, err)
			}
			changed = true
		}
		if !changed {
			return nil
		}
		if touch {
			s.DB.touch()
		}
		_, err := tx.Exec(`UPDATE settings_meta SET value = value + 1 WHERE name = 'revision'`)
		return err
	})
}

func masked(v *string) *string {
	if v == nil {
		return nil
	}
	return new(secretMasked)
}

// Imported is true once Import ran on this database.
func (s Settings) Imported() (bool, error) {
	var done int64
	if err := s.DB.QueryRow(`SELECT value FROM settings_meta WHERE name = 'imported'`).Scan(&done); err != nil {
		return false, fmt.Errorf("settings import: %w", err)
	}
	return done != 0, nil
}

// Import stores the values of the keys that aren't set yet and marks the database as
// imported, once: when it already is, nothing changes and imported is false.
func (s Settings) Import(values map[string]string, secret func(string) bool, who string) (imported bool, err error) {
	// checked again under the write lock below: another process may import meanwhile
	if done, err := s.Imported(); err != nil || done {
		return false, err
	}
	have, err := queryAll(s.DB, func(rows *sql.Rows) (string, error) {
		var key string
		return key, rows.Scan(&key)
	}, `SELECT key FROM settings`)
	if err != nil {
		return false, fmt.Errorf("import the settings: %w", err)
	}
	set := make(map[string]*string, len(values))
	for key, value := range values {
		if !slices.Contains(have, key) {
			set[key] = &value
		}
	}
	if err := s.set(set, secret, who, false); err != nil {
		return false, fmt.Errorf("import the settings: %w", err)
	}
	err = s.DB.InTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE settings_meta SET value = 1 WHERE name = 'imported' AND value = 0`)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		imported = n == 1
		return err
	})
	if err != nil {
		return false, fmt.Errorf("import the settings: %w", err)
	}
	return imported, nil
}

// History is the key's changes ("" for every key), newest first, at most limit (0 for all).
func (s Settings) History(key string, limit int) ([]SettingChange, error) {
	if limit <= 0 {
		limit = -1
	}
	changes, err := queryAll(s.DB, func(rows *sql.Rows) (SettingChange, error) {
		var c SettingChange
		var at int64
		err := rows.Scan(&c.ID, &c.Key, &c.Old, &c.New, &c.Who, &at)
		c.At = time.Unix(at, 0)
		return c, err
	}, `SELECT id, key, old, new, who, at FROM settings_history WHERE ? = '' OR key = ? ORDER BY id DESC LIMIT ?`, key, key, limit)
	if err != nil {
		return nil, fmt.Errorf("settings history: %w", err)
	}
	return changes, nil
}
