package legacy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/philippgille/gokv/encoding"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/oauth2"
)

// writeBolt writes gob values into a bbolt file like the old cache did.
func writeBolt(t *testing.T, file, bucket string, values map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := bolt.Open(file, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		for k, v := range values {
			data, err := encoding.Gob.Marshal(v)
			if err != nil {
				return err
			}
			if err := b.Put([]byte(k), data); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestImportBBolt(t *testing.T) {
	dir := t.TempDir()
	data, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}

	writeBolt(t, filepath.Join(dir, "mangal-auth.db"), BucketAnilistAuth, map[string]any{"david": oauth2.Token{AccessToken: "secret"}})
	writeBolt(t, filepath.Join(dir, "mangal-auth.db"), storage.BucketAuthHistory, map[string]any{"anilist": []string{}})
	writeBolt(t, filepath.Join(dir, "metadata", "anilist.db"), metadata.CacheBucketNameTitleToID, map[string]any{"berserk": 30002})
	writeBolt(t, filepath.Join(dir, "anilist.db"), metadata.CacheBucketNameTitleToID, map[string]any{"vagabond": 30656})
	writeBolt(t, filepath.Join(dir, "mango-mangadex.db"), "chapters", map[string]any{"x": 1})
	// mangal 5.10 and older
	writeBolt(t, filepath.Join(dir, "anilist", "title-to-id.db"), metadata.CacheBucketNameTitleToID, map[string]any{"claymore": 30583})
	writeBolt(t, filepath.Join(dir, "anilist", "id-to-manga.db"), "id-to-manga", map[string]any{"30583": "x"})
	writeBolt(t, filepath.Join(dir, "anilist", "access-token.db"), "access-token", map[string]any{"hi": "token"})

	// a file held by another (old) mangal
	writeBolt(t, filepath.Join(dir, "mango-mangaplus.db"), "volumes", map[string]any{"x": 1})
	held, err := bolt.Open(filepath.Join(dir, "mango-mangaplus.db"), 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	notice := Import(dir, data)
	if !strings.Contains(notice, "kept 3 title bindings, 1 logins and 1 histories") || !strings.Contains(notice, "log in again") || !strings.Contains(notice, "mango-mangaplus.db are in use") {
		t.Errorf("notice %q", notice)
	}

	var token oauth2.Token
	if found, err := (storage.KV{DB: data, Table: storage.TableAuthTokens, Owner: BucketAnilistAuth}).Get("david", &token); err != nil || !found || token.AccessToken != "secret" {
		t.Errorf("token %+v, %t, %v", token, found, err)
	}
	bindings := storage.KV{DB: data, Table: storage.TableTitleBindings, Owner: "anilist"}
	for title, want := range map[string]int{"berserk": 30002, "vagabond": 30656, "claymore": 30583} {
		var id int
		if found, err := bindings.Get(title, &id); err != nil || !found || id != want {
			t.Errorf("binding %q: %d, %t, %v", title, id, found, err)
		}
	}

	left, _ := filepath.Glob(filepath.Join(dir, "*.db"))
	if len(left) != 1 || filepath.Base(left[0]) != "mango-mangaplus.db" {
		t.Errorf("left %v, want only the file in use", left)
	}
	if _, err := os.Stat(filepath.Join(dir, "metadata")); !os.IsNotExist(err) {
		t.Error("empty metadata dir left")
	}
	if _, err := os.Stat(filepath.Join(dir, "anilist")); !os.IsNotExist(err) {
		t.Error("old anilist dir left")
	}

	// nothing left to convert (but the one in use)
	held.Close()
	if notice := Import(dir, data); !strings.Contains(notice, "kept 0 title bindings") {
		t.Errorf("second run: %q", notice)
	}
	if notice := Import(dir, data); notice != "" {
		t.Errorf("third run: %q", notice)
	}
}

func TestImportForeign(t *testing.T) {
	dir := t.TempDir()
	data, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	// someone else's bbolt file, a file that isn't bbolt, and an old file that can't be
	// converted (the data database is closed)
	writeBolt(t, filepath.Join(dir, "other-app.db"), "users", map[string]any{"x": 1})
	if err := os.WriteFile(filepath.Join(dir, "notes.db"), []byte("not bbolt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if notice := Import(dir, data); notice != "" {
		t.Errorf("notice %q, want none", notice)
	}

	writeBolt(t, filepath.Join(dir, "mangal.db"), storage.BucketSearchHistory, map[string]any{"mangas": []string{}})
	data.Close()
	if notice := Import(dir, data); !strings.Contains(notice, "Couldn't convert") || strings.Contains(notice, "Converted") {
		t.Errorf("notice %q", notice)
	}
	for _, name := range []string{"other-app.db", "notes.db", "mangal.db"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v, want it kept", name, err)
		}
	}
}
