package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
)

// MangaPlus secrets are kept with the other logins (auth tokens).
const (
	authOwnerMangaPlus     = "mangaplus"
	keyMangaPlusSecret     = "secret"
	keyMangaPlusFreeDevice = "free_secret"
)

func getString(owner, key string) (string, error) {
	st, err := Storage()
	if err != nil {
		return "", err
	}
	var value string
	if _, err := st.AuthStore(owner, keyring).Get(key, &value); err != nil {
		return "", fmt.Errorf("read the %s %s: %w", owner, key, err)
	}
	return value, nil
}

func setString(owner, key, value string) error {
	st, err := Storage()
	if err != nil {
		return err
	}
	kv := st.AuthStore(owner, keyring)
	if value == "" {
		if err := kv.Delete(key); err != nil {
			return fmt.Errorf("remove the %s %s: %w", owner, key, err)
		}
		return nil
	}
	if err := kv.Set(key, value); err != nil {
		return fmt.Errorf("save the %s %s: %w", owner, key, err)
	}
	return nil
}

// MangaPlusSecret is the device secret of the official app (subscription), empty if not set.
func MangaPlusSecret() (string, error) {
	return getString(authOwnerMangaPlus, keyMangaPlusSecret)
}

// SetMangaPlusSecret saves the device secret, empty removes it.
func SetMangaPlusSecret(secret string) error {
	return setString(authOwnerMangaPlus, keyMangaPlusSecret, secret)
}

// MangaPlusFreeSecret is the secret of the registered free device, empty if none yet.
func MangaPlusFreeSecret() (string, error) {
	return getString(authOwnerMangaPlus, keyMangaPlusFreeDevice)
}

// SetMangaPlusFreeSecret saves the secret of a newly registered free device.
func SetMangaPlusFreeSecret(secret string) error {
	return setString(authOwnerMangaPlus, keyMangaPlusFreeDevice, secret)
}

// The API clients of AniList and MyAnimeList have owners of their own: the services' logins
// are keyed by username, which any key could be.
const (
	authOwnerAnilistClient     = "anilist-client"
	authOwnerMyAnimeListClient = "myanimelist-client"
	keyClientID                = "id"
	keyClientSecret            = "secret"
)

func apiClient(owner string) (id, secret string, err error) {
	if id, err = getString(owner, keyClientID); err != nil || id == "" {
		return "", "", err
	}
	secret, err = getString(owner, keyClientSecret)
	return id, secret, err
}

func setAPIClient(owner, id, secret string) error {
	if id == "" {
		secret = ""
	}
	if err := setString(owner, keyClientID, id); err != nil {
		return err
	}
	return setString(owner, keyClientSecret, secret)
}

// MyAnimeListClient is the saved MyAnimeList API client, an empty id if none.
func MyAnimeListClient() (id, secret string, err error) {
	return apiClient(authOwnerMyAnimeListClient)
}

// SetMyAnimeListClient saves the MyAnimeList API client, an empty id removes it.
func SetMyAnimeListClient(id, secret string) error {
	return setAPIClient(authOwnerMyAnimeListClient, id, secret)
}

// AnilistClient is the AniList API client of the last new login, an empty id if none.
func AnilistClient() (id, secret string, err error) {
	return apiClient(authOwnerAnilistClient)
}

// SetAnilistClient saves the AniList API client, an empty id removes it.
func SetAnilistClient(id, secret string) error {
	return setAPIClient(authOwnerAnilistClient, id, secret)
}

// The Discord webhook is a secret too (its url is enough to post).
const (
	authOwnerDiscord  = "discord"
	keyDiscordWebhook = "webhook"
	authOwnerServe    = "serve"
	keyServeToken     = "token"
	keyServeTokens    = "tokens"
)

// DiscordWebhook is the saved Discord webhook url, empty if none.
func DiscordWebhook() (string, error) {
	return getString(authOwnerDiscord, keyDiscordWebhook)
}

// SetDiscordWebhook saves the Discord webhook url, empty removes it.
func SetDiscordWebhook(url string) error {
	return setString(authOwnerDiscord, keyDiscordWebhook, url)
}

