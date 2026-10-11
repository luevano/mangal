package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/adrg/xdg"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// ErrUnknownKey is matched by the errors about a key that isn't registered.
var ErrUnknownKey = errors.New("unknown config key")

type unknownKeyError string

func (e unknownKeyError) Error() string        { return fmt.Sprintf("unknown config key %q", string(e)) }
func (e unknownKeyError) Is(target error) bool { return target == ErrUnknownKey }

// unsupported are the keys of the loaded file that aren't registered.
var unsupported []string

func init() {
	setup()
}

// vmu guards viper, which isn't safe for concurrent use: the TUI changes values while
// background work (downloads, checks) reads them.
var vmu sync.RWMutex

func viperGet(key string) any {
	vmu.RLock()
	defer vmu.RUnlock()
	return viper.Get(key)
}

func viperSet(key string, value any) {
	vmu.Lock()
	defer vmu.Unlock()
	viper.Set(key, value)
}

// setup configures viper and sets the defaults, nothing is read here.
func setup() {
	vmu.Lock()
	defer vmu.Unlock()
	viper.KeyDelimiter(".")
	viper.SetFs(afs.Afero.Fs)
	viper.SetConfigType("toml")

	// Accept environment variables, all uppercase with "MANGAL_" prefix and "_" for ".",
	// e.g. MANGAL_DOWNLOAD_PATH for download.path
	viper.SetEnvPrefix(meta.AppName) // uppercased automatically
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	for key, f := range fields {
		viper.SetDefault(key, f.defaultRaw())
	}
}

// Reset forgets the loaded file and the values set at runtime, for tests.
func Reset() {
	vmu.Lock()
	viper.Reset()
	vmu.Unlock()
	setup()
	unsupported = nil
}

func lookup(key string) (field, error) {
	f, ok := fields[key]
	if !ok {
		return nil, unknownKeyError(key)
	}
	return f, nil
}

// Get returns the value of the key as stored in the file (e.g. a format as a string).
func Get(key string) (any, error) {
	f, err := lookup(key)
	if err != nil {
		return nil, err
	}
	raw, err := f.toRaw(viperGet(key))
	if err != nil {
		return nil, fmt.Errorf("config key %q: %w", key, err)
	}
	return raw, nil
}

// Default value of the key, as stored in the file.
func Default(key string) (any, error) {
	f, err := lookup(key)
	if err != nil {
		return nil, err
	}
	return f.defaultRaw(), nil
}

// Description of the key.
func Description(key string) (string, error) {
	f, err := lookup(key)
	if err != nil {
		return "", err
	}
	return f.description(), nil
}

// Kind is the type of a key's value in the file.
type Kind int

const (
	KindString Kind = iota
	KindBool
	KindInt
	// KindMap is a table of strings
	KindMap
)

// KindOf the key, KindString for unknown keys.
func KindOf(key string) Kind {
	if f, ok := fields[key]; ok {
		return f.kind()
	}
	return KindString
}

// Values are the accepted values of an enum key, nil for the others.
func Values(key string) []string {
	if f, ok := fields[key]; ok {
		return f.values()
	}
	return nil
}

// EnvVar is the environment variable of the key, set if it has a value from it (which
// wins over the file).
func EnvVar(key string) (name string, set bool) {
	name = envName(key)
	_, set = os.LookupEnv(name)
	return name, set
}

// FileValue is the key's value in the config file (or its default), ignoring the
// environment and flags that may override it for this run.
func FileValue(key string) (any, error) {
	f, err := lookup(key)
	if err != nil {
		return nil, err
	}
	file := viper.New()
	file.SetFs(afs.Afero.Fs)
	file.SetConfigFile(Path)
	if err := file.ReadInConfig(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read config %q: %w", Path, err)
	}
	if !file.IsSet(key) {
		return f.defaultRaw(), nil
	}
	raw, err := f.toRaw(file.Get(key))
	if err != nil {
		return nil, fmt.Errorf("config key %q: %w", key, err)
	}
	return raw, nil
}

// IsSecret is true for keys that hold secrets (tokens, webhooks).
func IsSecret(key string) bool {
	f, ok := fields[key]
	return ok && f.secret()
}

// Set a value for the key for this run, it isn't written to the file (see WriteKey). The
// value can be the raw one (as in the file), the typed one or a string.
func Set(key string, value any) error {
	raw, err := Parse(key, value)
	if err != nil {
		return err
	}
	viperSet(key, raw)
	return nil
}

