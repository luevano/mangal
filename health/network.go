package health

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/luevano/mangoprovider"
	"github.com/luevano/mangoprovider/fetch"
	"golang.org/x/sync/errgroup"
)

// ProviderManga is a provider's manga the network checks ask about.
type ProviderManga struct {
	MangaID, Title string
	// Have are the library's chapter numbers of it.
	Have []float32
}

// ProviderChapters is what a provider lists of a manga.
type ProviderChapters struct {
	// New are the chapters a watch check would download now: newer than Have.
	New []float32
	// Listed are the numbers of the chapters it has, Unavailable those it lists as not
	// downloadable (e.g. expired).
	Listed, Unavailable []float32
}

// ChaptersOf is what the provider's chapters are to a library that has the numbers
// have: New as a watch check picks them (watch.Pick without gaps).
func ChaptersOf(chapters []mangadata.Chapter, have []float32) ProviderChapters {
	haveSet := map[float32]bool{}
	var last *float32
	for _, n := range have {
		haveSet[n] = true
		if last == nil || n > *last {
			last = &n
		}
	}
	var pc ProviderChapters
	picked, _, _ := watch.Pick(chapters, haveSet, last, nil, false, nil, nil)
	for _, ch := range picked {
		pc.New = append(pc.New, ch.Info().Number)
	}
	seen := map[float32]bool{}
	for _, ch := range chapters {
		info := ch.Info()
		switch {
		case info.Unavailable == "" && !slices.Contains(pc.Listed, info.Number):
			pc.Listed = append(pc.Listed, info.Number)
		case info.Unavailable != "" && !seen[info.Number]:
			pc.Unavailable = append(pc.Unavailable, info.Number)
		}
		seen[info.Number] = true
	}
	// a number listed once available and once not is available
	pc.Unavailable = slices.DeleteFunc(pc.Unavailable, func(n float32) bool { return slices.Contains(pc.Listed, n) })
	return pc
}

// Probe is what asking a provider for one manga gave (A2).
type Probe struct {
	// Took is how long the request took (not the provider's loading).
	Took time.Duration
	// Statuses are what the provider reported meanwhile (a challenge solved, a mirror
	// down).
	Statuses []string
	// Account is the provider's account as last checked, "" when it has none.
	Account string
	// Skipped says why it wasn't asked.
	Skipped string
	Err     error
}

// answered is true when the provider answered: the manga not being there is an answer.
func (p Probe) answered() bool {
	return p.Skipped == "" && (p.Err == nil || notFound(p.Err))
}

func notFound(err error) bool {
	return errors.Is(err, watch.ErrMangaNotFound) || errors.Is(err, libmangal.ErrNotFound)
}

// challenged is true when one of the statuses is a Cloudflare challenge.
func (p Probe) challenged() bool {
	return slices.ContainsFunc(p.Statuses, func(s string) bool { return strings.Contains(strings.ToLower(s), "cloudflare challenge") })
}

// chapterResult is what the network checks found of a library manga on a provider.
type chapterResult struct {
	provider, mangaID string
	// watch is the watch whose manga it is, 0 for the library manga's source
	watch int64
	// forNew is true when W12 asked (an unwatched manga that may get new chapters)
	forNew bool
	ProviderChapters
	err error
}

// loadProbes asks each provider the library and the watches use for one of its mangas.
func (c Checker) loadProbes(ctx context.Context, s *snapshot) error {
	if c.Probe == nil {
		return errors.New("not available")
	}
	mangas := map[string]ProviderManga{}
	add := func(provider, mangaID, title string) {
		if mangaID == "" || strings.HasPrefix(mangaID, storage.PlaceholderPrefix) || !s.installed(provider) {
			return
		}
		// an old id is its provider's
		provider = s.providerInfo(provider).ID
		if _, ok := mangas[provider]; ok {
			return
		}
		mangas[provider] = ProviderManga{MangaID: mangaID, Title: title}
	}
	for _, w := range s.watches {
		if w.Enabled && !w.ProviderMissing && !w.NeedsRelink {
			if _, old := s.oldSite(w.Provider, w.MangaID); !old {
				add(w.Provider, w.MangaID, w.Title)
			}
		}
	}
	for _, m := range s.mangas {
		if src, ok := installedSource(s, m); ok {
			add(src.Provider, src.MangaID, m.Title)
		}
	}
	s.probeOrder = slices.Sorted(maps.Keys(mangas))
	var mu sync.Mutex
	done := 0
	c.progress(fmt.Sprintf("asking the providers: 0/%d", len(mangas)))
	c.parallel(ctx, s.probeOrder, func(provider string) {
		p := c.Probe(ctx, provider, mangas[provider])
		mu.Lock()
		defer mu.Unlock()
		s.probes[provider] = p
		s.probed[provider] = mangas[provider]
		done++
		c.progress(fmt.Sprintf("asking the providers: %d/%d (%s)", done, len(mangas), provider))
	})
	return ctx.Err()
}