// moveServeTokens makes API tokens of the tokens with every scope mangal had before the
// logins: serve.token ("config") and the one `mangal serve token` saved ("default"), so
// the programs sending them keep working. Once: the saved one is removed, serve.token is a
// retired key (config clean drops it).
func moveServeTokens(data *storage.DB) string {
	kv := storage.Storage{Data: data}.AuthStore(authOwnerServe, keyring)
	var tokens []ServeTokenEntry
	if _, err := kv.Get(keyServeTokens, &tokens); err != nil {
		return fmt.Sprintf("Couldn't read the serve API tokens: %s", err)
	}
	var saved string
	if _, err := kv.Get(keyServeToken, &saved); err != nil {
		return fmt.Sprintf("Couldn't read the saved serve token: %s", err)
	}
	var moved []string
	add := func(name, token string) {
		hash := sha256.Sum256([]byte(token))
		h := hex.EncodeToString(hash[:])
		if token == "" || slices.ContainsFunc(tokens, func(t ServeTokenEntry) bool { return t.Name == name || t.Hash == h }) {
			return
		}
		tokens = append(tokens, ServeTokenEntry{Name: name, Hash: h, Scopes: config.ServeScopes, Created: time.Now()})
		moved = append(moved, name)
	}
	add("config", config.RetiredValue("serve.token"))
	add("default", saved)
	if len(moved) != 0 {
		if err := kv.Set(keyServeTokens, tokens); err != nil {
			return fmt.Sprintf("Couldn't move the serve tokens to API tokens: %s", err)
		}
	}
	if saved != "" {
		if err := kv.Delete(keyServeToken); err != nil {
			return fmt.Sprintf("Couldn't remove the saved serve token: %s", err)
		}
	}
	if len(moved) == 0 {
		return ""
	}
	log.Info("moved the serve tokens %s to API tokens", strings.Join(moved, ", "))
	return fmt.Sprintf("Made API tokens (every scope) of the old serve tokens: %s. Programs sending them keep working; people log in as users now (\"mangal user add\"), \"mangal token ls\" shows the tokens.", strings.Join(moved, ", "))
}

// keyring is true when the logins are kept in the OS keyring (auth.keyring).
var keyring bool

// useKeyring moves the logins to the OS keyring, or back to the database, as auth.keyring
// says. Never an error: the logins just stay where they are.
func useKeyring(data *storage.DB) string {
	keyring = config.Auth.Keyring.Get()
	if keyring {
		if err := storage.KeyringAvailable(); err != nil {
			keyring = false
			return fmt.Sprintf("auth.keyring is on but the OS keyring can't be used (%s), the logins stay in mangal's database.", err)
		}
	}
	moved, err := storage.MoveToKeyring(data, storage.TableAuthTokens, keyring)
	switch {
	case err != nil:
		return fmt.Sprintf("Couldn't move the logins (auth.keyring): %s", err)
	case moved != 0 && keyring:
		return fmt.Sprintf("Moved %d logins and secrets to the OS keyring.", moved)
	case moved != 0:
		return fmt.Sprintf("Moved %d logins and secrets from the OS keyring back to mangal's database.", moved)
	}
	return ""
}

// Retired config keys of the MyAnimeList API client, see moveConfigClient.
const (
	retiredMyAnimeListClientID     = "metadata.myanimelist.client_id"
	retiredMyAnimeListClientSecret = "metadata.myanimelist.client_secret"
	// myAnimeListClientMoved (auth history) is set once the config's client was looked at,
	// so a client cleared later isn't brought back from the config
	myAnimeListClientMoved = MyAnimeListAuthHistory + "/client_moved"
)

