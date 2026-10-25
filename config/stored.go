package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/spf13/viper"
)

// Stored is where a key's value is stored.
type Stored string

const (
	// StoredFile keys are in mangal.toml: needed before the database opens, or about this
	// machine or process.
	StoredFile Stored = "file"
	// StoredDB keys are in the data database: what mangal does with the library,
	// providers, metadata, watches, jobs and notifications, the same in every process
	// sharing it (docs/settings-db.md). The providers' settings are stored there too.
	StoredDB Stored = "db"
)

var (
	dbPrefixes = []string{"download.", "read.history.", "watch.", "jobs.", "notification.", "providers.filter.", "providers.headless."}
	dbKeys     = []string{
		"metadata.anilist.tag_rank_min", "metadata.kitsu.enabled", "metadata.myanimelist.enabled",
		"cache.ttl", "cache.mirror_ttl", "data.backups", "providers.parallelism", "providers.mirrors",
		"health.source_check_interval",
	}
)

// StoredIn is where the key's value is stored.
func StoredIn(key string) Stored {
	if isProviderKey(key) || slices.Contains(dbKeys, key) || slices.ContainsFunc(dbPrefixes, func(p string) bool { return strings.HasPrefix(key, p) }) {
		return StoredDB
	}
	return StoredFile
}

// renamed are the keys that became a provider's settings, by their old name. Only for
// importing old config files: the ids are the providers' of that time.
var renamed = map[string]string{
	"providers.mangadex.data_saver":        "providers.mango-mangadex.data_saver",
	"providers.mangadex.exclude_groups":    "providers.mango-mangadex.exclude_groups",
	"providers.mangadex.prefer_groups":     "providers.mango-mangadex.prefer_groups",
	"providers.mangaplus.quality":          "providers.mango-mangaplus.quality",
	"providers.mangaplus.api":              "providers.mango-mangaplus.api",
	"providers.mangaplus.viewer":           "providers.mango-mangaplus.viewer",
	"providers.mangaplus.request_interval": "providers.mango-mangaplus.request_interval",
	"providers.mangaplus.os_version":       "providers.mango-mangaplus.os_version",
	"providers.mangaplus.app_version":      "providers.mango-mangaplus.app_version",
	"providers.mangaplus.android_id":       "providers.mango-mangaplus.android_id",
	"providers.mangaplus.free_secret":      "providers.mango-mangaplus.free_secret",
	"providers.mangaplus.secret_file":      "providers.mango-mangaplus.secret_file",
}

// renamedSecret are the renamed keys that hold secrets, by their new name.
var renamedSecret = []string{"providers.mango-mangaplus.android_id", "providers.mango-mangaplus.free_secret"}

// MovedKey is a key of mangal.toml that's stored in the database now: the file's entry
// is ignored. NewKey is its name now (a provider's setting), Key's otherwise.
type MovedKey struct {
	Key, NewKey string
}

// movedKeys are the keys whose file entries are ignored: the database keys and the
// renamed ones.
func movedKeys() []string {
	var keys []string
	for _, key := range Keys() {
		if StoredIn(key) == StoredDB {
			keys = append(keys, key)
		}
	}
	return append(keys, slices.Sorted(maps.Keys(renamed))...)
}

func newName(key string) string {
	if n, ok := renamed[key]; ok {
		return n
	}
	return key
}

// Store is where the database keys are kept, the data database's settings
// (storage.Settings). Values are JSON.
type Store interface {
	All() (map[string]string, int64, error)
	Revision() (int64, error)
	Set(key string, value *string, secret bool, who string) error
	Imported() (bool, error)
}

// ErrNoStore is a write of a database key without the database (it couldn't be opened).
var ErrNoStore = errors.New("it's stored in the database, which isn't open")

// Who is who changes the database keys, in their history: the command, set at startup.
var Who = meta.AppName

// The stored layer of viper (its "config"): the file's values without the moved keys,
// then the database's. Guarded by lmu.
var (
	lmu sync.Mutex
	// fileSettings are the file's values without the moved keys.
	fileSettings = map[string]any{}
	// movedInFile are the moved keys set in the file, their raw values by file key.
	movedInFile = map[string]any{}
	store       Store
	// stored are the database's values (JSON) by key, at storeRev.
	stored   = map[string]string{}
	storeRev int64
	imported bool
	// badStored are the invalid stored values already warned about.
	badStored = map[string]string{}
)

// applyLocked makes the file's and the database's values viper's config, lmu held.
func applyLocked() {
	m := cloneMap(fileSettings)
	for _, key := range slices.Sorted(maps.Keys(stored)) {
		if raw, ok := storedRawLocked(key); ok {
			setNested(m, strings.Split(key, "."), raw)
		}
	}
	vmu.Lock()
	defer vmu.Unlock()
	// an empty TOML document: viper's config is replaced, not merged
	_ = viper.ReadConfig(strings.NewReader(""))
	_ = viper.MergeConfigMap(m)
}

// storedRawLocked is the key's valid value in the database, lmu held. Keys that aren't
// registered (a provider not loaded yet) have none.
func storedRawLocked(key string) (any, bool) {
	text, ok := stored[key]
	if !ok {
		return nil, false
	}
	f, err := lookup(key)
	if err != nil {
		return nil, false
	}
	var v any
	err = json.Unmarshal([]byte(text), &v)
	var raw any
	if err == nil {
		raw, err = f.toRaw(v)
	}
	if err != nil {
		if badStored[key] != text {
			badStored[key] = text
			log.Warn("config: %s in the database is invalid, its default is used: %s", key, err)
		}
		return nil, false
	}
	return raw, true
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			v = cloneMap(sub)
		}
		out[k] = v
	}
	return out
}

