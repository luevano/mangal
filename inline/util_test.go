package inline

import (
	"context"
	"errors"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
)

// byIDProvider gets mangas by id (a Lua provider's MangaByID), its searches fail the test.
type byIDProvider struct {
	fakeProvider
	t *testing.T
}

func (p *byIDProvider) SearchMangas(context.Context, string) ([]mangadata.Manga, error) {
	p.t.Error("searched instead of getting the manga by id")
	return nil, nil
}

func (*byIDProvider) MangaByID(_ context.Context, id string) (mangadata.Manga, bool, error) {
	if id != "frieren" {
		return nil, false, nil
	}
	return &fakeManga{mangadata.MangaInfo{Title: "Frieren", ID: id}}, true, nil
}

type byIDLoader struct {
	fakeLoader
	t *testing.T
}

func (l byIDLoader) Load(context.Context) (libmangal.Provider, error) {
	return &byIDProvider{t: l.t}, nil
}

func TestSearchByID(t *testing.T) {
	useClient(t, byIDLoader{t: t})
	for _, args := range []Args{
		{ID: "frieren", Provider: "fake", MangaSelector: "all"},
		{ID: "frieren", Provider: "fake", MangaSelector: "exact"},
		// deprecated
		{Query: "mid:frieren", Provider: "fake", MangaSelector: "all"},
	} {
		_, results, err := search(context.Background(), args)
		if err != nil || len(results) != 1 || results[0].Manga.Info().ID != "frieren" {
			t.Fatalf("%+v: got %v, %v", args, results, err)
		}
	}
	if _, _, err := search(context.Background(), Args{ID: "other", Provider: "fake", MangaSelector: "all"}); err == nil {
		t.Error("unknown id: no error")
	}

	// without MangaByID it's an error, not a search of the query
	useClient(t, fakeLoader{})
	if _, _, err := search(context.Background(), Args{ID: "frieren", Provider: "fake", MangaSelector: "all"}); !errors.Is(err, libmangal.ErrMangaByIDUnsupported) {
		t.Errorf("got %v, want ErrMangaByIDUnsupported", err)
	}
}
