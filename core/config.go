package core

import (
	"context"
	"fmt"
	"sync"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/manager"
)

// Config is mangal's config, the providers' settings included: the secrets are masked,
// setting writes the key where it's stored: the config file (created when there's none,
// the rest of it left as it is) or the data database.
type Config interface {
	List(ctx context.Context) ([]ConfigEntry, error)
	// Get is the key's entry, a *NotFoundError for an unknown key.
	Get(ctx context.Context, key string) (ConfigEntry, error)
	// Set validates the value (as in the file, typed, or a string) and writes it.
	Set(ctx context.Context, key string, value any) (ConfigEntry, error)
}

// ConfigEntry is a config key.
type ConfigEntry struct {
	Key string `json:"key"`
	// Value and Default are as in the file, masked for a secret.
	Value       any    `json:"value"`
	Default     any    `json:"default"`
	Description string `json:"description"`
	// Kind is the value's type: string, bool, int or map.
	Kind   string `json:"kind"`
	Secret bool   `json:"secret,omitempty"`
	// Values are an enum's accepted values.
	Values []string `json:"values,omitempty"`
	// Env is the environment variable that sets it for this run, "" when it isn't set.
	Env string `json:"env,omitempty"`
	// Stored is where the value is kept: "file" (mangal.toml) or "db" (the data database).
	Stored string `json:"stored"`
	// Source is where the value comes from in this process: a flag, the environment, the
	// stored value or the default.
	Source config.Source `json:"source"`
	// Provider is the provider of a provider's setting (providers.<id>.<key>).
	Provider string `json:"provider,omitempty"`
}

type whoKey struct{}

// WithWho says who changes the config (in the settings' history), e.g. the API's user.
func WithWho(ctx context.Context, who string) context.Context {
	return context.WithValue(ctx, whoKey{}, who)
}

func whoOf(ctx context.Context) string {
	if who, ok := ctx.Value(whoKey{}).(string); ok {
		return who
	}
	return config.Who
}

var providersOnce sync.Once

// LoadProviderSettings registers the installed providers' settings as keys, once, when
// nothing loaded the providers yet.
func LoadProviderSettings() {
	providersOnce.Do(func() {
		if config.ProvidersRegistered() {
			return
		}
		if _, err := manager.Loaders(); err != nil {
			log.Warn("load the providers' settings: %s", err)
		}
	})
}

// NewConfig is mangal's config, without the rest of the services (no storage).
func NewConfig() Config { return localConfig{} }

type localConfig struct{}

var configKinds = map[config.Kind]string{config.KindString: "string", config.KindBool: "bool", config.KindInt: "int", config.KindMap: "map"}

func configEntry(key string) (ConfigEntry, error) {
	if !config.Exists(key) {
		return ConfigEntry{}, &NotFoundError{What: fmt.Sprintf("no config key %q", key)}
	}
	value, err := config.Get(key)
	if err != nil {
		return ConfigEntry{}, err
	}
	def, _ := config.Default(key)
	description, _ := config.Description(key)
	e := ConfigEntry{Key: key, Value: config.Masked(key, value), Default: config.Masked(key, def), Description: description,
		Kind: configKinds[config.KindOf(key)], Secret: config.IsSecret(key), Values: config.Values(key),
		Stored: string(config.StoredIn(key)), Source: config.SourceOf(key), Provider: config.ProviderOf(key)}
	if name, set := config.EnvVar(key); set {
		e.Env = name
	}
	return e, nil
}

func (localConfig) List(context.Context) ([]ConfigEntry, error) {
	LoadProviderSettings()
	keys := config.Keys()
	entries := make([]ConfigEntry, 0, len(keys))
	for _, key := range keys {
		e, err := configEntry(key)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (localConfig) Get(_ context.Context, key string) (ConfigEntry, error) {
	LoadProviderSettings()
	return configEntry(key)
}

func (localConfig) Set(ctx context.Context, key string, value any) (ConfigEntry, error) {
	LoadProviderSettings()
	if !config.Exists(key) {
		return ConfigEntry{}, &NotFoundError{What: fmt.Sprintf("no config key %q", key)}
	}
	raw, err := config.Parse(key, value)
	if err != nil {
		return ConfigEntry{}, invalid("%s", err)
	}
	if config.StoredIn(key) == config.StoredFile {
		if err := config.CreateFile(); err != nil {
			return ConfigEntry{}, err
		}
	}
	if err := config.WriteKeyBy(key, raw, whoOf(ctx)); err != nil {
		return ConfigEntry{}, err
	}
	return configEntry(key)
}
