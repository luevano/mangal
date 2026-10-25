package storage

import (
	"path/filepath"
	"testing"
)

func TestSettings(t *testing.T) {
	db, err := OpenData(filepath.Join(t.TempDir(), FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := Storage{Data: db}.Settings(false)

	if err := s.Set("download.format", new(`"cbz"`), false, "cli"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("notification.discord.webhook_url", new(`"https://x"`), true, "cli"); err != nil {
		t.Fatal(err)
	}
	// unchanged: no history, same revision
	if err := s.Set("download.format", new(`"cbz"`), false, "tui"); err != nil {
		t.Fatal(err)
	}
	values, rev, err := s.All()
	if err != nil {
		t.Fatal(err)
	}
	if values["download.format"] != `"cbz"` || values["notification.discord.webhook_url"] != `"https://x"` || rev != 2 {
		t.Fatalf("all: %v rev %d", values, rev)
	}

	// the import only adds what isn't set, once
	imported, err := s.Import(map[string]string{"download.format": `"pdf"`, "download.path": `"/lib"`}, func(string) bool { return false }, "import")
	if err != nil || !imported {
		t.Fatalf("import: %v %v", imported, err)
	}
	if imported, err := s.Import(map[string]string{"watch.gaps": "true"}, func(string) bool { return false }, "import"); err != nil || imported {
		t.Fatalf("second import: %v %v", imported, err)
	}
	values, _, _ = s.All()
	if values["download.format"] != `"cbz"` || values["download.path"] != `"/lib"` || values["watch.gaps"] != "" {
		t.Fatalf("after import: %v", values)
	}

	if err := s.Set("download.format", nil, false, "cli"); err != nil {
		t.Fatal(err)
	}
	history, err := s.History("", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 4 || history[0].Key != "download.format" || history[0].New != nil || *history[0].Old != `"cbz"` || history[0].Who != "cli" {
		t.Fatalf("history: %+v", history)
	}
	for _, c := range history {
		if c.Key == "notification.discord.webhook_url" && (c.New == nil || *c.New != secretMasked) {
			t.Errorf("secret in the history: %+v", c)
		}
	}
	if h, _ := s.History("download.path", 1); len(h) != 1 || h[0].Who != "import" {
		t.Errorf("key history: %+v", h)
	}
}