// parallel runs do for each item, Parallel at once.
func (c Checker) parallel(ctx context.Context, items []string, do func(string)) {
	var g errgroup.Group
	g.SetLimit(max(1, c.Parallel))
	for _, item := range items {
		g.Go(func() error {
			if ctx.Err() == nil {
				do(item)
			}
			return nil
		})
	}
	_ = g.Wait()
}

// loadChapters lists the chapters of the mangas the network checks ask about on their
// providers: the unwatched ones that may still get new chapters (W12) and those with gaps
// (G2). One provider's mangas one at a time (Chapters holds its lock), Parallel providers
// at once; a provider the probe found unreachable is skipped.
func (c Checker) loadChapters(ctx context.Context, s *snapshot) error {
	if c.Chapters == nil {
		return errors.New("not available")
	}
	type todo struct {
		manga int64
		chapterResult
		ProviderManga
	}
	var list []todo
	queued := map[int64]int{}
	if s.running["W12"] || s.running["W15"] {
		for _, m := range s.unwatched() {
			if library.StatusDone(m.Status) || m.Status == string(metadata.StatusHiatus) {
				continue
			}
			if src, ok := installedSource(s, m); ok {
				queued[m.ID] = len(list)
				list = append(list, todo{manga: m.ID, provider: src.Provider, mangaID: src.MangaID, forNew: true,
					MangaID: src.MangaID, Title: m.Title, Have: s.numbers[m.ID]})
			}
		}
	}
	if s.running["G2"] {
		for _, m := range s.mangas {
			if _, ok := queued[m.ID]; ok {
				continue
			}
			if g := s.gapsOf(m); len(g.missing) != 0 {
				if src, ok := s.gapSource(m, g); ok {
					queued[m.ID] = len(list)
					list = append(list, todo{manga: m.ID, chapterResult: src,
						MangaID: src.mangaID, Title: m.Title, Have: s.numbers[m.ID]})
				}
			}
		}
	}
	byProvider := map[string][]int{}
	var providers []string
	for i, t := range list {
		if p, ok := s.probes[s.providerInfo(t.provider).ID]; ok && !p.answered() && p.Skipped == "" {
			continue
		}
		if _, ok := byProvider[t.provider]; !ok {
			providers = append(providers, t.provider)
		}
		byProvider[t.provider] = append(byProvider[t.provider], i)
	}
	for _, provider := range s.probeOrder {
		if p := s.probes[provider]; !p.answered() && p.Skipped == "" && slices.ContainsFunc(list, func(t todo) bool { return s.providerInfo(t.provider).ID == provider }) {
			s.errors = append(s.errors, fmt.Sprintf("W12/G2: %s skipped, it didn't answer (A2)", provider))
		}
	}
	total := 0
	for _, ids := range byProvider {
		total += len(ids)
	}
	var mu sync.Mutex
	done := 0
	c.progress(fmt.Sprintf("listing chapters: 0/%d", total))
	c.parallel(ctx, providers, func(provider string) {
		ids := byProvider[provider]
		mangas := make([]ProviderManga, len(ids))
		for i, id := range ids {
			mangas[i] = list[id].ProviderManga
		}
		wait := func(msg string) {
			mu.Lock()
			defer mu.Unlock()
			c.progress(msg)
		}
		err := c.Chapters(ctx, provider, mangas, wait, func(i int, chapters ProviderChapters, err error) {
			mu.Lock()
			defer mu.Unlock()
			t := list[ids[i]]
			r := t.chapterResult
			r.ProviderChapters, r.err = chapters, err
			s.chapters[t.manga] = r
			done++
			c.progress(fmt.Sprintf("listing chapters: %d/%d (%s)", done, total, provider))
		})
		if err != nil {
			mu.Lock()
			s.errors = append(s.errors, fmt.Sprintf("W12/G2: %s: %s", provider, err))
			mu.Unlock()
		}
	})
	return ctx.Err()
}

