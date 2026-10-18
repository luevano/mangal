package selector

import (
	"context"
	"errors"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
)

type idManga struct {
	mangadata.Manga
	id, title string
}

func (m idManga) Info() mangadata.MangaInfo { return mangadata.MangaInfo{ID: m.id, Title: m.title} }

// searcher has the mangas, byID false is a provider without MangaByID.
type searcher struct {
	mangas   []mangadata.Manga
	byID     bool
	searched []string
}

func (s *searcher) Info() libmangal.ProviderInfo { return libmangal.ProviderInfo{ID: "fake"} }

func (s *searcher) MangaByID(_ context.Context, id string) (mangadata.Manga, bool, error) {
	if !s.byID {
		return nil, false, libmangal.ErrMangaByIDUnsupported
	}
	for _, m := range s.mangas {
		if m.Info().ID == id {
			return m, true, nil
		}
	}
	return nil, false, nil
}

func (s *searcher) SearchMangas(_ context.Context, query string) ([]mangadata.Manga, error) {
	s.searched = append(s.searched, query)
	return s.mangas, nil
}

func TestFind(t *testing.T) {
	ctx := context.Background()
	mangas := []mangadata.Manga{idManga{id: "1", title: "Berserk"}, idManga{id: "2", title: "Frieren"}}
	src := &searcher{mangas: mangas, byID: true}
	for _, tc := range []struct{ id, query, want string }{
		{"2", "", "2"},
		{"", "frieren", "2"},
		// deprecated
		{"", "mid:2", "2"},
	} {
		m, err := Find(ctx, src, tc.id, tc.query, "")
		if err != nil || m.Info().ID != tc.want {
			t.Errorf("Find(%q, %q) = %v, %v, want %s", tc.id, tc.query, m, err, tc.want)
		}
	}
	if _, err := Find(ctx, src, "3", "", ""); err == nil {
		t.Error("unknown id: no error")
	}
	if len(src.searched) != 1 {
		t.Errorf("searched %q, want only the title", src.searched)
	}

	// a provider without MangaByID: an error, the query isn't searched
	src = &searcher{mangas: mangas}
	if _, err := Find(ctx, src, "", "mid:2", ""); !errors.Is(err, libmangal.ErrMangaByIDUnsupported) {
		t.Errorf("got %v, want ErrMangaByIDUnsupported", err)
	}
	if len(src.searched) != 0 {
		t.Errorf("searched %q", src.searched)
	}
	// with a title, the title's search has it
	if m, err := Find(ctx, src, "2", "Frieren", ""); err != nil || m.Info().ID != "2" {
		t.Errorf("Find(2, Frieren) = %v, %v", m, err)
	}
	// ByID finds it in a search of the title
	if m, found, err := ByID(ctx, src, "2", "Frieren"); err != nil || !found || m.Info().ID != "2" {
		t.Errorf("ByID = %v, %v, %v", m, found, err)
	}
	if _, found, err := ByID(ctx, src, "3", "Frieren"); err != nil || found {
		t.Errorf("ByID of an unknown id = %v, %v", found, err)
	}
}
