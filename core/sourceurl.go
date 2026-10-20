package core

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"golang.org/x/sync/errgroup"
)

// MangaGone is the error for a manga the provider doesn't have (any more): it matches
// watch.ErrMangaNotFound and ErrNotFound.
func MangaGone(title, provider, id string) error {
	return goneError(fmt.Sprintf("%s: %q isn't on %s any more, it may have been removed (manga id %s)", watch.ErrMangaNotFound, title, provider, id))
}

type goneError string

func (e goneError) Error() string { return string(e) }

func (e goneError) Is(target error) bool {
	return target == watch.ErrMangaNotFound || target == ErrNotFound
}

// WebURL is true for an http(s) address: a provider's url can be anything, only a web
// page is opened.
func WebURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// SourceURL is the web page of the library manga's source (a provider's manga): the one
// recorded, else (or with refresh) the one its provider gives for the manga id, recorded
// then (a lookup's side effect, like a cache). MangaGone's error when the provider
// doesn't have it: the source is marked gone (its old url kept) and, until a refresh
// or a download finds it again, that's the answer without asking.
func (l *localLibrary) SourceURL(ctx context.Context, manga int64, provider, mangaID string, refresh bool) (string, error) {
	m, err := l.core.manga(manga)
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(m.Sources, func(s storage.Source) bool { return s.Provider == provider && s.MangaID == mangaID })
	if i < 0 {
		return "", &NotFoundError{What: fmt.Sprintf("%s has no source %s %q", m.Title, provider, mangaID)}
	}
	src := m.Sources[i]
	title := cmp.Or(src.Title, m.Title)
	switch {
	case refresh:
	case !src.Gone.IsZero():
		return "", MangaGone(title, l.core.providerName(src.Provider), src.MangaID)
	case WebURL(src.URL):
		return src.URL, nil
	}
	return l.core.lookupSourceURL(ctx, title, src)
}

// providerName is the installed provider's name, else the id.
func (c *Core) providerName(id string) string {
	if loaders, err := c.env.Loaders(); err == nil {
		if l, _, ok := libmangal.FindLoader(loaders, id); ok {
			return l.Info().Name
		}
	}
	return id
}

// lookupSourceURL gets the source's url from its provider and records it.
func (c *Core) lookupSourceURL(ctx context.Context, title string, src storage.Source) (string, error) {
	info, manga, found, err := c.env.ProviderManga(ctx, src.Provider, src.MangaID, title)
	switch {
	case err != nil:
		return "", fmt.Errorf("look %s %q up: %w", src.Provider, src.MangaID, err)
	case !found:
		if err := c.env.Storage.Library().SetSourceGone(src.Provider, src.MangaID, c.env.Now()); err != nil {
			return "", err
		}
		return "", MangaGone(title, cmp.Or(info.Name, src.Provider), src.MangaID)
	case !WebURL(manga.URL):
		return "", fmt.Errorf("%s gives no web page for %q (%q)", cmp.Or(info.Name, src.Provider), title, manga.URL)
	}
	if err := c.env.Storage.Library().SetSourceURL(src.Provider, src.MangaID, manga.URL); err != nil {
		return "", err
	}
	return manga.URL, nil
}

// providerManga is the provider's manga with the id (a search of title when it can't get
// one by id) with its client.
func providerManga(ctx context.Context, provider, id, title string) (libmangal.ProviderInfo, mangadata.MangaInfo, bool, error) {
	cl, _, release, err := client.HoldByID(ctx, provider)
	if err != nil {
		return libmangal.ProviderInfo{}, mangadata.MangaInfo{}, false, err
	}
	defer release()
	m, found, err := selector.ByID(ctx, client.Searcher(cl), id, title)
	if err != nil || !found {
		return cl.Info(), mangadata.MangaInfo{}, found, err
	}
	return cl.Info(), m.Info(), true, nil
}

// SourceURLsOptions are which sources LookupSourceURLs looks up.
type SourceURLsOptions struct {
	// All looks every source up again (refreshing the stored urls, the gone ones asked
	// again), not only those without a url and not gone.
	All bool `json:"all,omitempty"`
}

// SourceURLResult is a source looked up: its URL, or why it has none (Err, MangaGone's
// when the provider doesn't have it).
type SourceURLResult struct {
	Manga    int64
	Title    string
	Provider string
	MangaID  string
	URL      string
	Err      error
}

// SourceURLsReport is what LookupSourceURLs did.
type SourceURLsReport struct {
	// Results are the sources looked up, by title.
	Results []SourceURLResult
	// Skipped are the providers not asked, with why (not installed).
	Skipped []string
}

// sourceURLPause is the pause between two lookups on the same provider: 1 to 3 seconds,
// random so the site doesn't see a beat (a fair pace for a job nobody waits for).
// TODO: configurable.
func sourceURLPause() time.Duration { return time.Second + rand.N(2*time.Second) }

// sleep waits d, an error when ctx is done first.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// LookupSourceURLs looks the library's sources up on their providers (SourceURL) and
// records their urls (the sources not found are marked gone): those without one, or all.
// A provider is asked one source at a
// time with a pause between them, watch.parallel_providers providers at once; the
// providers not installed are skipped. A cancelled ctx stops it, the report has what was
// done.
func (l *localLibrary) LookupSourceURLs(ctx context.Context, opts SourceURLsOptions) (SourceURLsReport, error) {
	c := l.core
	var rep SourceURLsReport
	mangas, err := c.env.Storage.Library().Mangas()
	if err != nil {
		return rep, err
	}
	loaders, err := c.env.Loaders()
	if err != nil {
		return rep, err
	}
	byProvider := map[string][]SourceURLResult{}
	var providers []string
	for _, m := range mangas {
		for _, s := range m.Sources {
			if s.MangaID == "" || strings.HasPrefix(s.MangaID, storage.PlaceholderPrefix) || !opts.All && (WebURL(s.URL) || !s.Gone.IsZero()) {
				continue
			}
			if _, ok := byProvider[s.Provider]; !ok {
				providers = append(providers, s.Provider)
			}
			byProvider[s.Provider] = append(byProvider[s.Provider], SourceURLResult{Manga: m.ID, Title: cmp.Or(s.Title, m.Title), Provider: s.Provider, MangaID: s.MangaID})
		}
	}
	total := 0
	for _, p := range slices.Clone(providers) {
		n := len(byProvider[p])
		if _, _, ok := libmangal.FindLoader(loaders, p); !ok {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s: not installed (%d sources)", p, n))
			providers = slices.DeleteFunc(providers, func(id string) bool { return id == p })
			continue
		}
		total += n
	}

	var mu sync.Mutex
	g := errgroup.Group{}
	g.SetLimit(max(1, config.Watch.ParallelProviders.Get()))
	for _, p := range providers {
		g.Go(func() error {
			for i, r := range byProvider[p] {
				if i > 0 {
					if err := c.env.Sleep(ctx, sourceURLPause()); err != nil {
						return err
					}
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				r.URL, r.Err = c.lookupSourceURL(ctx, r.Title, storage.Source{Provider: r.Provider, MangaID: r.MangaID})
				if errors.Is(r.Err, context.Canceled) {
					return r.Err
				}
				mu.Lock()
				rep.Results = append(rep.Results, r)
				report(ctx, library.Progress{Phase: "looking up the source addresses", Done: len(rep.Results), Total: total, Item: r.Title})
				mu.Unlock()
			}
			return nil
		})
	}
	err = g.Wait()
	slices.SortStableFunc(rep.Results, func(a, b SourceURLResult) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)), strings.Compare(a.Provider, b.Provider))
	})
	return rep, err
}
