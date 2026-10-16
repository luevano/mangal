package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestReaderPositions(t *testing.T) {
	data, err := OpenData(filepath.Join(t.TempDir(), FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer data.Close()
	p := ReaderPositions{DB: data}
	if _, found, err := p.Get("lib:1:a.cbz"); found || err != nil {
		t.Fatalf("empty: %v, %v", found, err)
	}
	now := time.Now()
	if err := p.Set(ReaderPosition{Chapter: "lib:1:a.cbz", Page: 4, Updated: now}); err != nil {
		t.Fatal(err)
	}
	// an older position (a device that was offline) doesn't win
	if err := p.Set(ReaderPosition{Chapter: "lib:1:a.cbz", Page: 1, Updated: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	pos, found, err := p.Get("lib:1:a.cbz")
	if !found || err != nil || pos.Page != 4 || pos.Updated.UnixMilli() != now.UnixMilli() {
		t.Fatalf("got %+v, %v, %v", pos, found, err)
	}
	if err := p.Set(ReaderPosition{Chapter: "lib:1:a.cbz", Page: 9}); err != nil {
		t.Fatal(err)
	}
	if pos, _, _ := p.Get("lib:1:a.cbz"); pos.Page != 9 {
		t.Errorf("newer position not saved: %+v", pos)
	}
}
