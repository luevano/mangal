package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/adrg/xdg"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/util/afs"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// ErrUnknownKey is returned for a key that isn't registered.
var ErrUnknownKey = errors.New("unknown config key")

// unsupported are the keys of the loaded file that aren't registered.
var unsupported []string

func init() {
	setup()
}

// setup configures viper and sets the defaults, nothing is read here.
func setup() {
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

func lookup(key string) (field, error) {
	f, ok := fields[key]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownKey, key)
	}
	return f, nil
}

// Get returns the value of the key as stored in the file (e.g. a format as a string).
func Get(key string) (any, error) {
	f, err := lookup(key)
	if err != nil {
		return nil, err
	}
	raw, err := f.toRaw(viper.Get(key))
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
	viper.Set(key, raw)
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
	if err := viper.BindPFlag(key, flag); err != nil {
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

	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		if explicit || !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("read config %q: %w", path, err)
		}
	}

	unsupported = nil
	for _, key := range viper.AllKeys() {
		if !Exists(key) && !underMap(key) {
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
		if _, err := fields[key].toRaw(viper.Get(key)); err != nil {
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

// WriteKey sets the key and writes only that key to the existing config file: the other
// keys keep their values, but the file is re-serialised (comments and order are lost).
// Errors if there's no config file, it's never created here (see CreateFile).
//
// Unlike Write, runtime values (e.g. from flags) and missing defaults aren't written.
func WriteKey(key string, value any) error {
	raw, err := Parse(key, value)
	if err != nil {
		return err
	}

	file := viper.New()
	file.SetFs(afs.Afero.Fs)
	file.SetConfigFile(Path)
	if err := file.ReadInConfig(); err != nil {
		return fmt.Errorf("write config key %q: %w", key, err)
	}
	// through the settings map and not Set: viper would merge a map value with the file's
	settings := file.AllSettings()
	setNested(settings, strings.Split(key, "."), raw)
	out := viper.New()
	out.SetFs(afs.Afero.Fs)
	out.SetConfigFile(Path)
	if err := out.MergeConfigMap(settings); err != nil {
		return fmt.Errorf("write config key %q: %w", key, err)
	}
	if err := out.WriteConfig(); err != nil {
		return fmt.Errorf("write config key %q: %w", key, err)
	}
	viper.Set(key, raw)
	return nil
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
	file := viper.New()
	file.SetFs(afs.Afero.Fs)
	file.SetConfigFile(Path)
	if err := file.ReadInConfig(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("write config: %w", err)
	}
	settings := file.AllSettings()
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
