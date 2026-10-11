package loader

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/luevano/libmangal"
	"github.com/luevano/luaprovider"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/provider/info"
	"github.com/luevano/mangal/v5/util/afs"
)

const mainLua = "main.lua"

// LuaLoaders are the Lua providers in the providers dir. A provider that can't be loaded
// is skipped, its error is in problems.
func LuaLoaders() (loaders []libmangal.ProviderLoader, problems []error, err error) {
	dir := path.Providers()
	if _, err := afs.Afero.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	loaders, problems, err = getLoaderBundles("", dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read providers dir: %w", err)
	}
	return loaders, problems, nil
}

// getLoaderBundles returns a linear list of all providers with their parent bundle id attached.
// A bundle is a collection of providers, any amount of nested providers (or even bundles) is valid.
// The providers that can't be loaded are skipped and returned as problems, err is only for dir.
func getLoaderBundles(bundleID, dir string) (loaders []libmangal.ProviderLoader, problems []error, err error) {
	dirEntries, err := afs.Afero.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}

	for _, dirEntry := range dirEntries {
		if !dirEntry.IsDir() {
			continue
		}

		dirEntryPath := filepath.Join(dir, dirEntry.Name())

		// only dirs with a 'mangal.toml' are providers
		isProvider, err := afs.Afero.Exists(filepath.Join(dirEntryPath, info.Filename))
		if err != nil {
			problems = append(problems, fmt.Errorf("provider %s: %w", dirEntryPath, err))
			continue
		}
		if !isProvider {
			continue
		}

		l, p, err := getLoaders(bundleID, dirEntryPath)
		if err != nil {
			problems = append(problems, fmt.Errorf("provider %s: %w", dirEntryPath, err))
			continue
		}
		loaders = append(loaders, l...)
		problems = append(problems, p...)
	}

	return loaders, problems, nil
}

// getLoaders returns either the providerloader or the next subset of bundles.
// It all depends if the provider type is Lua (actual provider loader) or bundle.
func getLoaders(bundleID, dir string) ([]libmangal.ProviderLoader, []error, error) {
	infoFile, err := afs.Afero.Open(filepath.Join(dir, info.Filename))
	if err != nil {
		return nil, nil, err
	}
	defer infoFile.Close()

	providerInfo, err := info.New(infoFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", info.Filename, err)
	}

	providerInfo.ProviderInfo = withBundleID(providerInfo.ProviderInfo, bundleID)

	switch providerInfo.Type {
	case info.TypeLua:
		loader, err := newLoader(providerInfo.ProviderInfo, dir)
		if err != nil {
			return nil, nil, err
		}
		return []libmangal.ProviderLoader{loader}, nil, nil
	case info.TypeBundle:
		return getLoaderBundles(providerInfo.ID, dir)
	default:
		return nil, nil, fmt.Errorf("unknown provider type %v", providerInfo.Type)
	}
}

func newLoader(info libmangal.ProviderInfo, dir string) (libmangal.ProviderLoader, error) {
	script, err := afs.Afero.ReadFile(filepath.Join(dir, mainLua))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s is missing", mainLua)
	}
	if err != nil {
		return nil, err
	}

	loader, err := luaprovider.NewLoader(script, info, LuaOptions(dir))
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", mainLua, err)
	}
	return loader, nil
}

// withBundleID attaches the bundle id to the provider ids (current and old) to make them unique.
func withBundleID(info libmangal.ProviderInfo, bundleID string) libmangal.ProviderInfo {
	if bundleID == "" {
		return info
	}
	info.ID = fmt.Sprint(bundleID, "-", info.ID)
	history := make([]libmangal.ProviderAlias, len(info.History))
	for i, alias := range info.History {
		alias.ID = fmt.Sprint(bundleID, "-", alias.ID)
		history[i] = alias
	}
	info.History = history
	return info
}
