package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/storage"
	"golang.org/x/oauth2"
)

func TestMain(m *testing.M) {
	// keep the logins away from the user's databases
	dir, err := os.MkdirTemp("", "mangal-cache-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	os.Setenv("MANGAL_DATA_PATH", filepath.Join(dir, "data"))
	os.Setenv("MANGAL_AUTH_KEYRING", "false")
	// a config file of before, with the MyAnimeList API client
	file := filepath.Join(dir, "mangal.toml")
	data := "[metadata.myanimelist]\nclient_id = \" old-id \"\nclient_secret = \"old-secret\"\nenabled = true\n"
	if err := os.WriteFile(file, []byte(data), 0o600); err != nil {
		panic(err)
	}
	if err := config.Load(file); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestConfigClientMovedOnce(t *testing.T) {
	if err := Open(); err != nil {
		t.Fatal(err)
	}
	if notices := strings.Join(Notices(), "\n"); !strings.Contains(notices, "metadata.myanimelist.client_id") {
		t.Errorf("no notice of the move: %q", notices)
	}
	id, secret, err := MyAnimeListClient()
	if err != nil || id != "old-id" || secret != "old-secret" {
		t.Fatalf("moved client %q %q, %v", id, secret, err)
	}

	// cleared later, the config doesn't bring it back
	if err := SetMyAnimeListClient("", ""); err != nil {
		t.Fatal(err)
	}
	if notice := moveConfigClient(st.Data); notice != "" {
		t.Errorf("moved again: %q", notice)
	}
	if id, secret, err := MyAnimeListClient(); err != nil || id != "" || secret != "" {
		t.Errorf("cleared client is %q %q, %v", id, secret, err)
	}
	if unsupported := config.Unsupported(); len(unsupported) != 0 {
		t.Errorf("retired keys reported as unsupported: %v", unsupported)
	}
}

func TestAPIClientsBesideLogins(t *testing.T) {
	// the logins are keyed by username, the clients' keys could be one
	for _, user := range []string{keyClientID, keyClientSecret} {
		if err := SetMyAnimeListAuthData(user, &oauth2.Token{AccessToken: "mal-" + user}); err != nil {
			t.Fatal(err)
		}
		if err := SetAnilistAuthData(user, &oauth2.Token{AccessToken: "al-" + user}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := AuthFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if err := SetMyAnimeListClient("mal-id", "mal-secret"); err != nil {
		t.Fatal(err)
	}
	// mangal serve reloads the providers on a change
	if after, err := AuthFingerprint(); err != nil || after == before {
		t.Errorf("auth fingerprint unchanged by a new client (%v)", err)
	}
	if err := SetAnilistClient("al-id", ""); err != nil {
		t.Fatal(err)
	}

	if id, secret, err := MyAnimeListClient(); err != nil || id != "mal-id" || secret != "mal-secret" {
		t.Errorf("myanimelist client %q %q, %v", id, secret, err)
	}
	if id, secret, err := AnilistClient(); err != nil || id != "al-id" || secret != "" {
		t.Errorf("anilist client %q %q, %v", id, secret, err)
	}
	for _, user := range []string{keyClientID, keyClientSecret} {
		var token oauth2.Token
		if found, err := GetMyAnimeListAuthData(user, &token); err != nil || !found || token.AccessToken != "mal-"+user {
			t.Errorf("myanimelist login of %q: %v %v %q", user, found, err, token.AccessToken)
		}
		if found, err := GetAnilistAuthData(user, &token); err != nil || !found || token.AccessToken != "al-"+user {
			t.Errorf("anilist login of %q: %v %v %q", user, found, err, token.AccessToken)
		}
	}

	// an empty id removes the secret too
	if err := SetMyAnimeListClient("", "left"); err != nil {
		t.Fatal(err)
	}
	if id, secret, err := MyAnimeListClient(); err != nil || id != "" || secret != "" {
		t.Errorf("cleared client is %q %q, %v", id, secret, err)
	}
	if secret, err := getString(authOwnerMyAnimeListClient, keyClientSecret); err != nil || secret != "" {
		t.Errorf("secret kept after clearing: %q, %v", secret, err)
	}
}

// a database error says which secret it was saving or reading
func TestSecretErrorsWrapped(t *testing.T) {
	if err := Open(); err != nil {
		t.Fatal(err)
	}
	data, err := storage.OpenData(filepath.Join(t.TempDir(), storage.FilenameData))
	if err != nil {
		t.Fatal(err)
	}
	_ = data.Close()
	old := st
	st = storage.Storage{Cache: old.Cache, Data: data}
	t.Cleanup(func() { st = old })
	if err := SetDiscordWebhook("https://discord.test/hook"); err == nil || !strings.Contains(err.Error(), "save the discord webhook: ") {
		t.Errorf("save: %v", err)
	}
	if err := SetServeToken(""); err == nil || !strings.Contains(err.Error(), "remove the serve token: ") {
		t.Errorf("remove: %v", err)
	}
	if _, err := MangaPlusSecret(); err == nil || !strings.Contains(err.Error(), "read the mangaplus secret: ") {
		t.Errorf("read: %v", err)
	}
}
