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
	return importSettings(ctx, Ops{Storage: storage.Storage{Data: s.DB}}, s)
}

func importSettings(ctx context.Context, ops Ops, s storage.Settings) (string, error) {
	if done, err := s.Imported(); err != nil || done {
		return "", err
	}
	values, skipped := config.MovedValues()
	for _, why := range skipped {
		log.Warn("settings import: left out %s", why)
	}
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
	log.Info("copied the settings %s from mangal.toml to the database", strings.Join(keys, ", "))
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
