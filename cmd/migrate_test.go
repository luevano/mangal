package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/path"
)

// migrate works on the library: a missing download.path (a drive not mounted) is refused
func TestMigrateMissingDownloads(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dl")
	old := config.Download.Path.Get()
	if err := config.Download.Path.Set(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.Download.Path.Set(old) })
	if err := migrateCmd.RunE(migrateCmd, nil); !errors.Is(err, path.ErrNoDownloads) {
		t.Errorf("missing download path: %v", err)
	}
	if _, err := os.Stat(root); err == nil {
		t.Error("made the missing download path")
	}
}
