package config

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/log"
	"github.com/spf13/cast"
	"github.com/spf13/viper"
)

// The providers' settings (libmangal.ProviderInfo.Settings) are keys too,
// providers.<id>.<key>, stored in the database. They're registered when the providers
// load, so pmu guards them.
var (
	pmu                 sync.RWMutex
	providerFields      = map[string]*providerField{}
	providersRegistered bool
)

// ProvidersRegistered is true once RegisterProviders ran.
func ProvidersRegistered() bool {
	pmu.RLock()
	defer pmu.RUnlock()
	return providersRegistered
}

// providerField is a provider's setting as a key. Its raw value is a bool, an int64 or
// text, by kind.
type providerField struct {
	provider string
	setting  libmangal.ProviderSetting
	// mangal declares it (aliases.go), check validates its value further
	mangal bool
	check  func(string) error
}

func (p *providerField) description() string {
	if p.setting.Warning != "" {
		return p.setting.Description + " Warning: " + p.setting.Warning
	}
	return p.setting.Description
}

func (p *providerField) secret() bool { return p.setting.Kind == libmangal.SettingSecret }
func (p *providerField) isMap() bool  { return false }

func (p *providerField) kind() Kind {
	switch p.setting.Kind {
	case libmangal.SettingBool:
		return KindBool
	case libmangal.SettingInt:
		return KindInt
	}
	return KindString
}

func (p *providerField) values() []string {
	if p.setting.Kind == libmangal.SettingEnum {
		return p.setting.Values
	}
	return nil
}

func (p *providerField) defaultRaw() any {
	raw, _ := p.typed(p.setting.Default)
	return raw
}

func (p *providerField) toRaw(a any) (any, error) {
	text, err := cast.ToStringE(a)
	if err != nil {
		return nil, fmt.Errorf("%v (%T) isn't text", a, a)
	}
	if err := p.setting.Check(text); err != nil {
		return nil, err
	}
	if p.check != nil {
		if err := p.check(text); err != nil {
			return nil, err
		}
		if l := strings.ToLower(text); l == ProviderOn || l == ProviderOff {
			text = l
		}
	}
	return p.typed(text)
}

func (p *providerField) typed(text string) (any, error) {
	switch p.setting.Kind {
	case libmangal.SettingBool:
		return strconv.ParseBool(text)
	case libmangal.SettingInt:
		return strconv.ParseInt(text, 10, 64)
	}
	return text, nil
}

func providerKey(id, key string) string { return "providers." + id + "." + key }

func isProviderKey(key string) bool {
	pmu.RLock()
	defer pmu.RUnlock()
	_, ok := providerFields[key]
	return ok
}

func providerKeys() []string {
	pmu.RLock()
	defer pmu.RUnlock()
	return slices.Collect(maps.Keys(providerFields))
}

// RegisterProviders makes the providers' settings keys (providers.<id>.<key>), replacing
// the ones registered before. Their values in the database stay when a provider goes.
func RegisterProviders(infos []libmangal.ProviderInfo) {
	registered := map[string]*providerField{}
	for _, info := range infos {
		for _, s := range info.Settings {
			key := providerKey(info.ID, s.Key)
			if _, static := fields[key]; static {
				log.Warn("config: provider %s's setting %q is left out, it's mangal's key %s", info.ID, s.Key, key)
				continue
			}
			registered[key] = &providerField{provider: info.ID, setting: s}
		}
		for _, f := range mangalSettings(info) {
			key := providerKey(info.ID, f.setting.Key)
			if _, declared := registered[key]; declared {
				log.Warn("config: provider %s's setting %q is left out, it's mangal's %s", info.ID, f.setting.Key, key)
			}
			registered[key] = f
		}
	}
	pmu.Lock()
	providerFields, providersRegistered = registered, true
	pmu.Unlock()
	vmu.Lock()
	for key, f := range registered {
		viper.SetDefault(key, f.defaultRaw())
	}
	vmu.Unlock()
	lmu.Lock()
	applyLocked()
	lmu.Unlock()
	checkProviderEnv(slices.Collect(maps.Keys(registered)))
}

// ProviderValues are the values of the provider's settings for its loader
// (libmangal.SettingsFunc): every registered one, from a flag, the environment, the
// database or its default; a path's ~ expanded.
func ProviderValues(id string) map[string]string {
	pmu.RLock()
	var keys []*providerField
	for _, f := range providerFields {
		if f.provider == id {
			keys = append(keys, f)
		}
	}
	pmu.RUnlock()
	values := make(map[string]string, len(keys))
	for _, f := range keys {
		key := providerKey(id, f.setting.Key)
		raw, err := f.toRaw(viperGet(key))
		if err != nil {
			// Validate reports it
			raw = f.defaultRaw()
		}
		text := fmt.Sprint(raw)
		if f.setting.Kind == libmangal.SettingPath {
			if expanded, err := expandPath(text); err == nil {
				text = expanded
			}
		}
		values[f.setting.Key] = text
	}
	return values
}

// ProviderOf is the provider id of a provider's setting key, "" for mangal's keys.
func ProviderOf(key string) string {
	pmu.RLock()
	defer pmu.RUnlock()
	if f, ok := providerFields[key]; ok {
		return f.provider
	}
	return ""
}

// providerEnvPrefix are the variables that may be providers' settings, unknown until
// the providers load.
var providerEnvPrefix = strings.ToUpper(envName("providers") + "_")
