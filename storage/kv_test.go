package storage

import (
	"path/filepath"
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
