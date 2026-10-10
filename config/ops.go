package config

import (
	"fmt"
	"strings"

	"github.com/adrg/xdg"
	"github.com/luevano/mangal/meta"
	"github.com/luevano/mangal/util/afs"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// All of these are mostly just viper wrappers,
// which are used by the Config entries or the fields.

// Get value of the key.
//
// Wrapper of viper.Get.
func Get(key string) any {
	if ok := Exists(key); !ok {
		panic(errorf("Get: key %q is not set", key))
	}
	return viper.Get(key)
}

// Default value for the key.
func Default(key string) any {
	if ok := Exists(key); !ok {
		panic(errorf("Default: key %q is not set", key))
	}
	return fields[key].Default
}

// Description of the key value.
func Description(key string) string {
	if ok := Exists(key); !ok {
		panic(errorf("Description: key %q is not set", key))
	}
	return fields[key].Description
}

// Set a value for the key.
//
// Wrapper of viper.Set.
func Set(key string, value any) error {
	if err := Validate(key, value); err != nil {
		return errorf("Set: %s", err.Error())
	}
	viper.Set(key, value)
	return nil
}

// SetDefault sets a default value for the key.
//
// Wrapper of viper.SetDefault.
func SetDefault(key string, value any) error {
	if err := Validate(key, value); err != nil {
		return errorf("SetDefault: %s", err.Error())
	}
	viper.SetDefault(key, value)
	return nil
}

// BindPFlag binds a pflag to a key.
//
// Wrapper of viper.BindPFlag. Panics on error.
func BindPFlag(key string, flag *pflag.Flag) {
	if err := viper.BindPFlag(key, flag); err != nil {
		panic(errorf("BindPFlag: couldn't bind flag to key %q: %s", err.Error()))
	}
}

// Validate the value for a the registered key.
func Validate(key string, value any) error {
	if ok := Exists(key); !ok {
		return fmt.Errorf("Validate: key %q is not set", key)
	}
	if err := fields[key].Validate(value); err != nil {
		return fmt.Errorf("Validate: couldn't validate value %v for key %q: %s", value, key, err.Error())
	}
	return nil
}

// Exists if there is a registered key.
func Exists(key string) bool {
	_, found := fields[key]
	return found
}

// Keys returns all the registered keys.
func Keys() []string {
	keys := make([]string, len(fields))
	i := 0
	for k := range fields {
		keys[i] = k
		i++
	}
	return keys
}

// Init initializes viper with the desired config (viper) options
func Init() {
	viper.KeyDelimiter(".")
	viper.SetFs(afs.Afero.Fs)
	viper.SetTypeByDefaultValue(true)

	viper.AddConfigPath(xdgConfig())
	viper.AddConfigPath(xdg.Home)
	viper.SetConfigName(meta.AppName)
	viper.SetConfigType("toml")

	// Accept environment variables, all uppercase with "MANGAL_" prefix and "_" for ".",
	// e.g. MANGAL_DOWNLOAD_PATH for download.path
	viper.SetEnvPrefix("mangal") // uppercased automatically
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()
}

// Load the config file and validates all config keys.
//
// If path is empty, it will try to load from xdg.ConfigHome then from xdg.Home.
func Load(path string) error {
	if path != "" {
		viper.SetConfigFile(path)
	}

	// will read config and set all keys that match
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return errorf("Load: unexpected error reading in the config: %s", err.Error())
		}
	}

	// validate all values now that the config was read
	for _, key := range viper.AllKeys() {
		if !Exists(key) {
			fmt.Printf("Config key %q is not supported, update config file\n", key)
			continue
		}

		field := fields[key]
		v := Get(key)
		value, err := field.Unmarshal(v)
		if err != nil {
			return errorf("Load: error unmarshaling value for key %q: %s", key, err.Error())
		}
		if err := Validate(key, value); err != nil {
			return errorf("Load: %s", err.Error())
		}
	}
	return nil
}

// WriteKey sets the key and writes only that key to the existing config file, the rest of the file
// stays as is. Errors if there's no config file, it's never created here.
//
// Unlike Write, runtime values (e.g. from flags) and missing defaults aren't written.
func WriteKey(key string, value any) error {
	if err := Set(key, value); err != nil {
		return err
	}

	file := viper.New()
	file.SetConfigFile(Path)
	if err := file.ReadInConfig(); err != nil {
		return errorf("WriteKey: error reading the config file: %s", err.Error())
	}
	file.Set(key, value)
	if err := file.WriteConfig(); err != nil {
		return errorf("WriteKey: error writing the config file: %s", err.Error())
	}
	return nil
}

// Write current configuration to disk (to the set config directory/file).
//
// Tries to write to the first path available.
func Write() error {
	switch viper.WriteConfig().(type) {
	case nil:
		return nil
	case viper.ConfigFileNotFoundError:
		if err := viper.SafeWriteConfig(); err != nil {
			return errorf("Write: error writing new config file: %s", err.Error())
		}
		return nil
	default:
		return errorf("Write: unexpected error")
	}
}
