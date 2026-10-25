package core

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
)

// urlSite is a fake of the providers' sites: the mangas they have, the lookups asked.
type urlSite struct {
	mu      sync.Mutex
	asked   []string
	busy    map[string]int
	overlap bool
}

func (s *urlSite) manga(_ context.Context, provider, id, _ string) (libmangal.ProviderInfo, mangadata.MangaInfo, bool, error) {
	s.mu.Lock()
	s.asked = append(s.asked, provider+" "+id)
	s.busy[provider]++
	s.overlap = s.overlap || s.busy[provider] > 1
	s.mu.Unlock()
	time.Sleep(time.Millisecond)
	s.mu.Lock()
	s.busy[provider]--
	s.mu.Unlock()
	info := libmangal.ProviderInfo{ID: provider, Name: strings.TrimPrefix(provider, "mango-")}
	if id == "gone" {
		return info, mangadata.MangaInfo{}, false, nil
	}
	return info, mangadata.MangaInfo{ID: id, URL: "https://" + provider + "/title/" + id}, true, nil
}

func urlCore(t *testing.T, providers ...string) (*Core, *urlSite) {
	t.Helper()
	c, _ := testCore(t)
	site := &urlSite{busy: map[string]int{}}
	c.env.ProviderManga = site.manga
	c.env.Loaders = func() ([]libmangal.ProviderLoader, error) {
		var loaders []libmangal.ProviderLoader
		for _, p := range providers {
			info := libmangal.ProviderInfo{ID: p, Name: p, Version: "1.0.0"}
			// as mangoprovider's
			info.Traits.AvoidUnattended = p == "mango-mangaplus"
			loaders = append(loaders, infoLoader{info})
		}
		return loaders, nil
	}
	return c, site
}

func TestSourceURL(t *testing.T) {
	c, site := urlCore(t, "mango-mangadex")
	ctx := context.Background()
	m := addManga(t, c, "Dark Gathering", "mango-mangadex", "md", "Dark Gathering", 1)

	want := "https://mango-mangadex/title/md"
	for range 2 {
		got, err := c.Library.SourceURL(ctx, m.ID, "mango-mangadex", "md", false)
		if err != nil || got != want {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	if len(site.asked) != 1 {
		t.Errorf("asked %q, the second time is the stored one", site.asked)
	}
	if m, _ := c.Library.Manga(ctx, m.ID); m.Sources[0].URL != want {
		t.Errorf("stored %+v", m.Sources)
	}

	gone := addManga(t, c, "Gone", "mango-mangadex", "gone", "Gone", 1)
	_, err := c.Library.SourceURL(ctx, gone.ID, "mango-mangadex", "gone", false)
	if !errors.Is(err, watch.ErrMangaNotFound) || !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), `"Gone" isn't on mangadex any more`) {
		t.Errorf("gone: %v", err)
	}
	// marked gone, the old url kept: the same answer without asking, until a refresh
	site.asked = nil
	if g, _ := c.Library.Manga(ctx, gone.ID); g.Sources[0].Gone.IsZero() {
		t.Errorf("not marked gone %+v", g.Sources)
	}
	if _, err := c.Library.SourceURL(ctx, gone.ID, "mango-mangadex", "gone", false); !errors.Is(err, watch.ErrMangaNotFound) || len(site.asked) != 0 {
		t.Errorf("gone again: %v, asked %q", err, site.asked)
	}
	if _, err := c.Library.SourceURL(ctx, gone.ID, "mango-mangadex", "gone", true); !errors.Is(err, watch.ErrMangaNotFound) || len(site.asked) != 1 {
		t.Errorf("gone refreshed: %v, asked %q", err, site.asked)
	}

	// a stale url: a refresh gets the new one
	if err := c.env.Storage.Library().SetSourceURL("mango-mangadex", "md", "https://old/md"); err != nil {
		t.Fatal(err)
	}
	if got, err := c.Library.SourceURL(ctx, m.ID, "mango-mangadex", "md", false); got != "https://old/md" || err != nil {
		t.Errorf("stored: %q, %v", got, err)
	}
	if got, err := c.Library.SourceURL(ctx, m.ID, "mango-mangadex", "md", true); got != want || err != nil {
		t.Errorf("refreshed: %q, %v", got, err)
	}

	if _, err := c.Library.SourceURL(ctx, m.ID, "mango-mangapill", "md", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("not a source: %v", err)
	}
}

