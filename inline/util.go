package inline

import (
	"context"
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

	sel := args.MangaSelector
	if args.ID != "" {
		args.status("getting manga %q from %s", args.ID, args.Provider)
		sel = "id"
	} else {
		args.status("searching %q on %s", args.Query, args.Provider)
	}
	mangas, err := selector.Search(ctx, client.Searcher(c), args.ID, args.Query)
	if err != nil {
		return nil, nil, err
	}
	switch {
	case len(mangas) == 0 && args.ID != "":
		return nil, nil, fmt.Errorf("no manga with id %q in %s", args.ID, args.Provider)
	case len(mangas) == 0:
		return nil, nil, fmt.Errorf("no mangas found with provider %q and query %q", args.Provider, args.Query)
	}

	indexes, err := selector.Mangas(sel, args.Query, mangas)
	if err != nil {
		return nil, nil, err
	}
	results := make([]MangaResult, len(indexes))
	for i, index := range indexes {
		results[i] = MangaResult{Index: index, Manga: mangas[index]}
	}
	return c, results, nil
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
