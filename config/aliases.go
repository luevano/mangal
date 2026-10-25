package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/log"
)

// aliasTables are the provider tables that became each provider's own setting,
// providers.<id>.<setting> (docs/settings-db.md "Per-provider jobs and mirror"). The
// table keys stay as aliases: their value is made of the providers' settings (a table row
// left in the database, then the settings over it) and a write goes to the settings.
var aliasTables = map[string]string{
	"jobs.check.providers":   "job_check",
	"jobs.sources.providers": "job_sources",
	"jobs.urls.providers":    "job_urls",
	"jobs.health.providers":  "job_health",
	"providers.mirrors":      "mirror",
}

// IsAlias is true for a table key that's an alias of the providers' own settings: not
// listed or exported, the settings are.
func IsAlias(key string) bool {
	_, ok := aliasTables[key]
	return ok
}

// ListedKeys are Keys without the aliases: what the settings screens list.
func ListedKeys() []string {
	return slices.DeleteFunc(Keys(), IsAlias)
}

// aliasProvider is the provider id of a provider's setting key of the alias with setting.
func aliasProvider(key, setting string) (string, bool) {
	rest, ok := strings.CutPrefix(key, "providers.")
	if !ok {
		return "", false
	}
	id, ok := strings.CutSuffix(rest, "."+setting)
	return id, ok && id != "" && !strings.Contains(id, ".")
}

// aliasRawLocked is the alias table's value from the stored values (lmu held) and the
// providers' variables: false when none sets it.
func aliasRawLocked(key, setting string) (any, bool) {
	m := map[string]any{}
	set := false
	if text, ok := stored[key]; ok {
		var v map[string]any
		if json.Unmarshal([]byte(text), &v) == nil {
			maps.Copy(m, v)
			set = true
		}
	}
	for _, k := range slices.Sorted(maps.Keys(stored)) {
		id, ok := aliasProvider(k, setting)
		if !ok {
			continue
		}
		var v any
		if json.Unmarshal([]byte(stored[k]), &v) == nil {
			m[id], set = v, true
		}
	}
	// a provider's variable is its value, as for its setting
	for _, k := range providerKeys() {
		if id, ok := aliasProvider(k, setting); ok {
			if name, env := EnvVar(k); env {
				m[id], set = os.Getenv(name), true
			}
		}
	}
	if !set {
		return nil, false
	}
	f, err := lookup(key)
	if err != nil {
		return nil, false
	}
	raw, err := f.toRaw(m)
	if err != nil {
		text, _ := json.Marshal(m)
		if badStored[key] != string(text) {
			badStored[key] = string(text)
			log.Warn("config: %s in the database is invalid, its default is used: %s", key, err)
		}
		return nil, false
	}
	return raw, true
}

// AliasRows are the stored rows (JSON, nil unsets) that give the alias table key the
// value: the providers it names set, the others unset (their default), the old table
// row dropped.
func AliasRows(key string, value any) (map[string]*string, error) {
	raw, err := Parse(key, value)
	if err != nil {
		return nil, err
	}
	setting := aliasTables[key]
	next, _ := raw.(map[string]any)
	lmu.Lock()
	old, _ := aliasRawLocked(key, setting)
	_, row := stored[key]
	lmu.Unlock()
	prev, _ := old.(map[string]any)
	rows := map[string]*string{}
	if row {
		rows[key] = nil
	}
	for id := range prev {
		if _, ok := next[id]; !ok {
			rows[providerKey(id, setting)] = nil
		}
	}
	for id, v := range next {
		if p, ok := prev[id]; ok && fmt.Sprint(p) == fmt.Sprint(v) && !row {
			continue
		}
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("config key %q: %w", key, err)
		}
		rows[providerKey(id, setting)] = new(string(data))
	}
	return rows, nil
}

// writeAlias writes the alias table as its providers' settings (AliasRows), the table
// row last.
func writeAlias(s Store, key string, value any, who string) error {
	rows, err := AliasRows(key, value)
	if err != nil {
		return err
	}
	for _, k := range slices.Sorted(maps.Keys(rows)) {
		if k == key {
			continue
		}
		if err := s.Set(k, rows[k], false, who); err != nil {
			return fmt.Errorf("write config key %q: %w", key, err)
		}
	}
	if _, ok := rows[key]; ok {
		if err := s.Set(key, nil, false, who); err != nil {
			return fmt.Errorf("write config key %q: %w", key, err)
		}
	}
	refreshStore()
	return nil
}