// Parse validates the value for the key and returns it as stored in the file. The value
// can be the raw one, the typed one or a string (e.g. "5" for an int key).
func Parse(key string, value any) (any, error) {
	f, err := lookup(key)
	if err != nil {
		return nil, err
	}
	raw, err := f.toRaw(value)
	if err != nil {
		return nil, fmt.Errorf("config key %q: %w", key, err)
	}
	return raw, nil
}

// BindPFlag makes the flag, when given, the value of the key.
func BindPFlag(key string, flag *pflag.Flag) error {
	if _, err := lookup(key); err != nil {
		return err
	}
	vmu.Lock()
	err := viper.BindPFlag(key, flag)
	vmu.Unlock()
	if err != nil {
		return fmt.Errorf("bind flag %q to config key %q: %w", flag.Name, key, err)
	}
	return nil
}

// Exists if there is a registered key.
func Exists(key string) bool {
	_, found := fields[key]
	return found
}

// Keys returns all the registered keys, sorted.
func Keys() []string {
	return slices.Sorted(maps.Keys(fields))
}

// retired keys were removed from mangal; old config files still have them, they're ignored
// without a warning (and config write drops them).
var retired = map[string]bool{
	// never implemented, the library is the download path
	"library.path": true,
	// the databases are always 0600, they hold logins
	"download.mode_db_decimal": true,
	// saved with the logins (mangal myanimelist client), moved by RetiredValue
	"metadata.myanimelist.client_id":     true,
	"metadata.myanimelist.client_secret": true,
}

// RetiredValue is the value of a retired key in the loaded config file (or its environment
// variable), to move it elsewhere. Empty if unset.
func RetiredValue(key string) string {
	if !retired[key] {
		return ""
	}
	value, _ := viperGet(key).(string)
	return strings.TrimSpace(value)
}

// Unsupported are the keys of the loaded config file that mangal doesn't know (e.g. removed
// ones), sorted.
func Unsupported() []string {
	return unsupported
}

// envName is the environment variable of the key.
func envName(key string) string {
	return strings.ToUpper(meta.AppName + "_" + strings.ReplaceAll(key, ".", "_"))
}

// Load reads the config file at path. An empty path is the default location, which
// doesn't need to exist; an explicit path does.
//
// The values aren't validated here: Validate does it once the flags are parsed.
func Load(path string) error {
	explicit := path != ""
	if !explicit {
		path = defaultPath()
	}
	Path = path

	vmu.Lock()
	viper.SetConfigFile(path)
	err := viper.ReadInConfig()
	keys := viper.AllKeys()
	vmu.Unlock()
	if err != nil {
		if explicit || !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("read config %q: %w", path, err)
		}
	}

	unsupported = nil
	for _, key := range keys {
		if !Exists(key) && !underMap(key) && !retired[key] {
			unsupported = append(unsupported, key)
		}
	}
	slices.Sort(unsupported)
	return nil
}

// defaultPath is the config file in the config dir, or the one in the home dir if only that
// one exists (viper used to look there too).
func defaultPath() string {
	path := filepath.Join(dir, filename)
	if _, err := afs.Afero.Stat(path); err == nil {
		return path
	}
	home := filepath.Join(xdg.Home, filename)
	if _, err := afs.Afero.Stat(home); err == nil {
		return home
	}
	return path
}

// underMap is true for an entry of a map key, e.g. providers.mirrors.<id>.
func underMap(key string) bool {
	for k, f := range fields {
		if f.isMap() && strings.HasPrefix(key, k+".") {
			return true
		}
	}
	return false
}

