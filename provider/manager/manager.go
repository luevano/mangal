package manager

import (
	"fmt"
	"slices"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/provider/loader"
)

// Load returns the installed providers. A Lua provider that can't be loaded is skipped,
// its error is in problems, for the callers to show.
func Load() (loaders []libmangal.ProviderLoader, problems []error, err error) {
	mangoLoaders, err := loader.MangoLoaders()
	if err != nil {
		return nil, nil, err
	}
	loaders = append(loaders, mangoLoaders...)

	luaLoaders, problems, err := loader.LuaLoaders()
	if err != nil {
		return nil, nil, err
	}
	loaders = append(loaders, luaLoaders...)

	return dedupe(loaders, problems)
}

// dedupe drops a loader whose id, or an old id of its history, an earlier one has
// already: which one a lookup finds would be up to the order.
func dedupe(all []libmangal.ProviderLoader, problems []error) ([]libmangal.ProviderLoader, []error, error) {
	seen := map[string]bool{}
	var loaders []libmangal.ProviderLoader
	for _, l := range all {
		info := l.Info()
		ids := []string{info.ID}
		for _, alias := range info.History {
			ids = append(ids, alias.ID)
		}
		if i := slices.IndexFunc(ids, func(id string) bool { return seen[id] }); i >= 0 {
			problems = append(problems, fmt.Errorf("%s: duplicate id %q", l, ids[i]))
			continue
		}
		for _, id := range ids {
			seen[id] = true
		}
		loaders = append(loaders, l)
	}
	return loaders, problems, nil
}

// Loaders is Load with the problems only logged.
func Loaders() ([]libmangal.ProviderLoader, error) {
	loaders, problems, err := Load()
	for _, p := range problems {
		log.Warn("skipped %s", p)
	}
	return loaders, err
}