// splitRow writes the alias table row the provider's setting key belongs to, if one is
// left, as its providers' settings, before the key itself is written: its other
// providers keep their values, the row goes.
func splitRow(s Store, key, who string) error {
	pmu.RLock()
	f, ok := providerFields[key]
	pmu.RUnlock()
	if !ok || !f.mangal {
		return nil
	}
	for table, setting := range aliasTables {
		if setting != f.setting.Key {
			continue
		}
		lmu.Lock()
		set, drop := SplitAliases(map[string]string{table: stored[table]})
		_, row := stored[table]
		for k := range set {
			if _, have := stored[k]; have {
				delete(set, k)
			}
		}
		lmu.Unlock()
		if !row {
			return nil
		}
		for _, k := range slices.Sorted(maps.Keys(set)) {
			if err := s.Set(k, new(set[k]), false, who); err != nil {
				return fmt.Errorf("write config key %q: %w", k, err)
			}
		}
		for _, k := range drop {
			if err := s.Set(k, nil, false, who); err != nil {
				return fmt.Errorf("write config key %q: %w", k, err)
			}
		}
	}
	return nil
}

// SplitAliases are the alias table rows of values (stored JSON by key) as the providers'
// settings: set is the settings to add (a provider's setting already in values wins),
// drop the table rows to remove.
func SplitAliases(values map[string]string) (set map[string]string, drop []string) {
	set = map[string]string{}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		setting, ok := aliasTables[key]
		if !ok {
			continue
		}
		drop = append(drop, key)
		var m map[string]any
		if err := json.Unmarshal([]byte(values[key]), &m); err != nil {
			log.Warn("settings: %s in the database isn't a table, dropped: %s", key, err)
			continue
		}
		for id, v := range m {
			k := providerKey(id, setting)
			if _, have := values[k]; have {
				continue
			}
			if data, err := json.Marshal(v); err == nil {
				set[k] = string(data)
			}
		}
	}
	return set, drop
}

// mangalSettings are the providers' settings mangal declares for each provider: its
// part in each maintenance job, and its mirror when it has some. A maintenance job's
// default is off for a provider that avoids unattended requests (MangaPlus: a lockout
// risk), on for the others.
func mangalSettings(info libmangal.ProviderInfo) []*providerField {
	unattended := ProviderOn
	if info.Traits.AvoidUnattended {
		unattended = ProviderOff
	}
	avoid := ""
	if info.Traits.AvoidUnattended {
		avoid = " Off by default: the provider avoids unattended requests (a lockout risk)."
	}
	field := func(key, name, def, description string, valid func(string) error) *providerField {
		return &providerField{provider: info.ID, mangal: true, check: jobValue(valid), setting: libmangal.ProviderSetting{
			Key: key, Name: name, Description: description, Kind: libmangal.SettingString, Default: def,
		}}
	}
	fields := []*providerField{
		field("job_check", "Watch checks", ProviderOn,
			`Watch checks of this provider's watches: on, off (the scheduler skips them) or a cron schedule of their own for the watches without one, e.g. "0 */8 * * *" (jobs.check.providers).`, validateCron),
		field("job_sources", "Source checks", unattended,
			`Source checks (health L11) of this provider: on, off (never asked, mangal health --network included) or an interval of its own (a Go duration, e.g. 336h; jobs.sources.providers).`+avoid, validateDuration),
		field("job_urls", "Source addresses", unattended,
			`Source address lookups of this provider: on, off (never asked, mangal library urls included) or an interval of its own (a Go duration; jobs.urls.providers).`+avoid, validateDuration),
		field("job_health", "Network health", unattended,
			`Network health check (A2, a probe) of this provider: on or off (jobs.health.providers).`+avoid, nil),
	}
	if len(info.Mirrors) != 0 {
		fields = append(fields, &providerField{provider: info.ID, mangal: true, setting: libmangal.ProviderSetting{
			Key: "mirror", Name: "Website", Kind: libmangal.SettingEnum, Default: info.Website,
			Values:      append([]string{info.Website}, info.Mirrors...),
			Description: "The website this provider uses: its own or one of its mirrors (providers.mirrors).",
		}})
	}
	return fields
}

// jobValue checks a job value: on, off, or (with valid) an interval.
func jobValue(valid func(string) error) func(string) error {
	return func(s string) error {
		if l := strings.ToLower(s); l == ProviderOn || l == ProviderOff {
			return nil
		}
		if valid == nil {
			return fmt.Errorf("%q isn't on or off", s)
		}
		if err := valid(s); err != nil {
			return fmt.Errorf("%q isn't on, off or an interval: %w", s, err)
		}
		return nil
	}
}