func TestLookupSourceURLs(t *testing.T) {
	c, site := urlCore(t, "mango-mangadex", "mango-mangapill", "mango-mangaplus")
	ctx := context.Background()
	addManga(t, c, "A", "mango-mangadex", "a", "A", 1)
	addManga(t, c, "B", "mango-mangadex", "b", "B", 1)
	addManga(t, c, "C", "mango-mangadex", "gone", "C", 1)
	addManga(t, c, "D", "mango-mangapill", "d", "D", 1)
	addManga(t, c, "E", "mango-comick", "e", "E", 1)
	// off in jobs.urls.providers by default
	addManga(t, c, "F", "mango-mangaplus", "f", "F", 1)
	if err := c.env.Storage.Library().SetSourceURL("mango-mangadex", "a", "https://mangadex.org/title/a"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var pauses []time.Duration
	c.env.Sleep = func(_ context.Context, d time.Duration) error {
		mu.Lock()
		defer mu.Unlock()
		pauses = append(pauses, d)
		return nil
	}
	var progress []library.Progress
	pctx := WithProgress(ctx, func(p library.Progress) { progress = append(progress, p) })

	rep, err := c.Library.LookupSourceURLs(pctx, SourceURLsOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rep.Results {
		line := r.Title + " " + r.URL
		if r.Err != nil {
			line = r.Title + " gone"
		}
		got = append(got, line)
	}
	if want := "B https://mango-mangadex/title/b,C gone,D https://mango-mangapill/title/d"; strings.Join(got, ",") != want {
		t.Errorf("results %q, want %q", got, want)
	}
	if len(rep.Skipped) != 2 || !strings.Contains(rep.Skipped[0], "mango-comick: not installed") || !strings.Contains(rep.Skipped[1], "mango-mangaplus: turned off") {
		t.Errorf("skipped %q", rep.Skipped)
	}
	// mangadex's two lookups have a pause between them, mangapill's one none
	if len(pauses) != 1 || pauses[0] < time.Second || pauses[0] >= 3*time.Second {
		t.Errorf("pauses %v", pauses)
	}
	if site.overlap {
		t.Error("two lookups on a provider at once")
	}
	if len(progress) != 3 || progress[2].Done != 3 || progress[2].Total != 3 {
		t.Errorf("progress %+v", progress)
	}
	if b, _, _ := c.env.Storage.Library().MangaBySource("mango-mangadex", "b"); b.Sources[0].URL != "https://mango-mangadex/title/b" {
		t.Errorf("not stored %+v", b.Sources)
	}

	// missing: the gone one isn't asked again
	site.asked = nil
	if rep, err = c.Library.LookupSourceURLs(ctx, SourceURLsOptions{}); err != nil || len(rep.Results) != 0 || len(site.asked) != 0 {
		t.Errorf("missing again %+v, %v, asked %q", rep, err, site.asked)
	}

	// all: the stored ones again too
	site.asked = nil
	if rep, err = c.Library.LookupSourceURLs(ctx, SourceURLsOptions{All: true}); err != nil || len(rep.Results) != 4 {
		t.Errorf("all %+v, %v", rep, err)
	}
	if a, _, _ := c.env.Storage.Library().MangaBySource("mango-mangadex", "a"); a.Sources[0].URL != "https://mango-mangadex/title/a" {
		t.Errorf("all didn't replace %+v", a.Sources)
	}

	// cancelled during a pause: what was done is reported
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.env.Sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	rep, err = c.Library.LookupSourceURLs(cctx, SourceURLsOptions{All: true})
	if !errors.Is(err, context.Canceled) || len(rep.Results) == 0 || len(rep.Results) >= 4 {
		t.Errorf("cancelled: %d results, %v", len(rep.Results), err)
	}
}
