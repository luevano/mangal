package core

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/manager"
	"github.com/luevano/mangal/v5/util/cache"
	stringutil "github.com/luevano/mangal/v5/util/string"
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
	// History is the changes of the database keys (key "" for all), newest first, at most
	// limit (0 for all). The keys in mangal.toml have none.
	History(ctx context.Context, key string, limit int) ([]ConfigChange, error)
	// Undo sets the change's key back to its old value, recorded as a change of its own;
	// refused when the key changed after it, or it's a secret's.
	Undo(ctx context.Context, id int64) (ConfigEntry, error)
	// Export is the database keys as TOML (secrets left out), for ImportPlan.
	Export(ctx context.Context) ([]byte, error)
	// ImportPlan is what importing the TOML would change, nothing changes.
	ImportPlan(ctx context.Context, data []byte) ([]config.ImportChange, error)
	// Import applies the plan as one library operation (`mangal library undo` reverts it).
	Import(ctx context.Context, changes []config.ImportChange) (opID int64, err error)
}

// ConfigChange is a change of a database key: Old and New as stored (JSON), nil when it
// was unset (the default), masked for a secret.
type ConfigChange struct {
	ID  int64     `json:"id"`
	Key string    `json:"key"`
	Old *string   `json:"old"`
	New *string   `json:"new"`
	Who string    `json:"who"`
	At  time.Time `json:"at"`
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
	// Applies is when a change applies in a running mangal: now, reload (the providers
	// load again) or restart.
	Applies string `json:"applies"`
	// Restart is true when the stored value changed since this process started, and it
	// uses the old one until it restarts.
	Restart bool `json:"restart,omitempty"`
	// Risky keys weaken access when changed: only an admin sees them over HTTP.
	Risky bool `json:"risky,omitempty"`
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
		Stored: string(config.StoredIn(key)), Source: config.SourceOf(key), Provider: config.ProviderOf(key),
		Applies: config.Applies(key), Restart: slices.Contains(config.RestartNeeded(), key), Risky: config.IsRisky(key)}
	if name, set := config.EnvVar(key); set {
		e.Env = name
	}
	return e, nil
}

func (localConfig) List(context.Context) ([]ConfigEntry, error) {
	LoadProviderSettings()
	keys := config.ListedKeys()
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

func (localConfig) History(_ context.Context, key string, limit int) ([]ConfigChange, error) {
	s, err := cache.Settings()
	if err != nil {
		return nil, err
	}
	changes, err := s.History(key, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ConfigChange, len(changes))
	for i, c := range changes {
		out[i] = ConfigChange{ID: c.ID, Key: c.Key, Old: c.Old, New: c.New, Who: c.Who, At: c.At}
	}
	return out, nil
}

func (localConfig) Undo(ctx context.Context, id int64) (ConfigEntry, error) {
	LoadProviderSettings()
	s, err := cache.Settings()
	if err != nil {
		return ConfigEntry{}, err
	}
	change, ok, err := s.Change(id)
	if err != nil {
		return ConfigEntry{}, err
	}
	if !ok {
		return ConfigEntry{}, &NotFoundError{What: fmt.Sprintf("no settings change %d", id)}
	}
	if !config.Exists(change.Key) {
		return ConfigEntry{}, &NotFoundError{What: fmt.Sprintf("no config key %q (its provider isn't installed?)", change.Key)}
	}
	if config.IsSecret(change.Key) {
		return ConfigEntry{}, invalid("%s is a secret, its history doesn't keep the value: set it again", change.Key)
	}
	current, err := config.FileValue(change.Key)
	if err != nil {
		return ConfigEntry{}, err
	}
	now, err := config.StoredText(change.Key, current)
	if err != nil {
		return ConfigEntry{}, err
	}
	if !sameText(now, change.New) {
		return ConfigEntry{}, invalid("%s changed after change %d, undo the later changes first", change.Key, id)
	}
	var old any
	if change.Old == nil {
		old, _ = config.Default(change.Key)
	} else if err := json.Unmarshal([]byte(*change.Old), &old); err != nil {
		return ConfigEntry{}, fmt.Errorf("settings change %d: %w", id, err)
	}
	if err := config.WriteKeyBy(change.Key, old, fmt.Sprintf("%s (undo of change %d)", whoOf(ctx), id)); err != nil {
		return ConfigEntry{}, err
	}
	return configEntry(change.Key)
}

// sameText compares two stored values, JSON compacted.
func sameText(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	var va, vb any
	if json.Unmarshal([]byte(*a), &va) != nil || json.Unmarshal([]byte(*b), &vb) != nil {
		return *a == *b
	}
	ja, _ := json.Marshal(va)
	jb, _ := json.Marshal(vb)
	return string(ja) == string(jb)
}

func (localConfig) Export(context.Context) ([]byte, error) {
	LoadProviderSettings()
	return config.Export()
}

func (localConfig) ImportPlan(_ context.Context, data []byte) ([]config.ImportChange, error) {
	LoadProviderSettings()
	changes, err := config.ImportPlan(data)
	if err != nil {
		return nil, invalid("%s", err)
	}
	return changes, nil
}

func (localConfig) Import(ctx context.Context, changes []config.ImportChange) (int64, error) {
	if len(changes) == 0 {
		return 0, nil
	}
	s, err := cache.Settings()
	if err != nil {
		return 0, err
	}
	ops, err := client.DefaultOps()
	if err != nil {
		return 0, err
	}
	values := make(map[string]*string, len(changes))
	for _, c := range changes {
		if config.IsAlias(c.Key) {
			// an old per-provider table: each provider's settings
			rows, err := config.AliasRows(c.Key, c.New)
			if err != nil {
				return 0, invalid("%s", err)
			}
			maps.Copy(values, rows)
			continue
		}
		text, err := config.StoredText(c.Key, c.New)
		if err != nil {
			return 0, invalid("%s", err)
		}
		values[c.Key] = text
	}
	keys := slices.Sorted(maps.Keys(values))
	summary := fmt.Sprintf("import %s: %s", stringutil.Quantify(len(keys), "setting", "settings"), strings.Join(keys, ", "))
	info, err := ops.Run(ctx, "settings", summary, func(library.Journal) error {
		return s.Apply(values, config.IsSecret, whoOf(ctx)+" (import)")
	})
	if err != nil {
		return info.ID, fmt.Errorf("import the settings: %w", err)
	}
	config.Refresh()
	return info.ID, nil
}
