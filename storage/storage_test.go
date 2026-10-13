package storage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	mango "github.com/luevano/mangoprovider"
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

func TestSynchronous(t *testing.T) {
	for _, tt := range []struct {
		open func(string) (*DB, error)
		want int
	}{{OpenCache, 1}, {OpenData, 2}} {
		db, err := tt.open(filepath.Join(t.TempDir(), "db.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		var got int
		if err := db.QueryRow("PRAGMA synchronous").Scan(&got); err != nil || got != tt.want {
			t.Errorf("synchronous %d, %v, want %d (1 normal, 2 full)", got, err, tt.want)
		}
		db.Close()
	}
}

func TestOpenEscapedPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a?b#c%20d")
	db, err := OpenCache(filepath.Join(dir, FilenameCache))
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := os.Stat(filepath.Join(dir, FilenameCache)); err != nil {
		t.Errorf("not created where asked: %v", err)
	}
}

func TestClearCacheProviderRows(t *testing.T) {
	db, err := OpenCache(filepath.Join(t.TempDir(), FilenameCache))
	if err != nil {
		t.Fatal(err)
	}
	st := Storage{Cache: db}
	_ = st.Store("mango_a", mango.CacheBucketNameMirrors, StoreOptions{}).Set("mango_a", "https://a")
	_ = st.AccountsStore(0).Set("mango_a", 1)
	_ = st.Store("mango_a", mango.CacheBucketNameChapters, StoreOptions{}).Set("x", 1)
	// _ isn't a wildcard
	_ = st.Store("mangoXa", mango.CacheBucketNameMirrors, StoreOptions{}).Set("mangoXa", "https://b")
	_ = st.AccountsStore(0).Set("mangoXa", 1)

	if n, err := st.ClearCache("mango_a"); err != nil || n != 3 {
		t.Errorf("cleared %d, %v, want the mirror, account and chapters", n, err)
	}
	if n, _ := ClearCache(db, ""); n != 2 {
		t.Errorf("%d left, want the other provider's 2", n)
	}
}

func TestClearChapterCache(t *testing.T) {
	cache, data := openBoth(t)
	st := Storage{Cache: cache, Data: data}
	opts := StoreOptions{TTL: time.Hour}
	_ = st.Store("p", "volumes", opts).Set("m", 1)
	_ = st.Store("p", "chapters", opts).Set("m", 1)
	_ = st.Store("p", "p", opts).Set("url", 1) // lua http
	_ = st.Store("p", "mangas", opts).Set("q", 1)
	_ = st.Store("other", "chapters", opts).Set("m", 1)
	if n, err := st.ClearChapterCache("p"); err != nil || n != 3 {
		t.Fatalf("cleared %d, %v", n, err)
	}
	if n, _ := countProviderRows(cache, "p"); n != 1 {
		t.Errorf("%d left, want the searches", n)
	}
	if n, _ := countProviderRows(cache, "other"); n != 1 {
		t.Errorf("another provider's cache cleared")
	}
}

func TestPermissions(t *testing.T) {
	dir := t.TempDir()
	cache, err := OpenCache(filepath.Join(dir, "cache", FilenameCache))
	if err != nil {
		t.Fatal(err)
	}
	cache.Close()
	if info, err := os.Stat(filepath.Join(dir, "cache", FilenameCache)); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("cache %v, %v", info.Mode(), err)
	}
	// a folder of the user's keeps its mode, one made for the database is private
	shared := filepath.Join(dir, "shared")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "notes.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{shared, filepath.Join(dir, "new")} {
		data, err := OpenData(filepath.Join(d, FilenameData))
		if err != nil {
			t.Fatal(err)
		}
		data.Close()
	}
	if info, _ := os.Stat(shared); info.Mode().Perm() != 0o755 {
		t.Errorf("shared folder %v", info.Mode())
	}
	if info, _ := os.Stat(filepath.Join(dir, "new")); info.Mode().Perm() != 0o700 {
		t.Errorf("new folder %v", info.Mode())
	}
	if info, _ := os.Stat(filepath.Join(shared, FilenameData)); info.Mode().Perm() != 0o600 {
		t.Errorf("data %v", info.Mode())
	}
}
