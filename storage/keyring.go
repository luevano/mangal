package storage

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// keyringService is the name the values are kept under in the OS keyring.
const keyringService = "mangal"

// inKeyring is the value of a row whose value is in the OS keyring.
var inKeyring = []byte("keyring")

func keyringUser(table, owner, key string) string {
	return table + "/" + owner + "/" + key
}

// KeyringAvailable says if the OS keyring can be used (Secret Service on Linux, the
// Keychain on macOS, the Credential Manager on Windows).
func KeyringAvailable() error {
	_, err := keyring.Get(keyringService, "probe")
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("keyring: %w", err)
}

func keyringGet(table, owner, key string) ([]byte, bool, error) {
	value, err := keyring.Get(keyringService, keyringUser(table, owner, key))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("keyring: %w", err)
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, false, fmt.Errorf("keyring: %w", err)
	}
	return data, true, nil
}

func keyringSet(table, owner, key string, data []byte) error {
	if err := keyring.Set(keyringService, keyringUser(table, owner, key), base64.StdEncoding.EncodeToString(data)); err != nil {
		return fmt.Errorf("keyring: %w", err)
	}
	return nil
}

func keyringDelete(table, owner, key string) error {
	if err := keyring.Delete(keyringService, keyringUser(table, owner, key)); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("keyring: %w", err)
	}
	return nil
}

// MoveToKeyring moves the values of a kv table into the OS keyring (toKeyring) or back
// into the table, returns how many moved.
func MoveToKeyring(db *DB, table string, toKeyring bool) (int, error) {
	rows, err := db.Query(`SELECT owner, key, value FROM ` + table)
	if err != nil {
		return 0, err
	}
	type row struct {
		owner, key string
		value      []byte
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.owner, &r.key, &r.value); err != nil {
			rows.Close()
			return 0, err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	moved := 0
	for _, r := range all {
		kept := bytes.Equal(r.value, inKeyring)
		switch {
		case toKeyring && !kept:
			if err := keyringSet(table, r.owner, r.key, r.value); err != nil {
				return moved, err
			}
			if _, err := db.Exec(`UPDATE `+table+` SET value = ? WHERE owner = ? AND key = ?`, inKeyring, r.owner, r.key); err != nil {
				return moved, err
			}
		case !toKeyring && kept:
			data, found, err := keyringGet(table, r.owner, r.key)
			if err != nil {
				return moved, err
			}
			if !found {
				if _, err := db.Exec(`DELETE FROM `+table+` WHERE owner = ? AND key = ?`, r.owner, r.key); err != nil {
					return moved, err
				}
				continue
			}
			if _, err := db.Exec(`UPDATE `+table+` SET value = ? WHERE owner = ? AND key = ?`, data, r.owner, r.key); err != nil {
				return moved, err
			}
			if err := keyringDelete(table, r.owner, r.key); err != nil {
				return moved, err
			}
		default:
			continue
		}
		moved++
	}
	return moved, nil
}
