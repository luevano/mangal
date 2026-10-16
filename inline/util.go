package inline

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/util/sanitize"
)

// acquire is the provider's client (client.AcquireByID). Swapped in tests.
var acquire = client.AcquireByID

// search gets the provider's client, searches the query and picks the mangas with the
// manga selector.
func search(ctx context.Context, args Args) (*libmangal.Client, []MangaResult, error) {
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
	mangas, err := searchMangas(ctx, c, args.Query)
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

// searchMangas searches the query; a "mid:<id>" one gets the manga by its id when the provider
// can (Lua providers' MangaByID), else it's searched like any query (mangoprovider takes it).
func searchMangas(ctx context.Context, c *libmangal.Client, query string) ([]mangadata.Manga, error) {
	id, ok := selector.IDQuery(query)
	if !ok {
		return c.SearchMangasContext(ctx, query)
	}
	manga, found, err := c.MangaByIDContext(ctx, id)
	switch {
	case errors.Is(err, libmangal.ErrMangaByIDUnsupported):
		return c.SearchMangasContext(ctx, query)
	case err != nil:
		return nil, err
	case !found:
		return nil, nil
	}
	return []mangadata.Manga{manga}, nil
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
