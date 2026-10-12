package selector

import (
	"context"
	"errors"
	"fmt"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
)

// Searcher is a provider to find a manga in.
type Searcher interface {
	Info() libmangal.ProviderInfo
	// MangaByID is libmangal's Client.MangaByIDContext, its error matches
	// libmangal.ErrMangaByIDUnsupported when the provider can't.
	MangaByID(ctx context.Context, id string) (mangadata.Manga, bool, error)
	SearchMangas(ctx context.Context, query string) ([]mangadata.Manga, error)
}

// Find is the manga with the id (or the id of a "mid:<id>" query), else the one the
// manga selector (exact if empty) picks from a search of the query.
func Find(ctx context.Context, src Searcher, id, query, sel string) (mangadata.Manga, error) {
	if id == "" {
		id, _ = IDQuery(query)
	}
	if id != "" {
		manga, found, err := src.MangaByID(ctx, id)
		switch {
		case errors.Is(err, libmangal.ErrMangaByIDUnsupported) && query != "":
			// mangoprovider's searches take "mid:" queries too
			return pick(ctx, src, query, "id", id)
		case errors.Is(err, libmangal.ErrMangaByIDUnsupported):
			return nil, fmt.Errorf("%s can't get a manga by id, add it with a title query", src.Info().ID)
		case err != nil:
			return nil, err
		case !found:
			return nil, fmt.Errorf("no manga with id %q in %s", id, src.Info().ID)
		}
		return manga, nil
	}
	if query == "" {
		return nil, errors.New("no manga id or query")
	}
	if sel == "" {
		sel = "exact"
	}
	return pick(ctx, src, query, sel, "")
}

// pick searches the query and picks one manga with the selector (with an id, it must be
// that manga).
func pick(ctx context.Context, src Searcher, query, sel, id string) (mangadata.Manga, error) {
	mangas, err := src.SearchMangas(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", query, err)
	}
	if len(mangas) == 0 {
		return nil, fmt.Errorf("no mangas found in %s with %q", src.Info().ID, query)
	}
	indexes, err := Mangas(sel, query, mangas)
	if err != nil {
		return nil, err
	}
	if len(indexes) != 1 {
		return nil, fmt.Errorf("manga selector %q picks %d mangas, it needs to pick 1", sel, len(indexes))
	}
	manga := mangas[indexes[0]]
	if id != "" && manga.Info().ID != id {
		return nil, fmt.Errorf("no manga with id %q in %s", id, src.Info().ID)
	}
	return manga, nil
}
