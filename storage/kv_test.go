package storage

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestKVDecode(t *testing.T) {
	db, err := OpenCache(filepath.Join(t.TempDir(), FilenameCache))
	if err != nil {
		t.Fatal(err)
	}
	type value struct {
		A string
		B int
	}
	kv := KV{DB: db, Table: TableOther, Owner: "x"}
	if err := kv.Set("k", value{A: "a"}); err != nil {
		t.Fatal(err)
	}
	// zero fields aren't encoded, what the caller had in them must not stay
	got := value{A: "stale", B: 7}
	if found, err := kv.Get("k", &got); err != nil || !found || got != (value{A: "a"}) {
		t.Errorf("got %+v, %t, %v", got, found, err)
	}

	// a value of another type (e.g. stored by an older mangal) is a miss
	var other []float64
	if found, err := kv.Get("k", &other); err != nil || found {
		t.Errorf("other type: %t, %v, want a miss", found, err)
	}
}

// TestKVUpdate: the changes of two stores on the same database (two processes) add up.
func TestKVUpdate(t *testing.T) {
	st, err := OpenCache(filepath.Join(t.TempDir(), FilenameCache))
	if err != nil {
		t.Fatal(err)
	}
	kv := KV{DB: st, Table: TableOther, Owner: "t"}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			var n int
			if err := kv.Update("n", &n, func() { n++ }); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var n int
	if ok, err := kv.Get("n", &n); err != nil || !ok || n != 20 {
		t.Errorf("got %d %v %v", n, ok, err)
	}
}
