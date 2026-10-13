package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

func setDownloads(t *testing.T, dir string) {
	t.Helper()
	old := config.Download.Path.Get()
	if err := config.Download.Path.Set(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Download.Path.Set(old) })
}

// a missing download.path (a drive not mounted) is refused, not made, and esc stops a scan
func TestScan(t *testing.T) {
	db, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	lib := storage.Library{DB: db}
	root := filepath.Join(t.TempDir(), "dl")
	setDownloads(t, root)
	files := library.Files{FS: afero.NewOsFs(), Library: lib, Root: root}

	if _, err := scan(context.Background(), files, false, false, func(string) {}); !errors.Is(err, path.ErrNoDownloads) {
		t.Errorf("missing download path: %v", err)
	}
	if _, err := os.Stat(root); err == nil {
		t.Error("the scan made the missing download path")
	}

	_ = os.MkdirAll(filepath.Join(root, "Berserk"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "Berserk", "1.cbz"), []byte("x"), 0o644)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scan(ctx, files, false, false, func(string) {}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	if mangas, _ := lib.Mangas(); len(mangas) != 0 {
		t.Errorf("cancelled, still added %d mangas", len(mangas))
	}
	if msg, err := scan(context.Background(), files, false, false, func(string) {}); err != nil || msg.result.New != 1 {
		t.Errorf("scan: %+v %v", msg.result, err)
	}
}
