package config

import (
	"maps"

	"github.com/spf13/viper"
)

// SetMirror writes the provider's preferred mirror to the config file, an empty mirror
// removes it. Like WriteKey, the rest of the file stays as is.
func SetMirror(providerID, mirror string) error {
	mirrors := maps.Clone(Providers.Mirrors.Get())
	if mirror == "" {
		delete(mirrors, providerID)
	} else {
		mirrors[providerID] = mirror
	}
	raw, err := Providers.Mirrors.Marshal(mirrors)
	if err != nil {
		return err
	}
	if err := Set(Providers.Mirrors.Key, raw); err != nil {
		return err
	}

	// WriteKey can't remove an entry, viper merges the table with the file's
	file := viper.New()
	file.SetConfigFile(Path)
	if err := file.ReadInConfig(); err != nil {
		return errorf("SetMirror: error reading the config file: %s", err.Error())
	}
	settings := file.AllSettings()
	providers, _ := settings["providers"].(map[string]any)
	if providers == nil {
		providers = map[string]any{}
	}
	providers["mirrors"] = raw
	settings["providers"] = providers
	out := viper.New()
	out.SetConfigFile(Path)
	if err := out.MergeConfigMap(settings); err != nil {
		return err
	}
	if err := out.WriteConfig(); err != nil {
		return errorf("SetMirror: error writing the config file: %s", err.Error())
	}
	return nil
}
