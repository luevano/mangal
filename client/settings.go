package client

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// ImportSettings copies the moved keys of the loaded mangal.toml into the database, once
// per data database (docs/settings-db.md "Migration"): a library operation that
// `mangal library undo` reverts (the next start then imports again). The notice says what
// was copied, empty when nothing was.
func ImportSettings(ctx context.Context) (notice string, err error) {
	s, err := cache.Settings()
	if err != nil {
		return "", err
	}
	ops := Ops{Storage: storage.Storage{Data: s.DB}}
	notice, err = importSettings(ctx, ops, s)
	if err != nil {
		return "", err
	}
	split, err := splitSettings(ctx, ops, s)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(notice + " " + split), nil
}

// splitSettings moves the per-provider tables left in the database (jobs.*.providers,
// providers.mirrors) into each provider's own settings, as an operation `mangal library
// undo` reverts (the next start then moves them again). The notice says so, empty when
// there was nothing to move.
func splitSettings(ctx context.Context, ops Ops, s storage.Settings) (string, error) {
	values, _, err := s.All()
	if err != nil {
		return "", err
	}
	set, drop := config.SplitAliases(values)
	if len(drop) == 0 {
		return "", nil
	}
	changes := make(map[string]*string, len(set)+len(drop))
	for key, v := range set {
		changes[key] = new(v)
	}
	for _, key := range drop {
		changes[key] = nil
	}
	summary := fmt.Sprintf("move %s into each provider's settings", strings.Join(drop, ", "))
	if _, err := ops.Run(ctx, "settings", summary, func(library.Journal) error {
		return s.Apply(changes, func(string) bool { return false }, "mangal: each provider's settings")
	}); err != nil {
		return "", fmt.Errorf("move the provider tables: %w", err)
	}
	if err := config.UseStore(s); err != nil {
		return "", err
	}
	return fmt.Sprintf("Moved %s into each provider's settings (providers.<id>.job_check, job_sources, job_urls, job_health, mirror); the old keys still work. \"mangal library undo\" reverts it.",
		strings.Join(drop, ", ")), nil
}

func importSettings(ctx context.Context, ops Ops, s storage.Settings) (string, error) {
	if done, err := s.Imported(); err != nil || done {
		return "", err
	}
	values, skipped := config.MovedValues()
	for _, why := range skipped {
		log.Warn("settings import: left out %s", why)
	}
	// the old per-provider tables go to each provider's settings
	set, drop := config.SplitAliases(values)
	for _, key := range drop {
		delete(values, key)
	}
	maps.Copy(values, set)
	imported := false
	var err error
	if len(values) == 0 {
		// nothing to undo: only marked
		imported, err = s.Import(nil, config.MovedSecret, "mangal.toml")
	} else {
		summary := fmt.Sprintf("copy %s from mangal.toml to the database", stringutil.Quantify(len(values), "setting", "settings"))
		_, err = ops.Run(ctx, "settings", summary, func(library.Journal) error {
			var err error
			imported, err = s.Import(values, config.MovedSecret, "mangal.toml")
			return err
		})
	}
	if err != nil {
		return "", fmt.Errorf("import the settings: %w", err)
	}
	if err := config.UseStore(s); err != nil {
		return "", err
	}
	if !imported || len(values) == 0 {
		return "", nil
	}
	keys := slices.Sorted(maps.Keys(values))
	return fmt.Sprintf("Copied %s from mangal.toml to the database, they're read from there now: %s. \"mangal library undo\" reverts it.",
		stringutil.Quantify(len(keys), "setting", "settings"), strings.Join(keys, ", ")), nil
}

// MovedNotice tells about the moved keys still in mangal.toml (ignored), empty if none.
func MovedNotice() string {
	moved := config.MovedInFile()
	if len(moved) == 0 {
		return ""
	}
	names := make([]string, len(moved))
	for i, m := range moved {
		names[i] = m.Key
		if m.NewKey != m.Key {
			names[i] += " (now " + m.NewKey + ")"
		}
	}
	return fmt.Sprintf("mangal.toml has %s that %s used any more, %s stored in the database now: %s. Change them with \"mangal config set\"; \"mangal config clean --apply\" removes the old entries.",
		stringutil.Quantify(len(moved), "key", "keys"), map[bool]string{true: "isn't", false: "aren't"}[len(moved) == 1],
		map[bool]string{true: "it's", false: "they're"}[len(moved) == 1], strings.Join(names, ", "))
}
