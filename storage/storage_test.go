package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestKV(t *testing.T) {
	path := filepath.Join(t.TempDir(), FilenameCache)
	db, err := OpenCache(path)
	if err != nil {
		t.Fatal(err)
	}
	kv := KV{DB: db, Table: TableVolumes, Owner: "mango-mangadex"}

	if err := kv.Set("x", []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	var got []int
	if found, err := kv.Get("x", &got); err != nil || !found || len(got) != 2 {
		t.Fatalf("got %v, %t, %v", got, found, err)
	}
	// another owner doesn't see it
	if found, _ := (KV{DB: db, Table: TableVolumes, Owner: "other"}).Get("x", &got); found {
		t.Error("found another owner's value")
	}
	if err := kv.Delete("x"); err != nil {
		t.Fatal(err)
	}
	if found, _ := kv.Get("x", &got); found {
		t.Error("found after delete")
	}

	// expired values are gone, also after reopening
	expiring := KV{DB: db, Table: TableVolumes, Owner: "mango-mangadex", TTL: time.Second}
	if err := expiring.Set("y", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE volumes SET expires = 1`); err != nil {
		t.Fatal(err)
	}
	var n int
	if found, _ := expiring.Get("y", &n); found {
		t.Error("found an expired value")
	}
	_ = expiring.Set("z", 1)
	_, _ = db.Exec(`UPDATE volumes SET expires = 1`)
	db.Close()

	db, err = OpenCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM volumes`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d rows after reopening, want the expired ones pruned (%v)", n, err)
	}
}

func TestConcurrentProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), FilenameData)
	// two handles, like two mangal processes
	a, err := OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 50 {
		for _, db := range []*DB{a, b} {
			wg.Go(func() {
				kv := KV{DB: db, Table: TableSearchHistory}
				if err := kv.Set(fmt.Sprint(i), i); err != nil {
					t.Error(err)
				}
			})
		}
	}
	wg.Wait()
	var n int
	if err := a.QueryRow(`SELECT count(*) FROM search_history`).Scan(&n); err != nil || n != 50 {
		t.Errorf("%d rows, %v", n, err)
	}
}

func TestClearCache(t *testing.T) {
	db, err := OpenCache(filepath.Join(t.TempDir(), FilenameCache))
	if err != nil {
		t.Fatal(err)
	}
	_ = KV{DB: db, Table: TableVolumes, Owner: "mango-mangadex"}.Set("a", 1)
	_ = KV{DB: db, Table: TableOther, Owner: "mango-mangadex/x"}.Set("b", 1)
	_ = KV{DB: db, Table: TableVolumes, Owner: "mango-mangapill"}.Set("a", 1)

	if n, err := ClearCache(db, "mango-mangadex"); err != nil || n != 2 {
		t.Errorf("cleared %d, %v, want 2", n, err)
	}
	if n, err := ClearCache(db, ""); err != nil || n != 1 {
		t.Errorf("cleared %d, %v, want the remaining 1", n, err)
	}
}

func TestDataIsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mangal")
	path := filepath.Join(dir, FilenameData)
	// an existing database readable by anyone
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := OpenData(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = KV{DB: db, Table: TableAuthTokens}.Set("x", 1)
	for p, want := range map[string]os.FileMode{dir: 0o700, path: 0o600, path + "-wal": 0o600} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s: %o, want %o", filepath.Base(p), got, want)
		}
	}
}

// several processes starting on a fresh database: only one applies each migration
func TestConcurrentMigrations(t *testing.T) {
	for _, open := range []func(string) (*DB, error){OpenCache, OpenData} {
		path := filepath.Join(t.TempDir(), "db.sqlite")
		var wg sync.WaitGroup
		errs := make([]error, 8)
		for i := range errs {
			wg.Go(func() {
				db, err := open(path)
				if err == nil {
					err = db.Close()
				}
				errs[i] = err
			})
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
