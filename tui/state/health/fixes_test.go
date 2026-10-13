package health

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/path"
)

// the scan fix refuses a missing download.path (a drive not mounted), it doesn't make it
func TestScanLibraryMissingDownloads(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dl")
	old := config.Download.Path.Get()
	if err := config.Download.Path.Set(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Download.Path.Set(old) })
	if _, err := scanLibrary(context.Background(), func(string) {}); !errors.Is(err, path.ErrNoDownloads) {
		t.Errorf("missing download path: %v", err)
	}
	if _, err := os.Stat(root); err == nil {
		t.Error("made the missing download path")
	}
}

// the rename fix makes its folders with download.mode_dir, stops on cancel
func TestApplyRenames(t *testing.T) {
	old := config.Download.ModeDir.Get()
	if err := config.Download.ModeDir.Set(0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Download.ModeDir.Set(old) })
	root := t.TempDir()
	from, to := filepath.Join(root, "Berserk"), filepath.Join(root, "Weeb Central", "Berserk")
	_ = os.MkdirAll(from, 0o755)
	renames := []library.Rename{{From: from, To: to}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := applyRenames(ctx, renames, func(string) {}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	if _, err := os.Stat(from); err != nil {
		t.Error("cancelled, still renamed")
	}

	if _, err := applyRenames(context.Background(), renames, func(string) {}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(to))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != fs.FileMode(0o700) {
		t.Errorf("mode %o, want 700", got)
	}
}
