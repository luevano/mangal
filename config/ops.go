package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
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

func (e unknownKeyError) Error() string {
	if s := Suggest(string(e)); s != "" {
		return fmt.Sprintf("unknown config key %q (did you mean %q?)", string(e), s)
	}
	return fmt.Sprintf("unknown config key %q", string(e))
}
func (e unknownKeyError) Is(target error) bool { return target == ErrUnknownKey }

// unsupported are the keys of the loaded file that aren't registered, retiredSet the
// retired ones it has.
var unsupported, retiredSet []string

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
	unsupported, retiredSet = nil, nil
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

// Masked is the value as shown to the user: "••••••" for a set secret (config get still
// prints it).
func Masked(key string, value any) any {
	if IsSecret(key) && value != nil && fmt.Sprint(value) != "" {
		return "••••••"
	}
	return value
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

// retired keys were removed from mangal, with why; old config files still have them,
// they're ignored (a warning says so, config write and config clean drop them).
var retired = map[string]string{
	"library.path":             "never implemented, the library is download.path",
	"download.mode_db_decimal": "the databases are always 0600, they hold logins",
	// moved by RetiredValue
	"metadata.myanimelist.client_id":     "saved with the logins now, see mangal myanimelist client",
	"metadata.myanimelist.client_secret": "saved with the logins now, see mangal myanimelist client",
	// moved by RetiredValue
	"serve.token":             "an API token named config now, see mangal token ls; people log in as users, see mangal user",
	"serve.token_on_loopback": "every request logs in now, the same machine too",
	"serve.loopback_scopes":   "every request logs in now, the same machine too; mangal on this config uses the server's key",
	"serve.allowed_hosts":     "every request logs in now, any host name is answered",
	"serve.allowed_origins":   "the browsers' Sec-Fetch-Site tells same-origin requests apart, behind a proxy too",
	"web.host":                "mangal web uses serve.host",
	"web.port":                "mangal web uses serve.port",
}

// RetiredValue is the value of a retired key in the loaded config file (or its environment
// variable), to move it elsewhere. Empty if unset.
func RetiredValue(key string) string {
	if _, ok := retired[key]; !ok {
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

// Retired are the retired keys of the loaded config file, sorted.
func Retired() []string {
	return retiredSet
}

// RetiredReason is why the retired key was removed, "" if it isn't one.
func RetiredReason(key string) string {
	return retired[key]
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

	unsupported, retiredSet = nil, nil
	for _, key := range keys {
		if _, ok := retired[key]; ok {
			retiredSet = append(retiredSet, key)
		} else if !Exists(key) && !underMap(key) {
			unsupported = append(unsupported, key)
		}
	}
	slices.Sort(unsupported)
	slices.Sort(retiredSet)
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
// comments included. The default value is written as a commented default ("# key =
// value"): the key follows mangal's default from then on. Errors if there's no config
// file, it's never created here (see CreateFile).
//
// Unlike Write, runtime values (e.g. from flags) and missing defaults aren't written.
func WriteKey(key string, value any) error {
	raw, err := Parse(key, value)
	if err != nil {
		return err
	}
	data, err := afs.Afero.ReadFile(Path)
	if errors.Is(err, fs.ErrNotExist) {
		// never created behind the user's back (David)
		return fmt.Errorf("write config key %q: %w", key, ErrNoFile)
	}
	if err != nil {
		return fmt.Errorf("write config key %q: %w", key, err)
	}
	var edited []byte
	if isDefault(key, raw) {
		edited, err = defaultTOML(data, key, raw)
		if err == nil {
			err = checkTOMLRemoved(data, edited, key)
		}
	} else {
		edited, err = editTOML(data, key, raw)
		if err == nil {
			err = checkTOMLEdit(data, edited, key, raw)
		}
	}
	if err != nil {
		// TODO: drop the fallback once the editor has seen enough files
		log.Warn("config: couldn't edit %q in place (%s), the file is re-serialised (its comments are lost)", key, err)
		if err := rewriteKey(key, raw); err != nil {
			return fmt.Errorf("write config key %q: %w", key, err)
		}
	} else if err := writeFile(edited); err != nil {
		return fmt.Errorf("write config key %q: %w", key, err)
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
	return writeSettings(settings)
}

// writeSettings re-serialises the settings into the file.
func writeSettings(settings map[string]any) error {
	data, err := encodeSettings(settings)
	if err != nil {
		return err
	}
	return writeFile(data)
}

// encodeSettings serialises the settings as the config file's type.
func encodeSettings(settings map[string]any) ([]byte, error) {
	out := viper.New()
	configType := strings.TrimPrefix(filepath.Ext(Path), ".")
	if configType == "" {
		configType = "toml"
	}
	out.SetConfigType(configType)
	if err := out.MergeConfigMap(settings); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := out.WriteConfigTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// isTOML is true when the config file is TOML (the in-place editor only edits TOML).
func isTOML() bool {
	ext := filepath.Ext(Path)
	return ext == "" || strings.EqualFold(ext, ".toml")
}

// writeFile replaces the config file atomically; a new one is only readable by the user
// (it can hold secrets).
func writeFile(data []byte) error {
	return afs.WriteFileAtomic(Path, data, 0o600)
}

// ReadableByOthers is true when the config file exists and its group or others can read it.
func ReadableByOthers() (fs.FileMode, bool) {
	info, err := afs.Afero.Stat(Path)
	if err != nil {
		return 0, false
	}
	return info.Mode().Perm(), info.Mode().Perm()&0o044 != 0
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

// ErrNoFile is a write that needs the config file, which doesn't exist: mangal never
// creates it on its own, `mangal config write` (or the TUI's config window) does.
var ErrNoFile = errors.New("no config file, `mangal config write` creates one")

// CreateFile creates an empty config file (and its directory) at Path if there's none.
func CreateFile() error {
	if _, err := afs.Afero.Stat(Path); err == nil {
		return nil
	}
	if err := writeFile(nil); err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	return nil
}

// Write writes every key to the config file: the file's values, and the keys it doesn't
// have as commented defaults ("# key = value", still the default: they follow mangal's)
// so the file lists every key. Values from the environment or flags aren't written,
// retired keys are dropped. The file is edited in place (comments and order stay), and
// created if there's none.
func Write() error {
	if !isTOML() {
		return rewrite()
	}
	data, err := afs.Afero.ReadFile(Path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("write config: %w", err)
	}
	edited, err := writeInPlace(data)
	if err == nil {
		if err := writeFile(edited); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		return nil
	}
	log.Warn("config: couldn't write the defaults in place (%s), the file is re-serialised (its comments are lost)", err)
	return rewrite()
}

// writeInPlace adds the missing keys as commented defaults (refreshing the ones there)
// and drops the retired keys, keeping the comments and order of the file.
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
		if data, err = removeTOMLDefault(data, key); err != nil {
			return nil, err
		}
	}
	for _, key := range Keys() {
		if file.IsSet(key) {
			continue
		}
		raw := fields[key].defaultRaw()
		var edited []byte
		if m, ok := raw.(map[string]any); ok {
			if len(m) == 0 {
				continue
			}
			// a table can't be a comment
			edited, err = editTOML(data, key, raw)
			if err == nil {
				err = checkTOMLEdit(data, edited, key, raw)
			}
		} else {
			edited, err = defaultTOML(data, key, raw)
			if err == nil {
				err = checkTOMLRemoved(data, edited, key)
			}
		}
		if err != nil {
			return nil, err
		}
		data = edited
	}
	return data, nil
}

// isDefault is true when raw is the key's default, written as a commented default.
func isDefault(key string, raw any) bool {
	f, ok := fields[key]
	if !ok || f.isMap() {
		return false
	}
	def := f.defaultRaw()
	if a, ok := raw.(string); ok && f.values() != nil {
		b, _ := def.(string)
		return strings.EqualFold(a, b)
	}
	return reflect.DeepEqual(raw, def)
}

// rewrite re-serialises the file's values and adds the defaults: as comments when the
// re-serialised file can be edited, else as values.
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
	if data, err := encodeSettings(settings); err == nil && isTOML() {
		if edited, err := writeInPlace(data); err == nil {
			return writeFile(edited)
		}
	}
	for _, key := range Keys() {
		if !file.IsSet(key) {
			setNested(settings, strings.Split(key, "."), fields[key].defaultRaw())
		}
	}
	if err := writeSettings(settings); err != nil {
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