// Validate checks the value of every key, wherever it comes from (file, environment,
// flags).
func Validate() error {
	var errs []error
	for _, key := range Keys() {
		if _, err := fields[key].toRaw(viperGet(key)); err != nil {
			if _, ok := os.LookupEnv(envName(key)); ok {
				err = fmt.Errorf("from %s: %w", envName(key), err)
			}
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
	}
	if len(errs) != 0 {
		return fmt.Errorf("invalid config: %w", errors.Join(errs...))
	}
	return nil
}

// WriteKey sets the key and writes only that key to the existing config file: its value
// is edited in place (or added to its table), the rest of the file is kept as it is,
// comments included. Errors if there's no config file, it's never created here (see
// CreateFile).
//
// Unlike Write, runtime values (e.g. from flags) and missing defaults aren't written.
func WriteKey(key string, value any) error {
	raw, err := Parse(key, value)
	if err != nil {
		return err
	}
	data, err := afs.Afero.ReadFile(Path)
	if err != nil {
		return fmt.Errorf("write config key %q: %w", key, err)
	}
	edited, err := editTOML(data, key, raw)
	if err == nil {
		err = checkTOMLEdit(data, edited, key, raw)
	}
	if err != nil {
		// TODO: drop the fallback once the editor has seen enough files
		log.Warn("config: couldn't edit %q in place (%s), the file is re-serialised (its comments are lost)", key, err)
		if err := rewriteKey(key, raw); err != nil {
			return fmt.Errorf("write config key %q: %w", key, err)
		}
	} else {
		mode := fs.FileMode(0o644)
		if info, err := afs.Afero.Stat(Path); err == nil {
			mode = info.Mode().Perm()
		}
		if err := afs.Afero.WriteFile(Path, edited, mode); err != nil {
			return fmt.Errorf("write config key %q: %w", key, err)
		}
	}
	viperSet(key, raw)
	return nil
}

// rewriteKey writes the key by re-serialising the whole file.
func rewriteKey(key string, raw any) error {
	file := viper.New()
	file.SetFs(afs.Afero.Fs)
	file.SetConfigFile(Path)
	if err := file.ReadInConfig(); err != nil {
		return err
	}
	// through the settings map and not Set: viper would merge a map value with the file's
	settings := file.AllSettings()
	setNested(settings, strings.Split(key, "."), raw)
	out := viper.New()
	out.SetFs(afs.Afero.Fs)
	out.SetConfigFile(Path)
	if err := out.MergeConfigMap(settings); err != nil {
		return err
	}
	return out.WriteConfig()
}

func setNested(m map[string]any, path []string, value any) {
	for _, k := range path[:len(path)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[path[len(path)-1]] = value
}

// CreateFile creates an empty config file (and its directory) at Path if there's none.
func CreateFile() error {
	if _, err := afs.Afero.Stat(Path); err == nil {
		return nil
	}
	if err := afs.Afero.MkdirAll(filepath.Dir(Path), 0o755); err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	if err := afs.Afero.WriteFile(Path, nil, 0o644); err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	return nil
}

// Write writes every key to the config file: the file's values and the defaults of the
// keys it doesn't have (values from the environment or flags aren't written). The file
// is re-serialised (comments and order are lost), and created if there's none.
func Write() error {
	if data, err := afs.Afero.ReadFile(Path); err == nil {
		edited, err := writeInPlace(data)
		if err == nil {
			mode := fs.FileMode(0o644)
			if info, err := afs.Afero.Stat(Path); err == nil {
				mode = info.Mode().Perm()
			}
			if err := afs.Afero.WriteFile(Path, edited, mode); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			return nil
		}
		log.Warn("config: couldn't write the defaults in place (%s), the file is re-serialised (its comments are lost)", err)
	}
	return rewrite()
}

// writeInPlace adds the missing keys with their defaults and drops the retired ones,
// keeping the comments and order of the file.
func writeInPlace(data []byte) ([]byte, error) {
	file := viper.New()
	file.SetConfigType("toml")
	if err := file.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, err
	}
	var err error
	for _, key := range slices.Sorted(maps.Keys(retired)) {
		if file.IsSet(key) {
			if data, err = removeTOMLKey(data, key); err != nil {
				return nil, err
			}
		}
	}
	for _, key := range Keys() {
		if file.IsSet(key) {
			continue
		}
		raw := fields[key].defaultRaw()
		if m, ok := raw.(map[string]any); ok && len(m) == 0 {
			continue
		}
		edited, err := editTOML(data, key, raw)
		if err == nil {
			err = checkTOMLEdit(data, edited, key, raw)
		}
		if err != nil {
			return nil, err
		}
		data = edited
	}
	return data, nil
}

// rewrite writes the file's values plus the defaults by re-serialising it.
func rewrite() error {
	file := viper.New()
	file.SetFs(afs.Afero.Fs)
	file.SetConfigFile(Path)
	if err := file.ReadInConfig(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("write config: %w", err)
	}
	settings := file.AllSettings()
	for key := range retired {
		deleteNested(settings, strings.Split(key, "."))
	}
	for _, key := range Keys() {
		if !file.IsSet(key) {
			setNested(settings, strings.Split(key, "."), fields[key].defaultRaw())
		}
	}
	out := viper.New()
	out.SetFs(afs.Afero.Fs)
	out.SetConfigFile(Path)
	if err := out.MergeConfigMap(settings); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := afs.Afero.MkdirAll(filepath.Dir(Path), 0o755); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := out.WriteConfig(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// deleteNested deletes the key at path from nested maps, and the maps it empties.
func deleteNested(m map[string]any, path []string) {
	if len(path) == 1 {
		delete(m, path[0])
		return
	}
	sub, ok := m[path[0]].(map[string]any)
	if !ok {
		return
	}
	deleteNested(sub, path[1:])
	if len(sub) == 0 {
		delete(m, path[0])
	}
}
