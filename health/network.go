package health

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// ProviderManga is a provider's manga the network checks list the chapters of.
type ProviderManga struct {
	MangaID, Title string
	// Have are the library's chapter numbers of it.
	Have []float32
}

// newChapters is what the network check found of an unwatched manga on its source.
type newChapters struct {
	provider, mangaID string
	numbers           []float32
	err               error
}

// loadNetwork lists the chapters of the unwatched library mangas that may still get new
// ones (ongoing or unknown, with an installed source) on their providers: one provider's
// mangas one at a time (NewChapters holds its lock), Parallel providers at once.
func (c Checker) loadNetwork(ctx context.Context, s *snapshot) error {
	if c.NewChapters == nil {
		return errors.New("not available")
	}
	type todo struct {
		manga int64
		ProviderManga
	}
	byProvider := map[string][]todo{}
	var providers []string
	for _, m := range s.unwatched() {
		if library.StatusDone(m.Status) || m.Status == string(metadata.StatusHiatus) {
			continue
		}
		src, ok := installedSource(s, m)
		if !ok {
			continue
		}
		if _, ok := byProvider[src.Provider]; !ok {
			providers = append(providers, src.Provider)
		}
		byProvider[src.Provider] = append(byProvider[src.Provider], todo{manga: m.ID,
			ProviderManga: ProviderManga{MangaID: src.MangaID, Title: m.Title, Have: s.numbers[m.ID]}})
	}
	total := 0
	for _, list := range byProvider {
		total += len(list)
	}
	var mu sync.Mutex
	done := 0
	c.progress(fmt.Sprintf("looking for new chapters: 0/%d", total))
	sem := make(chan struct{}, max(1, c.Parallel))
	var wg sync.WaitGroup
	for _, provider := range providers {
		list := byProvider[provider]
		mangas := make([]ProviderManga, len(list))
		for i, t := range list {
			mangas[i] = t.ProviderManga
		}
		wg.Go(func() {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			wait := func(msg string) {
				mu.Lock()
				defer mu.Unlock()
				c.progress(msg)
			}
			err := c.NewChapters(ctx, provider, mangas, wait, func(i int, numbers []float32, err error) {
				mu.Lock()
				defer mu.Unlock()
				s.newChapters[list[i].manga] = newChapters{provider: provider, mangaID: list[i].MangaID, numbers: numbers, err: err}
				done++
				c.progress(fmt.Sprintf("looking for new chapters: %d/%d (%s)", done, total, provider))
			})
			if err != nil {
				mu.Lock()
				s.errors = append(s.errors, fmt.Sprintf("W12: %s: %s", provider, err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return ctx.Err()
}

// W12 (network): unwatched library mangas whose source has chapters newer than the
// library's: watch them.
func checkNewChapters(s *snapshot) []Finding {
	var out []Finding
	for _, m := range s.mangas {
		r, ok := s.newChapters[m.ID]
		if !ok {
			continue
		}
		switch {
		case errors.Is(r.err, watch.ErrMangaNotFound):
			out = append(out, Finding{Severity: Warning, Subject: mangaSubject(m),
				Message: fmt.Sprintf("not found on %s any more", r.provider),
				Fix: Fix{
					Commands: []string{fmt.Sprintf("mangal library move %d %s", m.ID, r.provider)},
					Note:     "moved to another manga id or provider: move it there (searches its title)",
					TUI:      tuiLibrary + ": move to a provider",
				}})
		case r.err != nil:
			s.errors = append(s.errors, fmt.Sprintf("W12: manga %d %s on %s: %s", m.ID, m.Title, r.provider, r.err))
		case len(r.numbers) != 0:
			newest := slices.MaxFunc(r.numbers, cmp.Compare[float32])
			out = append(out, Finding{Severity: Warning, Subject: mangaSubject(m),
				Message: fmt.Sprintf("%s on %s (up to %s): watch it?", plural(len(r.numbers), "new chapter"), r.provider, stringutil.FormatFloa32(newest)),
				Fix: Fix{
					Commands: []string{fmt.Sprintf("mangal watch add %s mid:%s", r.provider, quote(r.mangaID))},
					Note:     "its first check downloads them",
					TUI:      "health: add a watch",
					Action:   &Action{Kind: ActionWatch, Manga: m.ID, Provider: r.provider, MangaID: r.mangaID},
				}})
		}
	}
	return out
}
