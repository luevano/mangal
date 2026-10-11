package manager

import (
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
