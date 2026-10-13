package inline

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	lmanilist "github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/util/sanitize"
)

// acquire is the provider's client (client.AcquireByID). Swapped in tests.
var acquire = client.AcquireByID

// search gets the provider's client, searches the query and picks the mangas with the
// manga selector.
func search(ctx context.Context, args Args) (*libmangal.Client, []MangaResult, error) {
	args.status("loading %s", args.Provider)
	c, warnings, err := acquire(ctx, args.Provider)
	args.print(func() {
		for _, w := range warnings {
			fmt.Fprintln(os.Stderr, "warning: "+sanitize.Text(w))
		}
	})
	if err != nil {
		return nil, nil, err
	}

	args.status("searching %q on %s", args.Query, args.Provider)
	mangas, err := c.SearchMangasContext(ctx, args.Query)
	if err != nil {
		return nil, nil, fmt.Errorf("search %q: %w", args.Query, err)
	}
	if len(mangas) == 0 {
		return nil, nil, fmt.Errorf("no mangas found with provider %q and query %q", args.Provider, args.Query)
	}

	indexes, err := selector.Mangas(args.MangaSelector, args.Query, mangas)
	if err != nil {
		return nil, nil, err
	}
	results := make([]MangaResult, len(indexes))
	for i, index := range indexes {
		results[i] = MangaResult{Index: index, Manga: mangas[index]}
	}
	return c, results, nil
}

// TODO: refactor to handle metadata in general, not just anilist;
// use the provider metadata if preferred and search metadata in general otherwise
func assignAnilist(ctx context.Context, c *libmangal.Client, args Args, mangaResults []MangaResult) error {
	if len(mangaResults) == 0 {
		return nil
	}
	ani, err := c.GetMetadataProvider(metadata.IDSourceAnilist)
	if err != nil {
		return err
	}
	if err := anilist.EnsureLogin(ctx); err != nil {
		log.Debug("anilist login: %s", err)
	}
	args.status("looking up %d manga(s) on AniList", len(mangaResults))

	matches := make([]metadata.Match, len(mangaResults))
	found := make([]bool, len(mangaResults))
	if args.AnilistID != 0 {
		var meta metadata.Metadata
		meta, found[0], err = ani.SearchByIDContext(ctx, args.AnilistID)
		for i := range matches {
			matches[i], found[i] = metadata.Match{Metadata: meta, Score: 1, Bound: true}, found[0]
		}
	} else {
		// a manga's AniList binding (mangal bind) over a match by title
		bound, berr := anilistBindings(c.Info().ID, mangaResults)
		if berr != nil {
			return berr
		}
		var mangas []mangadata.Manga
		var unbound []int
		for i, r := range mangaResults {
			id, ok := bound[i]
			if !ok {
				mangas, unbound = append(mangas, r.Manga), append(unbound, i)
				continue
			}
			var meta metadata.Metadata
			if meta, found[i], err = ani.SearchByIDContext(ctx, id); err != nil {
				break
			}
			matches[i] = metadata.Match{Metadata: meta, Score: 1, Bound: true}
		}
		if err == nil && len(mangas) > 0 {
			// all at once, AniList allows few requests a minute
			var m []metadata.Match
			var f []bool
			m, f, err = c.MatchMangasContext(ctx, ani, mangas)
			for j, i := range unbound {
				if j < len(m) {
					matches[i], found[i] = m[j], f[j]
				}
			}
		}
	}
	if err != nil {
		// the results are still useful without it
		log.Warn("anilist: %s", err)
		args.print(func() { fmt.Fprintf(os.Stderr, "warning: anilist: %s\n", sanitize.Text(err.Error())) })
		return nil
	}

	for i, match := range matches {
		aniMeta, ok := match.Metadata.(*lmanilist.Manga)
		if !found[i] || !ok {
			continue
		}
		if match.Bad() {
			mangaResults[i].AnilistBadMatch = &BadMatch{Score: match.Score, Anilist: aniMeta}
			continue
		}
		mangaResults[i].Anilist = aniMeta
	}
	return nil
}

// anilistBindings are the AniList ids the results' mangas are bound to, by result index.
func anilistBindings(provider string, results []MangaResult) (map[int]int, error) {
	bindings, err := cache.Bindings()
	if err != nil {
		return nil, err
	}
	bound := map[int]int{}
	for i, r := range results {
		list, err := bindings.Get(provider, r.Manga.Info().ID)
		if err != nil {
			return nil, err
		}
		for _, b := range list {
			if id, err := strconv.Atoi(b.ID); err == nil && b.Source == metadata.IDCodeAnilist {
				bound[i] = id
			}
		}
	}
	return bound, nil
}

// chapters are the manga's chapters the chapter selector picks.
func chapters(ctx context.Context, c *libmangal.Client, args Args, manga mangadata.Manga) ([]mangadata.Chapter, error) {
	args.status("getting the chapters of %q", manga)
	all, err := client.MangaChapters(ctx, c, manga)
	if err != nil {
		return nil, err
	}
	selected, err := selector.Chapters(args.ChapterSelector, all)
	if err != nil {
		return nil, fmt.Errorf("chapters of %q: %w", manga, err)
	}
	return selected, nil
}