// loadSearch looks for an AniList id of the mangas M1 has no suggestion for (by their
// MyAnimeList id, else their title), and of those whose AniList id M5 found wrong.
func (c Checker) loadSearch(ctx context.Context, s *snapshot) error {
	var errs []error
	if s.running["M1"] && c.AnilistFromMAL != nil {
		if err := c.loadFromMAL(ctx, s); err != nil {
			errs = append(errs, fmt.Errorf("MyAnimeList ids: %w", err))
		}
	}
	if c.SearchAnilist == nil {
		return errors.Join(append(errs, errors.New("not available"))...)
	}
	var mangas []storage.Manga
	var queries []metadata.Query
	for _, m := range s.mangas {
		_, wrong := s.adoptedIssue(m, metadata.IDCodeAnilist)
		wrong = wrong && s.running["M5"]
		if linked, suggest, _ := s.anilistSuggestion(m); !wrong && (!s.running["M1"] || linked || suggest != "" || s.fromMAL[m.ID] != "") {
			continue
		}
		if q := library.MangaQuery(m); len(q.Titles) != 0 {
			mangas, queries = append(mangas, m), append(queries, q)
		}
	}
	if len(queries) == 0 {
		return errors.Join(errs...)
	}
	c.progress(fmt.Sprintf("searching AniList for %s", stringutil.Quantify(len(queries), "manga", "mangas")))
	matches, found, err := c.SearchAnilist(ctx, queries)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for i, m := range mangas {
		if i < len(found) && found[i] && matches[i].Metadata != nil {
			s.anilist[m.ID] = matches[i]
		}
	}
	return errors.Join(errs...)
}

// W12 (network): unwatched library mangas whose source has chapters newer than the
// library's: watch them.
func checkNewChapters(s *snapshot) []Finding {
	var out []Finding
	for _, m := range s.mangas {
		r, ok := s.chapters[m.ID]
		if !ok || !r.forNew {
			continue
		}
		switch {
		case errors.Is(r.err, watch.ErrMangaNotFound) && s.gone(m.ID):
			// L11's finding
			continue
		case errors.Is(r.err, watch.ErrMangaNotFound):
			out = append(out, Finding{Severity: Warning, Subject: mangaSubject(m), Network: true,
				Message: fmt.Sprintf("not found on %s any more", r.provider),
				Fix: Fix{
					Commands: []string{fmt.Sprintf("mangal library move %d %s", m.ID, r.provider)},
					Note:     "moved to another manga id or provider: move it there (searches its title)",
					TUI:      tuiLibrary + ": move to a provider",
				}})
		case r.err != nil:
			s.errors = append(s.errors, fmt.Sprintf("W12: manga %d %s on %s: %s", m.ID, m.Title, r.provider, r.err))
		case len(r.New) != 0:
			newest := slices.MaxFunc(r.New, cmp.Compare[float32])
			out = append(out, Finding{Severity: Warning, Subject: mangaSubject(m), Network: true,
				Message: fmt.Sprintf("%s on %s (up to %s): watch it?", stringutil.Quantify(len(r.New), "new chapter", "new chapters"), r.provider, stringutil.FormatFloat32(newest)),
				Fix: Fix{
					Commands: []string{fmt.Sprintf("mangal watch add %s --id %s", r.provider, quote(r.mangaID))},
					Note:     "its first check downloads them",
					TUI:      "health: add a watch",
					Action:   &Action{Kind: ActionWatch, Manga: m.ID, Provider: r.provider, MangaID: r.mangaID},
				}})
		}
	}
	return out
}

