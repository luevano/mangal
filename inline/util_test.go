package inline

import (
	"context"
	"strings"
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
	_, results, err := search(context.Background(), Args{Query: "mid:frieren", Provider: "fake", MangaSelector: "all"})
	if err != nil || len(results) != 1 || results[0].Manga.Info().ID != "frieren" {
		t.Fatalf("got %v, %v", results, err)
	}
	if _, _, err := search(context.Background(), Args{Query: "mid:other", Provider: "fake", MangaSelector: "all"}); err == nil {
		t.Error("unknown id: no error")
	}

	// without MangaByID the query is searched (mangoprovider takes "mid:" queries)
	useClient(t, fakeLoader{})
	// the fake's search finds 2
	if _, _, err := search(context.Background(), Args{Query: "mid:frieren", Provider: "fake", MangaSelector: "all"}); err == nil || !strings.Contains(err.Error(), "2 mangas found") {
		t.Errorf("not searched: %v", err)
	}
}
