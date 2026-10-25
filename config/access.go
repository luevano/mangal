package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/viper"
)

// riskyKeys and riskyPrefixes weaken access when changed: the server's own security, the
// guard that keeps the providers off the LAN and what reaches past it, where mangal keeps
// its data and runs providers from. Over HTTP only an admin sees and changes them.
var (
	riskyPrefixes = []string{"serve.", "auth."}
	riskyKeys     = []string{
		"providers.allow_private_addresses", "providers.private_allowlist", "providers.headless.flaresolverr_url",
		"providers.mirrors", "data.path", "cache.path", "providers.path",
	}
)

// IsRisky is true for a key only an admin may see or change over HTTP; a provider's path
// setting too (it reads a file of the server).
func IsRisky(key string) bool {
	if slices.Contains(riskyKeys, key) || slices.ContainsFunc(riskyPrefixes, func(p string) bool { return strings.HasPrefix(key, p) }) {
		return true
	}
	pmu.RLock()
	defer pmu.RUnlock()
	f, ok := providerFields[key]
	return ok && f.setting.Kind == libmangal.SettingPath
}

// startupPrefixes are read once when mangal starts: where the databases are, the log, the
// keyring, how serve listens.
var startupPrefixes = []string{"data.path", "cache.path", "log.", "auth.", "serve."}

// reloadPrefixes are the options the providers are loaded with (their caches too).
var reloadPrefixes = []string{"providers.", "metadata.", "cache.", "download.user_agent", "download.mode_dir_decimal", "download.mode_file_decimal"}

// Startup is true for a key read once when mangal starts: a change needs a restart.
func Startup(key string) bool {
	return StoredIn(key) == StoredFile && hasPrefix(key, startupPrefixes)
}

func hasPrefix(key string, prefixes []string) bool {
	return slices.ContainsFunc(prefixes, func(p string) bool { return key == p || strings.HasPrefix(key, p) })
}

// Applies is when a change of the key applies in a running mangal: "restart" (Startup),
// "reload" (when the providers load again: the TUI's providers list, serve's next
// start) or "now".
func Applies(key string) string {
	switch {
	case Startup(key):
		return "restart"
	case hasPrefix(key, reloadPrefixes):
		return "reload"
	}
	return "now"
}

// The startup keys' values this process started with, and the last RestartNeeded,
// guarded by rmu.
var (
	rmu       sync.Mutex
	started   map[string]any
	restartAt struct {
		mod  time.Time
		size int64
	}
	restartKeys []string
)

// startupValues are the startup keys' values in the file's settings (or their defaults).
func startupValues(settings map[string]any) map[string]any {
	values := map[string]any{}
	for key, f := range fields {
		if !Startup(key) {
			continue
		}
		raw := f.defaultRaw()
		if v, ok := getNested(settings, strings.Split(key, ".")); ok {
			if r, err := f.toRaw(v); err == nil {
				raw = r
			}
		}
		values[key] = raw
	}
	return values
}

// noteStarted keeps the startup keys' values of the first loaded file.
func noteStarted(settings map[string]any) {
	rmu.Lock()
	defer rmu.Unlock()
	if started == nil {
		started = startupValues(settings)
	}
}

// RestartNeeded are the startup keys whose value in the config file changed since this
// process started (by any process), so it uses the old value until it restarts. Keys
// an environment variable or a flag sets are left out: they keep winning.
func RestartNeeded() []string {
	info, err := afs.Afero.Stat(Path)
	rmu.Lock()
	defer rmu.Unlock()
	if started == nil {
		return nil
	}
	if err == nil && info.ModTime().Equal(restartAt.mod) && info.Size() == restartAt.size {
		return slices.Clone(restartKeys)
	}
	settings := map[string]any{}
	if err == nil {
		file := viper.New()
		file.SetFs(afs.Afero.Fs)
		file.SetConfigFile(Path)
		file.SetConfigType("toml")
		if err := file.ReadInConfig(); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return slices.Clone(restartKeys)
		}
		settings = file.AllSettings()
		restartAt.mod, restartAt.size = info.ModTime(), info.Size()
	}
	restartKeys = nil
	for key, now := range startupValues(settings) {
		if src := SourceOf(key).Kind; src == "env" || src == "flag" {
			continue
		}
		if !reflect.DeepEqual(now, started[key]) {
			restartKeys = append(restartKeys, key)
		}
	}
	slices.Sort(restartKeys)
	return slices.Clone(restartKeys)
}

func resetStarted() {
	rmu.Lock()
	defer rmu.Unlock()
	started, restartKeys = nil, nil
	restartAt.mod, restartAt.size = time.Time{}, 0
}

// Export is every database key's stored value (or its default) as TOML, secrets left
// out: a file to edit and load again with ImportPlan.
func Export() ([]byte, error) {
	m := map[string]any{}
	for _, key := range Keys() {
		if StoredIn(key) != StoredDB || IsSecret(key) {
			continue
		}
		raw, err := FileValue(key)
		if err != nil {
			return nil, err
		}
		setNested(m, strings.Split(key, "."), raw)
	}
	data, err := toml.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("export the settings: %w", err)
	}
	header := "# mangal settings stored in the database (secrets left out).\n# Edit and load with: mangal config import <file> --apply\n\n"
	return append([]byte(header), data...), nil
}

// ImportChange is a database key whose stored value an import changes, Old and New as
// stored (New is the default to unset it).
type ImportChange struct {
	Key      string
	Old, New any
}

// ImportPlan reads a TOML file of settings (as Export writes it) and says what it would
// change: the database keys whose value differs from the stored one. Keys of
// mangal.toml, unknown ones and invalid values are errors, nothing is changed.
func ImportPlan(data []byte) ([]ImportChange, error) {
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("read the settings: %w", err)
	}
	var changes []ImportChange
	var errs []error
	for _, key := range leafKeys(m, "") {
		if !Exists(key) {
			errs = append(errs, fmt.Errorf("%s: not a mangal key", key))
			continue
		}
		if StoredIn(key) != StoredDB {
			errs = append(errs, fmt.Errorf("%s: kept in mangal.toml, not the database (mangal config set)", key))
			continue
		}
		v, _ := getNested(m, strings.Split(key, "."))
		raw, err := Parse(key, v)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		old, err := FileValue(key)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !sameStored(key, old, raw) {
			changes = append(changes, ImportChange{Key: key, Old: old, New: raw})
		}
	}
	if len(errs) != 0 {
		return nil, fmt.Errorf("invalid settings: %w", errors.Join(errs...))
	}
	return changes, nil
}

// leafKeys are the keys of m: its values, a map key's table as one.
func leafKeys(m map[string]any, prefix string) []string {
	var keys []string
	for _, k := range slices.Sorted(maps.Keys(m)) {
		key := prefix + k
		if sub, ok := m[k].(map[string]any); ok && !(Exists(key) && KindOf(key) == KindMap) {
			keys = append(keys, leafKeys(sub, key+".")...)
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func sameStored(key string, a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb) || isStoredDefault(key, a) && isStoredDefault(key, b)
}

// StoredText is the value as the database stores it (JSON), nil for the default: the
// key is unset then.
func StoredText(key string, value any) (*string, error) {
	raw, err := Parse(key, value)
	if err != nil {
		return nil, err
	}
	if isStoredDefault(key, raw) {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("config key %q: %w", key, err)
	}
	return new(string(data)), nil
}

// Refresh reads the database keys again if they changed, e.g. after writing them to the
// store directly.
func Refresh() { refreshStore() }
