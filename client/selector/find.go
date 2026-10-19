package selector

import (
	"cmp"
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

// Search gets the manga with the id (nil when there's none), else searches the query. A
// deprecated "mid:<id>" query (IDQuery) is the id. A provider that can't get a manga by
// id is an error matching libmangal.ErrMangaByIDUnsupported.
func Search(ctx context.Context, src Searcher, id, query string) ([]mangadata.Manga, error) {
	if id == "" {
		id, _ = IDQuery(query)
	}
	if id == "" {
		mangas, err := src.SearchMangas(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("search %q: %w", query, err)
		}
		return mangas, nil
	}
	manga, found, err := src.MangaByID(ctx, id)
	switch {
	case errors.Is(err, libmangal.ErrMangaByIDUnsupported):
		return nil, fmt.Errorf("%s can't get a manga by id, search its title: %w", src.Info().ID, libmangal.ErrMangaByIDUnsupported)
	case err != nil:
		return nil, fmt.Errorf("get manga %q: %w", id, err)
	case !found:
		return nil, nil
	}
	return []mangadata.Manga{manga}, nil
}

// Find is the manga with the id (see Search; with a title query too, ByID), else the one
// the manga selector (exact if empty) picks from a search of the query. An id the
// provider doesn't have is an error matching libmangal.ErrNotFound.
func Find(ctx context.Context, src Searcher, id, query, sel string) (mangadata.Manga, error) {
	if qid, ok := IDQuery(query); ok {
		id, query = cmp.Or(id, qid), ""
	}
	if id != "" {
		var (
			manga mangadata.Manga
			found bool
			err   error
		)
		if query != "" {
			manga, found, err = ByID(ctx, src, id, query)
		} else {
			var mangas []mangadata.Manga
			mangas, err = Search(ctx, src, id, "")
			if found = len(mangas) != 0; found {
				manga = mangas[0]
			}
		}
		switch {
		case err != nil:
			return nil, err
		case !found:
			return nil, notFoundError{id: id, provider: src.Info().ID}
		}
		return manga, nil
	}
	if query == "" {
		return nil, errors.New("no manga id or query")
	}
	mangas, err := Search(ctx, src, "", query)
	if err != nil {
		return nil, err
	}
	if len(mangas) == 0 {
		return nil, fmt.Errorf("no mangas found in %s with %q", src.Info().ID, query)
	}
	if sel == "" {
		sel = "exact"
	}
	indexes, err := Mangas(sel, query, mangas)
	if err != nil {
		return nil, err
	}
	if len(indexes) != 1 {
		return nil, fmt.Errorf("manga selector %q picks %d mangas, it needs to pick 1", sel, len(indexes))
	}
	return mangas[indexes[0]], nil
}

// ByID is the manga with the id; a provider that can't get one by its id finds it in a
// search of its title. Found is false when the provider doesn't have it.
func ByID(ctx context.Context, src Searcher, id, title string) (manga mangadata.Manga, found bool, err error) {
	manga, found, err = src.MangaByID(ctx, id)
	if !errors.Is(err, libmangal.ErrMangaByIDUnsupported) {
		return manga, found, err
	}
	mangas, err := src.SearchMangas(ctx, title)
	if err != nil {
		return nil, false, fmt.Errorf("search %q: %w", title, err)
	}
	for _, m := range mangas {
		if m.Info().ID == id {
			return m, true, nil
		}
	}
	return nil, false, nil
}

// notFoundError is a manga id the provider doesn't have (any more), it matches
// libmangal.ErrNotFound.
type notFoundError struct{ id, provider string }

func (e notFoundError) Error() string {
	return fmt.Sprintf("no manga with id %q in %s", e.id, e.provider)
}

func (notFoundError) Is(target error) bool { return target == libmangal.ErrNotFound }
