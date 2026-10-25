package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/meta"
	"github.com/luevano/mangal/v5/util/afs"
)

// Every key has an environment variable, MANGAL_ and the key in uppercase with "." and
// "-" as "_" (download.path is MANGAL_DOWNLOAD_PATH), over the stored value and under a
// flag. A secret's also comes from a file, MANGAL_<KEY>_FILE (docs/settings-db.md
// "Precedence and environment variables").

// secretPlainEnv: a secret also takes its plain variable, not only _FILE (David,
// 2026-10-25); _FILE is the recommended, safer way.
const secretPlainEnv = true

// envName is the environment variable of the key.
func envName(key string) string {
	return strings.ToUpper(meta.AppName + "_" + envReplacer.Replace(key))
}

var envReplacer = strings.NewReplacer(".", "_", "-", "_")

// knownEnv are the MANGAL_ variables that aren't keys.
var knownEnv = []string{"MANGAL_CONFIG", "MANGAL_DEBUG", "MANGAL_VERBOSE", "MANGAL_API_TOKEN", "MANGAL_API_TOKEN_FILE"}

// The environment's state, guarded by emu.
var (
	emu sync.Mutex
	// envFiles are the keys set from a _FILE variable, by key: its name.
	envFiles = map[string]string{}
	// envErrs are the _FILE variables that couldn't be used, for Validate.
	envErrs = map[string]error{}
	// clashed are the variables of two keys, used for neither.
	clashed = map[string][]string{}
	// warnedEnv are the provider variables already warned about.
	warnedEnv = map[string]bool{}
)

// EnvVar is the environment variable of the key, set if it has a value from it (which
// wins over the stored value): its _FILE variable for a secret read from one.
func EnvVar(key string) (name string, set bool) {
	name = envName(key)
	emu.Lock()
	file, fromFile := envFiles[key]
	_, clash := clashed[name]
	emu.Unlock()
	if fromFile {
		return file, true
	}
	if clash {
		return name, false
	}
	_, set = os.LookupEnv(name)
	return name, set
}

// readEnvFiles sets the secret keys whose MANGAL_<KEY>_FILE is set to the file's
// content (its trailing newline dropped), for this run.
func readEnvFiles(keys []string) {
	for _, key := range keys {
		f, err := lookup(key)
		if err != nil || !f.secret() {
			continue
		}
		name := envName(key) + "_FILE"
		path, ok := os.LookupEnv(name)
		emu.Lock()
		delete(envFiles, key)
		delete(envErrs, key)
		emu.Unlock()
		if !ok {
			continue
		}
		if _, plain := os.LookupEnv(envName(key)); plain {
			setEnvErr(key, fmt.Errorf("both %s and %s are set, use one", envName(key), name))
			continue
		}
		data, err := afs.Afero.ReadFile(path)
		if err != nil {
			setEnvErr(key, fmt.Errorf("from %s: %w", name, err))
			continue
		}
		value := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
		raw, err := f.toRaw(value)
		if err != nil {
			setEnvErr(key, fmt.Errorf("from %s: %w", name, err))
			continue
		}
		viperSet(key, raw)
		emu.Lock()
		envFiles[key] = name
		emu.Unlock()
	}
}

func setEnvErr(key string, err error) {
	emu.Lock()
	defer emu.Unlock()
	envErrs[key] = err
}

// envError is the problem with the key's variables, nil if none.
func envError(key string) error {
	emu.Lock()
	err := envErrs[key]
	emu.Unlock()
	if err != nil {
		return err
	}
	if f, ferr := lookup(key); !secretPlainEnv && ferr == nil && f.secret() {
		if _, plain := os.LookupEnv(envName(key)); plain {
			return fmt.Errorf("%s: a secret comes from %s_FILE only", envName(key), envName(key))
		}
	}
	return nil
}

// checkClashes finds the keys with the same variable (provider ids are free-form:
// mango-x and mango_x): the variable is used for neither, unset from the environment
// (viper reads every key's variable itself).
func checkClashes() []string {
	byName := map[string][]string{}
	for _, key := range Keys() {
		byName[envName(key)] = append(byName[envName(key)], key)
	}
	var warnings []string
	emu.Lock()
	defer emu.Unlock()
	for name, keys := range byName {
		if len(keys) < 2 || slices.Equal(clashed[name], keys) {
			continue
		}
		clashed[name] = keys
		if _, set := os.LookupEnv(name); set {
			_ = os.Unsetenv(name)
		}
		warnings = append(warnings, fmt.Sprintf("%s is the variable of %s, it's used for neither", name, strings.Join(keys, " and ")))
	}
	slices.Sort(warnings)
	return warnings
}

// UnknownEnv are the MANGAL_ variables that set no key, each with the closest key's
// variable as the suggestion. The providers' (MANGAL_PROVIDERS_...) are only known once
// they load, those are warned about then.
func UnknownEnv() []UnknownKey {
	names := map[string]bool{}
	for _, key := range Keys() {
		names[envName(key)] = true
		if IsSecret(key) {
			names[envName(key)+"_FILE"] = true
		}
	}
	var unknown []UnknownKey
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(name, envName("")) || names[name] || slices.Contains(knownEnv, name) || strings.HasPrefix(name, providerEnvPrefix) {
			continue
		}
		unknown = append(unknown, UnknownKey{Key: name, Suggestion: suggestEnv(name, names)})
	}
	slices.SortFunc(unknown, func(a, b UnknownKey) int { return strings.Compare(a.Key, b.Key) })
	return unknown
}

// suggestEnv is the variable closest to name, "" when none is close enough.
func suggestEnv(name string, names map[string]bool) string {
	best, distance := "", -1
	for n := range names {
		if d := fuzzy.LevenshteinDistance(name, n); distance < 0 || d < distance || d == distance && n < best {
			best, distance = n, d
		}
	}
	if distance >= 0 && distance <= max(2, len(name)/5) {
		return best
	}
	return ""
}

// checkProviderEnv reads the providers' secrets from their _FILE variables and warns
// (once) about clashing names and MANGAL_PROVIDERS_ variables that set nothing.
func checkProviderEnv(keys []string) {
	readEnvFiles(keys)
	for _, w := range checkClashes() {
		log.Warn("config: %s", w)
	}
	names := map[string]bool{}
	for _, key := range Keys() {
		names[envName(key)] = true
		names[envName(key)+"_FILE"] = true
	}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(name, providerEnvPrefix) || names[name] {
			continue
		}
		emu.Lock()
		warned := warnedEnv[name]
		warnedEnv[name] = true
		emu.Unlock()
		if warned {
			continue
		}
		if s := suggestEnv(name, names); s != "" {
			log.Warn("config: %s sets no key (did you mean %s?)", name, s)
		} else {
			log.Warn("config: %s sets no key (no installed provider has that setting)", name)
		}
	}
}

// EnvSecret is the value of the variable name or of the file name_FILE points to (its
// trailing newline dropped), for secrets that aren't keys (MANGAL_API_TOKEN). Both set is
// an error.
func EnvSecret(name string) (string, error) {
	value, plain := os.LookupEnv(name)
	path, file := os.LookupEnv(name + "_FILE")
	switch {
	case plain && file:
		return "", fmt.Errorf("both %s and %s_FILE are set, use one", name, name)
	case file:
		data, err := afs.Afero.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("from %s_FILE: %w", name, err)
		}
		return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"), nil
	}
	return value, nil
}

// errEnv joins the problems with the keys' variables.
func errEnv(keys []string) error {
	var errs []error
	for _, key := range keys {
		if err := envError(key); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
	}
	return errors.Join(errs...)
}
