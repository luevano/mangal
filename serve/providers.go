package serve

import (
	"context"
	"fmt"
	"github.com/luevano/mangal/v5/log"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/provider/install"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// Swapped in tests.
var (
	updateProviders = func(ctx context.Context) ([]install.UpdateResult, error) {
		dir, err := path.Ensure(path.Providers())
		if err != nil {
			return nil, err
		}
		return install.UpdateEach(ctx, dir, io.Discard)
	}
	setMirror                         = config.SetMirror
	forgetMirror, defaultForgetMirror = cache.DeleteMirror, cache.DeleteMirror
	defaultUpdateProviders            = updateProviders
)

// providersMu keeps provider updates and mirror changes one at a time.
var providersMu sync.Mutex

// providerUpdateTimeout bounds a providers update (git pulls).
const providerUpdateTimeout = 5 * time.Minute

// reloadProviders loads the providers again and drops their clients, so the next request
// makes them with the new code or options.
func (a *API) reloadProviders() ([]string, error) {
	var problems []string
	if a.Browser != nil {
		errs, err := a.Browser.Reload()
		if err != nil {
			return nil, err
		}
		for _, e := range errs {
			problems = append(problems, e.Error())
		}
	}
	if err := client.CloseAll(); err != nil {
		log.Warn("close the provider clients: %s", err)
	}
	return problems, nil
}

// SetProviderMirror implements apiv1.StrictServerInterface: the TUI's mirrors screen.
func (a *API) SetProviderMirror(_ context.Context, request apiv1.SetProviderMirrorRequestObject) (apiv1.SetProviderMirrorResponseObject, error) {
	l, err := a.loader(request.Provider)
	if err != nil {
		return nil, err
	}
	info := l.Info()
	mirror := strings.TrimSpace(deref(request.Body).Mirror)
	if mirror != "" && mirror != info.Website && !slices.Contains(info.Mirrors, mirror) {
		return nil, badRequest("%s isn't %s's website or one of its mirrors (%s)", mirror, info.Name, strings.Join(append([]string{info.Website}, info.Mirrors...), ", "))
	}
	if mirror == info.Website {
		// the default, no need to keep it in the config
		mirror = ""
	}
	providersMu.Lock()
	defer providersMu.Unlock()
	if err := setMirror(info.ID, mirror); err != nil {
		return nil, fmt.Errorf("set the mirror of %s: %w", info.Name, err)
	}
	// the choice wins over the mirror that last worked
	if err := forgetMirror(info.ID); err != nil {
		log.Warn("forget the last mirror of %s: %s", info.Name, err)
	}
	if _, err := a.reloadProviders(); err != nil {
		return nil, err
	}
	log.Info("%s prefers %s (set through the API)", info.Name, cmpOr(mirror, info.Website))
	if l, err = a.loader(info.ID); err != nil {
		return nil, err
	}
	return apiv1.SetProviderMirror200JSONResponse(toAPIProvider(l.Info())), nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// UpdateProviders implements apiv1.StrictServerInterface: `mangal providers up`, then the
// providers loaded again. Installing has no endpoint (code from a url sent over HTTP).
func (a *API) UpdateProviders(ctx context.Context, _ apiv1.UpdateProvidersRequestObject) (apiv1.UpdateProvidersResponseObject, error) {
	if !providersMu.TryLock() {
		return nil, newAPIError(http.StatusConflict, watch.CodeBusy, "the providers are being updated")
	}
	defer providersMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, providerUpdateTimeout)
	defer cancel()
	results, err := updateProviders(ctx)
	if err != nil {
		return nil, err
	}
	out := apiv1.UpdateProviders200JSONResponse{Providers: []apiv1.ProviderUpdate{}}
	updated := 0
	for _, r := range results {
		u := apiv1.ProviderUpdate{Dir: r.Dir, Status: r.Status}
		if r.Err != nil {
			u.Error = new(r.Err.Error())
			log.Warn("update provider %s: %s", r.Dir, r.Err)
		}
		if r.Status == install.StatusUpdated {
			updated++
		}
		out.Providers = append(out.Providers, u)
	}
	if updated != 0 {
		problems, err := a.reloadProviders()
		if err != nil {
			return nil, err
		}
		if len(problems) != 0 {
			out.Problems = &problems
		}
	}
	log.Info("providers updated through the API: %d of %d", updated, len(results))
	return out, nil
}

func toAPIProvider(info libmangal.ProviderInfo) apiv1.Provider {
	out := apiv1.Provider{Id: info.ID, Name: info.Name, Version: info.Version}
	if info.Description != "" {
		out.Description = &info.Description
	}
	if info.Website != "" {
		out.Website = &info.Website
		preferred := cmpOr(config.Providers.Mirrors.Get()[info.ID], info.Website)
		out.Preferred = &preferred
	}
	if info.Icon != "" {
		out.Icon = &info.Icon
	}
	if len(info.Mirrors) != 0 {
		out.Mirrors = &info.Mirrors
	}
	return out
}