func getNested(m map[string]any, path []string) (any, bool) {
	for _, k := range path[:len(path)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			return nil, false
		}
		m = next
	}
	v, ok := m[path[len(path)-1]]
	return v, ok
}

// UseStore reads the database keys from s, and keeps using it: writes of database keys
// go to it, Fresh reads it again when its revision moved.
func UseStore(s Store) error {
	values, rev, err := s.All()
	if err != nil {
		return err
	}
	done, err := s.Imported()
	if err != nil {
		return err
	}
	lmu.Lock()
	defer lmu.Unlock()
	store, stored, storeRev, imported = s, values, rev, done
	applyLocked()
	return nil
}

// refreshStore reads the database keys again if another process changed them.
func refreshStore() {
	lmu.Lock()
	defer lmu.Unlock()
	if store == nil {
		return
	}
	rev, err := store.Revision()
	if err != nil || rev == storeRev {
		if err != nil {
			log.Warn("config: %s", err)
		}
		return
	}
	values, rev, err := store.All()
	if err != nil {
		log.Warn("config: %s", err)
		return
	}
	stored, storeRev = values, rev
	applyLocked()
}

// Imported is true once mangal.toml's moved keys were copied into the database (it's
// open and was imported).
func Imported() bool {
	lmu.Lock()
	defer lmu.Unlock()
	return store != nil && imported
}

// MovedInFile are the moved keys set in the loaded file, ignored: the database has
// them (or their defaults) once imported.
func MovedInFile() []MovedKey {
	lmu.Lock()
	defer lmu.Unlock()
	var moved []MovedKey
	for _, key := range slices.Sorted(maps.Keys(movedInFile)) {
		moved = append(moved, MovedKey{Key: key, NewKey: newName(key)})
	}
	return moved
}

// MovedValues are the values of the moved keys in the loaded file, as the database
// stores them, by their new name, for the one-time import. Invalid values are left out,
// with why in skipped.
func MovedValues() (values map[string]string, skipped []string) {
	lmu.Lock()
	defer lmu.Unlock()
	values = map[string]string{}
	for _, key := range slices.Sorted(maps.Keys(movedInFile)) {
		raw := movedInFile[key]
		if f, err := lookup(key); err == nil {
			if raw, err = f.toRaw(raw); err != nil {
				skipped = append(skipped, fmt.Sprintf("%s: %s", key, err))
				continue
			}
		}
		data, err := json.Marshal(raw)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %s", key, err))
			continue
		}
		values[newName(key)] = string(data)
	}
	return values, skipped
}

// MovedSecret is true for a moved key's new name that holds a secret.
func MovedSecret(key string) bool {
	return IsSecret(key) || slices.Contains(renamedSecret, key)
}

// writeStored writes a database key: its default unsets it (it follows the default).
func writeStored(key string, value any, who string) error {
	text, err := StoredText(key, value)
	if err != nil {
		return err
	}
	lmu.Lock()
	s := store
	lmu.Unlock()
	if s == nil {
		return fmt.Errorf("write config key %q: %w", key, ErrNoStore)
	}
	if err := s.Set(key, text, IsSecret(key), who); err != nil {
		return fmt.Errorf("write config key %q: %w", key, err)
	}
	refreshStore()
	return nil
}

// isStoredDefault is isDefault for a database key, maps too: an empty map is an empty
// default (the file keeps maps as tables, so isDefault never says so for them).
func isStoredDefault(key string, raw any) bool {
	if isDefault(key, raw) {
		return true
	}
	f, err := lookup(key)
	if err != nil || !f.isMap() {
		return false
	}
	def := f.defaultRaw()
	if reflect.DeepEqual(raw, def) {
		return true
	}
	a, b := reflect.ValueOf(raw), reflect.ValueOf(def)
	return a.Kind() == reflect.Map && b.Kind() == reflect.Map && a.Len() == 0 && b.Len() == 0
}

// storedValue is the database key's stored value (read again), or its default.
func storedValue(key string, f field) any {
	refreshStore()
	lmu.Lock()
	defer lmu.Unlock()
	if raw, ok := storedRawLocked(key); ok {
		return raw
	}
	return f.defaultRaw()
}

// Source is where a key's value comes from in this process, highest first: set for
// this run (Set), a flag, the environment, the stored value (mangal.toml or the
// database), the default (the provider's for its settings).
type Source struct {
	// Kind is "run", "flag", "env", "file", "db" or "default".
	Kind string `json:"kind"`
	// Name is the flag or the environment variable.
	Name string `json:"name,omitempty"`
}

// SourceOf is where the key's value comes from.
func SourceOf(key string) Source {
	vmu.RLock()
	run, flag := runSet[key], flags[key]
	vmu.RUnlock()
	switch {
	case run:
		return Source{Kind: "run"}
	case flag != nil && flag.Changed:
		return Source{Kind: "flag", Name: "--" + flag.Name}
	}
	if name, set := EnvVar(key); set {
		return Source{Kind: "env", Name: name}
	}
	lmu.Lock()
	defer lmu.Unlock()
	if StoredIn(key) == StoredDB {
		if _, ok := storedRawLocked(key); ok {
			return Source{Kind: string(StoredDB)}
		}
	} else if _, ok := getNested(fileSettings, strings.Split(key, ".")); ok {
		return Source{Kind: string(StoredFile)}
	}
	return Source{Kind: "default"}
}