// G2 (network): G1's gaps against the provider's chapter list: which missing chapters it
// has (a gap check downloads them), lists as unavailable, or doesn't have at all.
func checkGapsConfirmed(s *snapshot) []Finding {
	var out []Finding
	for _, m := range s.mangas {
		r, ok := s.chapters[m.ID]
		if !ok {
			continue
		}
		g := s.gapsOf(m)
		if len(g.missing) == 0 {
			continue
		}
		if r.err != nil {
			if !errors.Is(r.err, watch.ErrMangaNotFound) || !r.forNew {
				s.errors = append(s.errors, fmt.Sprintf("G2: manga %d %s on %s: %s", m.ID, m.Title, r.provider, r.err))
			}
			continue
		}
		var there, unavailable, absent []int
		for _, n := range g.missing {
			switch {
			case slices.Contains(r.Listed, float32(n)):
				there = append(there, n)
			case slices.Contains(r.Unavailable, float32(n)):
				unavailable = append(unavailable, n)
			default:
				absent = append(absent, n)
			}
		}
		f := Finding{Severity: Warning, Subject: mangaSubject(m), Network: true}
		if len(there) != 0 {
			f.Message = fmt.Sprintf("%d of %s on %s, downloadable: %s", len(there), stringutil.Quantify(len(g.missing), "missing chapter", "missing chapters"), r.provider, formatRanges(there))
		} else {
			f.Severity = Info
			f.Message = fmt.Sprintf("%s not on %s", stringutil.Quantify(len(g.missing), "missing chapter", "missing chapters"), r.provider)
			if len(g.missing) > 1 {
				f.Message = fmt.Sprintf("none of the %d missing chapters on %s", len(g.missing), r.provider)
			}
		}
		if len(unavailable) != 0 {
			f.Details = append(f.Details, "listed as unavailable: "+formatRanges(unavailable))
		}
		if len(absent) != 0 && len(there) != 0 {
			f.Details = append(f.Details, "not listed: "+formatRanges(absent))
		}
		notThere := slices.Concat(unavailable, absent)
		slices.Sort(notThere)
		switch {
		case r.watch != 0:
			if len(there) != 0 {
				f.Fix.Commands = []string{fmt.Sprintf("mangal watch check %d --gaps", r.watch)}
				f.Fix.TUI = tuiWatches + ": fill gaps"
			}
			if len(notThere) != 0 && len(notThere) <= 20 {
				f.Fix.Commands = append(f.Fix.Commands, fmt.Sprintf("mangal watch set %d --skip %s", r.watch, joinInts(notThere, ",")))
				f.Fix.Note = "the skip list stops gap checks looking for the ones it doesn't have"
			}
		case len(there) != 0:
			f.Fix.Commands = []string{fmt.Sprintf("mangal watch add %s --id %s --gaps", r.provider, quote(r.mangaID))}
			f.Fix.Note = "not watched: a watch with gap filling downloads them"
			f.Fix.TUI = tuiLibrary + ": open the manga"
		default:
			f.Fix.Note = "the provider doesn't have them: another one may, or dismiss it"
			f.Fix.TUI = tuiLibrary + ": open the manga"
		}
		out = append(out, f)
	}
	return out
}

// gapSource is where a gap check of the manga would look: its enabled watch (else its
// first), or its source when not watched.
func (s *snapshot) gapSource(m storage.Manga, g gaps) (chapterResult, bool) {
	if len(g.watches) != 0 {
		w := g.watches[0]
		for _, x := range g.watches {
			if x.Enabled {
				w = x
				break
			}
		}
		if w.ProviderMissing || w.NeedsRelink || !s.installed(w.Provider) || strings.HasPrefix(w.MangaID, storage.PlaceholderPrefix) {
			return chapterResult{}, false
		}
		return chapterResult{provider: w.Provider, mangaID: w.MangaID, watch: w.ID}, true
	}
	src, ok := installedSource(s, m)
	return chapterResult{provider: src.Provider, mangaID: src.MangaID}, ok
}

// ProbeTimeout is how long a provider gets to answer A2's request: a Cloudflare challenge
// takes FlareSolverr up to a minute.
const ProbeTimeout = 90 * time.Second

