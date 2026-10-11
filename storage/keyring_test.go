package storage

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestKeyring(t *testing.T) {
	keyring.MockInit()
	db, err := OpenData(filepath.Join(t.TempDir(), FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	row := func(key string) []byte {
		var value []byte
		_ = db.QueryRow(`SELECT value FROM auth_tokens WHERE owner = 'anilist' AND key = ?`, key).Scan(&value)
		return value
	}

	plain := KV{DB: db, Table: TableAuthTokens, Owner: "anilist"}
	if err := plain.Set("before", "token-1"); err != nil {
		t.Fatal(err)
	}
	// turned on: what's there moves, new values go straight to the keyring
	if moved, err := MoveToKeyring(db, TableAuthTokens, true); err != nil || moved != 1 {
		t.Fatalf("move: %d, %v", moved, err)
	}
	kept := KV{DB: db, Table: TableAuthTokens, Owner: "anilist", Keyring: true}
	if err := kept.Set("after", "token-2"); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"before": "token-1", "after": "token-2"} {
		if !bytes.Equal(row(key), inKeyring) {
			t.Errorf("%s: row %q, want only the keyring mark", key, row(key))
		}
		var got string
		// read either way
		if found, err := plain.Get(key, &got); err != nil || !found || got != want {
			t.Errorf("%s: %q, %t, %v", key, got, found, err)
		}
	}

	// turned off: back into the database
	if moved, err := MoveToKeyring(db, TableAuthTokens, false); err != nil || moved != 2 {
		t.Fatalf("move back: %d, %v", moved, err)
	}
	if _, err := keyring.Get(keyringService, keyringUser(TableAuthTokens, "anilist", "after")); err != keyring.ErrNotFound {
		t.Errorf("still in the keyring: %v", err)
	}
	var got string
	if found, err := plain.Get("after", &got); err != nil || !found || got != "token-2" {
		t.Errorf("after: %q, %t, %v", got, found, err)
	}

	// deleting removes it from the keyring too
	if err := kept.Set("gone", "x"); err != nil {
		t.Fatal(err)
	}
	if err := kept.Delete("gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(keyringService, keyringUser(TableAuthTokens, "anilist", "gone")); err != keyring.ErrNotFound {
		t.Errorf("deleted but still in the keyring: %v", err)
	}
}
