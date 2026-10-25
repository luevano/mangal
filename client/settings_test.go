package client

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/storage"
)

func TestImportSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mangal.toml")
	if err := os.WriteFile(path, []byte("[download]\nformat = 'CBZ'\n[providers.mangaplus]\nandroid_id = 'abc'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(config.Reset)
	if err := config.Load(path); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenData(filepath.Join(dir, storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	st := storage.Storage{Data: db}
	s := st.Settings(false)
	ops := Ops{Storage: st, BackupsDir: filepath.Join(dir, "backups"), Keep: 3}

	notice, err := importSettings(context.Background(), ops, s)
	if err != nil || !strings.Contains(notice, "Copied 2 settings") {
		t.Fatalf("notice %q, %v", notice, err)
	}
	if config.Download.Format.Get() != libmangal.FormatCBZ || !config.Imported() {
		t.Errorf("not read from the database: %s", config.Download.Format.Get())
	}
	values, _, _ := s.All()
	if values["providers.mango-mangaplus.android_id"] != `"abc"` {
		t.Errorf("values %v", values)
	}
	// the secret is with the logins, not in the settings table
	var row string
	if err := db.QueryRow(`SELECT value FROM settings WHERE key = 'providers.mango-mangaplus.android_id'`).Scan(&row); err != nil || row != "" {
		t.Errorf("secret row %q, %v", row, err)
	}
	ops2, err := st.Journal().Ops(0)
	if err != nil || len(ops2) != 1 || ops2[0].Kind != "settings" {
		t.Errorf("journal %+v, %v", ops2, err)
	}
	// once per database
	if notice, err := importSettings(context.Background(), ops, s); notice != "" || err != nil {
		t.Errorf("second import %q, %v", notice, err)
	}
	if !strings.Contains(MovedNotice(), "providers.mangaplus.android_id (now providers.mango-mangaplus.android_id)") {
		t.Errorf("moved notice %q", MovedNotice())
	}
}