// A2 (network): the providers the library and the watches use, asked for one of their
// mangas: one summary of those that answered, a finding for each that didn't.
func checkProviders(s *snapshot) []Finding {
	var out []Finding
	summary := Finding{Severity: Info, Subject: Subject{Kind: "providers", Name: "providers"}, Network: true}
	answered := 0
	for _, provider := range s.probeOrder {
		p := s.probes[provider]
		manga := s.probed[provider]
		subject := Subject{Kind: "provider", Name: provider}
		item := Finding{Severity: Info, Subject: subject, Network: true}
		switch {
		case p.Skipped != "":
			item.Message = "not asked: " + p.Skipped
		case p.answered():
			answered++
			item.Message = "answered in " + formatTook(p.Took)
			if p.challenged() {
				item.Message += ", through a Cloudflare challenge"
			}
			if p.Err != nil {
				item.Message += fmt.Sprintf(" (%s isn't there any more)", manga.Title)
			}
		default:
			out = append(out, providerProblem(subject, manga, p))
			continue
		}
		if p.Account != "" {
			item.Message += "; account: " + p.Account
		}
		summary.Items = append(summary.Items, item)
	}
	if len(summary.Items) != 0 {
		summary.Message = fmt.Sprintf("%d of %s answered", answered, stringutil.Quantify(len(s.probeOrder), "provider", "providers"))
		summary.Fix.Note = "each was asked for one of its mangas; --only A2 lists them"
		out = append(out, summary)
	}
	return out
}

// providerProblem is the finding of a provider that didn't answer.
func providerProblem(subject Subject, manga ProviderManga, p Probe) Finding {
	f := Finding{Severity: Warning, Subject: subject, Network: true,
		Details: []string{fmt.Sprintf("asked for %s (%s): %s", manga.Title, manga.MangaID, p.Err)}}
	msg := p.Err.Error()
	lower := strings.ToLower(msg)
	switch {
	case errors.Is(p.Err, fetch.ErrChallenged) || strings.Contains(lower, "cloudflare") || strings.Contains(lower, "challenge"):
		f.Message = "blocked by a Cloudflare challenge"
		f.Fix.Commands = []string{"mangal config set providers.headless.use_flaresolverr true"}
		f.Fix.Note = "FlareSolverr solves them (providers.headless.flaresolverr_url)"
	case errors.Is(p.Err, libmangal.ErrRateLimited):
		f.Message = "rate limited"
		f.Fix.Note = "it answers again later; its watch checks wait it out"
	case errors.Is(p.Err, context.DeadlineExceeded):
		f.Message = "no answer in " + formatTook(ProbeTimeout)
	case errors.Is(p.Err, mangoprovider.ErrSiteDown):
		f.Message = "site down"
		f.Fix.Note = "its watch checks fail until it's back"
	default:
		f.Message = "failed: " + msg
	}
	if p.Account != "" {
		f.Details = append(f.Details, "account: "+p.Account)
	}
	f.Details = append(f.Details, p.Statuses...)
	return f
}

func formatTook(d time.Duration) string {
	if d < 10*time.Second {
		return d.Round(100 * time.Millisecond).String()
	}
	return d.Round(time.Second).String()
}

// sourceResult is what L11's lookup found of a library manga's current source.
type sourceResult struct {
	provider, mangaID string
	found             bool
	err               error
}

// currentSource is the source the manga's folder follows (Mangas lists it first) when its
// provider is installed and its id is the current site's (L7 and L10 report the others).
func currentSource(s *snapshot, m storage.Manga) (storage.Source, bool) {
	if len(m.Sources) == 0 {
		return storage.Source{}, false
	}
	src := m.Sources[0]
	if src.MangaID == "" || strings.HasPrefix(src.MangaID, storage.PlaceholderPrefix) || !s.installed(src.Provider) {
		return storage.Source{}, false
	}
	if _, old := s.oldSite(src.Provider, src.MangaID); old {
		return storage.Source{}, false
	}
	return src, true
}

// currentSources are the library mangas' current sources as L11's Part.
func (s *snapshot) currentSources() map[int64]string {
	out := make(map[int64]string, len(s.mangas))
	for _, m := range s.mangas {
		if src, ok := currentSource(s, m); ok {
			out[m.ID] = s.providerInfo(src.Provider).ID + "/" + src.MangaID
		}
	}
	return out
}