// moveConfigClient moves the MyAnimeList API client of the config file (where mangal kept it
// before) to the logins, once and only when none is saved yet.
func moveConfigClient(data *storage.DB) string {
	id := config.RetiredValue(retiredMyAnimeListClientID)
	if id == "" {
		return ""
	}
	history := storage.KV{DB: data, Table: storage.TableAuthHistory}
	var moved bool
	if _, err := history.Get(myAnimeListClientMoved, &moved); err != nil {
		return fmt.Sprintf("Couldn't move %s to mangal's database: %s", retiredMyAnimeListClientID, err)
	}
	if moved {
		return ""
	}
	kv := storage.Storage{Data: data}.AuthStore(authOwnerMyAnimeListClient, keyring)
	var saved string
	if _, err := kv.Get(keyClientID, &saved); err != nil {
		return fmt.Sprintf("Couldn't move %s to mangal's database: %s", retiredMyAnimeListClientID, err)
	}
	if saved == "" {
		err := kv.Set(keyClientID, id)
		if secret := config.RetiredValue(retiredMyAnimeListClientSecret); err == nil && secret != "" {
			err = kv.Set(keyClientSecret, secret)
		}
		if err != nil {
			return fmt.Sprintf("Couldn't move %s to mangal's database: %s", retiredMyAnimeListClientID, err)
		}
	}
	if err := history.Set(myAnimeListClientMoved, true); err != nil {
		return fmt.Sprintf("Couldn't move %s to mangal's database: %s", retiredMyAnimeListClientID, err)
	}
	if saved != "" {
		log.Info("%s of the config ignored, a MyAnimeList API client is already saved", retiredMyAnimeListClientID)
		return ""
	}
	log.Info("moved %s (and the secret, if any) from the config to the saved logins", retiredMyAnimeListClientID)
	return fmt.Sprintf("Moved the MyAnimeList API client (%s) from the config to mangal's saved logins, it's no longer read from the config (\"mangal config write\" drops it, \"mangal myanimelist client\" changes it).", retiredMyAnimeListClientID)
}

// ServeTokenEntry is a named token of the `mangal serve` API: only its hash is kept, the
// token is shown once when made.
type ServeTokenEntry struct {
	Name string `json:"name"`
	// Hash is the hex sha256 of the token.
	Hash string `json:"hash"`
	// Scopes are the permissions before 5.28 (read, watch, progress, write), kept for
	// tokens made then; Perms are today's.
	Scopes []string `json:"scopes,omitempty"`
	Perms  []string `json:"perms,omitempty"`
	// Owner is the user who made it (it never has more than they do), empty when made
	// from the CLI or by an admin for the server.
	Owner   string    `json:"owner,omitempty"`
	Created time.Time `json:"created"`
}

// ServeTokens are the named tokens of the `mangal serve` API.
func ServeTokens() ([]ServeTokenEntry, error) {
	st, err := Storage()
	if err != nil {
		return nil, err
	}
	var tokens []ServeTokenEntry
	if _, err := st.AuthStore(authOwnerServe, keyring).Get(keyServeTokens, &tokens); err != nil {
		return nil, fmt.Errorf("read the serve tokens: %w", err)
	}
	return tokens, nil
}

// SetServeTokens saves the named tokens of the `mangal serve` API, none removes them.
func SetServeTokens(tokens []ServeTokenEntry) error {
	st, err := Storage()
	if err != nil {
		return err
	}
	kv := st.AuthStore(authOwnerServe, keyring)
	if len(tokens) == 0 {
		err = kv.Delete(keyServeTokens)
	} else {
		err = kv.Set(keyServeTokens, tokens)
	}
	if err != nil {
		return fmt.Errorf("save the serve tokens: %w", err)
	}
	return nil
}

// serveTokensMu and the serveTokensLock make UpdateServeTokens one at a time, in this
// process and across them.
var serveTokensMu sync.Mutex

const serveTokensLock = "serve-tokens"

// UpdateServeTokens saves the tokens change makes of the saved ones, one change at a time
// (two at once could undo each other: a revoked token come back).
func UpdateServeTokens(change func([]ServeTokenEntry) ([]ServeTokenEntry, error)) error {
	serveTokensMu.Lock()
	defer serveTokensMu.Unlock()
	st, err := Storage()
	if err != nil {
		return err
	}
	lock, err := st.Watches().Lock(serveTokensLock, 10*time.Second)
	// another process changes them: it takes milliseconds
	for tries := 0; errors.Is(err, storage.ErrLocked) && tries < 100; tries++ {
		time.Sleep(50 * time.Millisecond)
		lock, err = st.Watches().Lock(serveTokensLock, 10*time.Second)
	}
	if err != nil {
		return fmt.Errorf("save the serve tokens: %w", err)
	}
	defer func() {
		if err := lock.Unlock(); err != nil {
			log.Warn("%s", err)
		}
	}()
	tokens, err := ServeTokens()
	if err != nil {
		return err
	}
	if tokens, err = change(tokens); err != nil {
		return err
	}
	return SetServeTokens(tokens)
}
