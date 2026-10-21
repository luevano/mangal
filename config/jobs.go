package config

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/luevano/mangal/v5/util/afs"
	"github.com/luevano/mangal/v5/util/cron"
	"github.com/spf13/viper"
)

// A provider's value in a job kind's jobs.<kind>.providers: off, on (the kind's own
// schedule), or its own interval (a cron schedule for the watch checks).
const (
	ProviderOn  = "on"
	ProviderOff = "off"
)

// ProviderSettings are a job kind's per-provider settings by provider id, the prebuilt
// defaults included.
type ProviderSettings map[string]string

// On is false when the provider is off for the kind.
func (p ProviderSettings) On(provider string) bool { return p[provider] != ProviderOff }

// Override is the provider's own interval (schedule), empty for the kind's.
func (p ProviderSettings) Override(provider string) string {
	if v := p[provider]; v != ProviderOn && v != ProviderOff {
		return v
	}
	return ""
}

func validateDuration(s string) error {
	d, err := time.ParseDuration(s)
	if err == nil && d <= 0 {
		err = fmt.Errorf("%q isn't a positive duration", s)
	}
	return err
}

func validateCron(s string) error {
	_, err := cron.Parse(s)
	return err
}

// providersEntry is a table of provider id = on, off or an interval checked by valid; a
// table in the file replaces the defaults, a provider it doesn't name is on.
func providersEntry(key string, defaults ProviderSettings, valid func(string) error, description string) *entry[map[string]any, ProviderSettings] {
	return reg(entry[map[string]any, ProviderSettings]{
		Key:         key,
		Default:     defaults,
		Description: description,
		Unmarshal: func(raw map[string]any) (ProviderSettings, error) {
			out := make(ProviderSettings, len(raw))
			for id, v := range raw {
				s, ok := v.(string)
				if !ok {
					return nil, fmt.Errorf("%q: expected a string, got %T", id, v)
				}
				s = strings.TrimSpace(s)
				if l := strings.ToLower(s); l == ProviderOn || l == ProviderOff {
					s = l
				}
				out[id] = s
			}
			return out, nil
		},
		Marshal: func(p ProviderSettings) (map[string]any, error) {
			raw := make(map[string]any, len(p))
			for id, s := range p {
				raw[id] = s
			}
			return raw, nil
		},
		Validate: func(p ProviderSettings) error {
			for id, s := range p {
				if s == ProviderOn || s == ProviderOff {
					continue
				}
				if err := valid(s); err != nil {
					return fmt.Errorf("%q: %q isn't on, off or an interval: %w", id, s, err)
				}
			}
			return nil
		},
	})
}

// Fresh is Get with the config file read again when it sets the key (and the environment
// doesn't): a long-running mangal (serve) sees what another process wrote.
func (e *entry[Raw, Value]) Fresh() Value {
	if _, env := EnvVar(e.Key); env {
		return e.Get()
	}
	file := viper.New()
	file.SetFs(afs.Afero.Fs)
	file.SetConfigFile(Path)
	if err := file.ReadInConfig(); err != nil || !file.IsSet(e.Key) {
		return e.Get()
	}
	value, err := e.decode(file.Get(e.Key))
	if err != nil {
		return e.Get()
	}
	return value
}

// mangaPlusOff is MangaPlus off: the maintenance jobs don't ask it unless turned on
// (a paid device secret risks a lockout).
var mangaPlusOff = ProviderSettings{"mango-mangaplus": ProviderOff}

// MaintenanceDefaults are the prebuilt per-provider settings of the maintenance jobs
// (source checks, source addresses, network health).
func MaintenanceDefaults() ProviderSettings { return maps.Clone(mangaPlusOff) }

var Jobs = configJobs{
	Check: configJobsCheck{
		Enabled: boolEntry("jobs.check.enabled", true,
			"Whether `mangal serve` checks the watches on their schedule (watch.schedule, a watch's own). Off, only manual checks run."),
		Providers: providersEntry("jobs.check.providers", ProviderSettings{}, validateCron,
			"Watch checks per provider id: off (the scheduler skips its watches), on, or a cron schedule of its own for its watches without one, e.g. mango-mangadex = \"0 */8 * * *\"."),
	},
	Sources: configJobsSources{
		Enabled: boolEntry("jobs.sources.enabled", true,
			"Whether `mangal serve` checks that the library sources are still on their providers (health L11) every health.source_check_interval."),
		Providers: providersEntry("jobs.sources.providers", mangaPlusOff, validateDuration,
			"Source checks per provider id: off (never asked, `mangal health --network` included), on, or its own interval (a Go duration, e.g. mango-mangadex = \"336h\"). MangaPlus is off unless set on."),
	},
	URLs: configJobsURLs{
		Enabled: boolEntry("jobs.urls.enabled", true,
			"Whether `mangal serve` looks up the web pages of the library sources without one (as `mangal library urls`) every jobs.urls.interval."),
		Interval: durationEntry("jobs.urls.interval", "168h",
			"How often `mangal serve` looks up the sources without a web page (only those, each at most once per interval), 168h a week. Read on each run.", false),
		Providers: providersEntry("jobs.urls.providers", mangaPlusOff, validateDuration,
			"Source address lookups per provider id: off (never asked, `mangal library urls` included), on, or its own interval (a Go duration). MangaPlus is off unless set on."),
	},
	Health: configJobsHealth{
		Providers: providersEntry("jobs.health.providers", mangaPlusOff, validateDuration,
			"Network health checks (A2, a probe of each provider) per provider id: off (not asked) or on. MangaPlus is off unless set on."),
	},
	DelayMin: durationEntry("jobs.delay_min", "1s",
		"Shortest pause between two requests to the same provider in the maintenance jobs (source checks, source addresses): a random one between this and jobs.delay_max, so the site doesn't see a beat.", false),
	DelayMax: durationEntry("jobs.delay_max", "3s",
		"Longest pause between two requests to the same provider in the maintenance jobs, see jobs.delay_min.", false),
}

// JobDelay is a random pause between jobs.delay_min and jobs.delay_max (read again), the
// one between two requests to a provider in the maintenance jobs.
func JobDelay() time.Duration {
	lo, err := time.ParseDuration(Jobs.DelayMin.Fresh())
	if err != nil {
		lo = time.Second
	}
	hi, err := time.ParseDuration(Jobs.DelayMax.Fresh())
	if err != nil || hi < lo {
		hi = lo
	}
	return lo + rand.N(hi-lo+1)
}