// loadSources looks the current source of each library manga up by its id (L11): one
// provider's mangas one at a time (Lookup goes through its shared client, so its
// throttle applies), Parallel providers at once.
func (c Checker) loadSources(ctx context.Context, s *snapshot) error {
	if c.Lookup == nil {
		return errors.New("not available")
	}
	type todo struct {
		manga   int64
		mangaID string
	}
	byProvider := map[string][]todo{}
	for _, m := range s.mangas {
		if src, ok := currentSource(s, m); ok {
			provider := s.providerInfo(src.Provider).ID
			byProvider[provider] = append(byProvider[provider], todo{m.ID, src.MangaID})
		}
	}
	total := 0
	for _, list := range byProvider {
		total += len(list)
	}
	var mu sync.Mutex
	done := 0
	c.progress(fmt.Sprintf("looking up the library's sources: 0/%d", total))
	c.parallel(ctx, slices.Sorted(maps.Keys(byProvider)), func(provider string) {
		list := byProvider[provider]
		ids := make([]string, len(list))
		for i, t := range list {
			ids[i] = t.mangaID
		}
		err := c.Lookup(ctx, provider, ids, func(i int, found bool, err error) {
			mu.Lock()
			defer mu.Unlock()
			s.sources[list[i].manga] = sourceResult{provider: provider, mangaID: list[i].mangaID, found: found, err: err}
			done++
			c.progress(fmt.Sprintf("looking up the library's sources: %d/%d (%s)", done, total, provider))
		})
		mu.Lock()
		defer mu.Unlock()
		switch {
		case errors.Is(err, libmangal.ErrMangaByIDUnsupported):
			s.noByID[provider] = len(list)
		case err != nil && ctx.Err() == nil:
			s.errors = append(s.errors, fmt.Sprintf("L11: %s: %s", provider, err))
		}
	})
	return ctx.Err()
}

// gone is true when L11 found the manga's current source gone.
func (s *snapshot) gone(manga int64) bool {
	r, ok := s.sources[manga]
	return ok && (r.err == nil && !r.found || notFound(r.err))
}

// L11 (network): library mangas whose current source the provider doesn't have any more.
// A lookup that failed otherwise isn't a finding (Report.Errors says why), never a "gone".
func checkSourceGone(s *snapshot) []Finding {
	var out []Finding
	for _, m := range s.mangas {
		r, ok := s.sources[m.ID]
		switch {
		case !ok:
			continue
		case !s.gone(m.ID):
			if r.err != nil {
				s.errors = append(s.errors, fmt.Sprintf("L11: manga %d %s on %s: %s", m.ID, m.Title, r.provider, r.err))
			}
			continue
		}
		f := Finding{Severity: Warning, Subject: mangaSubject(m), Network: true, Part: r.provider + "/" + r.mangaID,
			Message: fmt.Sprintf("not on %s any more (it may have been removed)", r.provider),
			Details: []string{"its manga id there: " + r.mangaID}}
		var w *watch.Watch
		for _, x := range s.watchesOf(m) {
			if x.Enabled && s.providerInfo(x.Provider).ID == r.provider && x.MangaID == r.mangaID {
				w = &x
				break
			}
		}
		switch {
		case library.StatusDone(m.Status):
			f.Severity = Info
			f.Message += fmt.Sprintf(", %s: nothing is lost, its chapters are local", strings.ToLower(m.Status))
			f.Fix.Note = fmt.Sprintf("nothing to do; mangal library move %d <provider> if you want a source to re-download from", m.ID)
		case w != nil:
			reason := fmt.Sprintf("its manga isn't on %s any more", r.provider)
			f.Fix = Fix{
				Commands: []string{fmt.Sprintf("mangal watch move %d <provider>", w.ID), fmt.Sprintf("mangal watch hold %d provider", w.ID)},
				Note:     "re-link its watch to another provider (or this one, if the manga got a new id there), or mark it to do later",
				TUI:      tuiWatches + ": re-link manga, hold",
				Action:   &Action{Kind: ActionHold, Watch: w.ID, Mark: watch.MarkProvider, Reason: reason},
			}
		default:
			f.Fix = Fix{
				Commands: []string{fmt.Sprintf("mangal library move %d <provider>", m.ID), fmt.Sprintf("mangal library hold %d provider", m.ID)},
				Note:     "move it to another provider (or this one, if the manga got a new id there: the move searches its title), or mark it to do later",
				TUI:      tuiLibrary + ": move to a provider, hold",
			}
		}
		out = append(out, f)
	}
	for _, provider := range slices.Sorted(maps.Keys(s.noByID)) {
		out = append(out, Finding{Severity: Info, Subject: Subject{Kind: "provider", Name: provider}, Network: true,
			Message: fmt.Sprintf("can't look mangas up by id: %s not checked", stringutil.Quantify(s.noByID[provider], "library source", "library sources"))})
	}
	return out
}
