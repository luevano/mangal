package library

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/spf13/afero"
)

type fakeMeta struct {
	metadata.Metadata
	title string
	id    metadata.ID
}

func (m fakeMeta) Title() string             { return m.title }
func (m fakeMeta) AlternateTitles() []string { return nil }
func (m fakeMeta) ID() metadata.ID           { return m.id }
func (m fakeMeta) StartDate() metadata.Date  { return metadata.Date{} }

type fakeIDProvider struct {
	name    string
	code    metadata.IDCode
	titles  map[int]string
	byIDs   [][]int
	queries []metadata.Query
}

func (p *fakeIDProvider) Info() metadata.ProviderInfo { return metadata.ProviderInfo{Name: p.name} }

func (p *fakeIDProvider) meta(id int) metadata.Metadata {
	return fakeMeta{title: p.titles[id], id: metadata.ID{Raw: strconv.Itoa(id), Code: p.code}}
}

func (p *fakeIDProvider) SearchByIDsContext(_ context.Context, ids []int) (map[int]metadata.Metadata, error) {
	p.byIDs = append(p.byIDs, ids)
	found := map[int]metadata.Metadata{}
	for _, id := range ids {
		if _, ok := p.titles[id]; ok {
			found[id] = p.meta(id)
		}
	}
	return found, nil
}

func (p *fakeIDProvider) FindClosestManyContext(_ context.Context, queries []metadata.Query) ([]metadata.Match, []bool, error) {
	p.queries = append(p.queries, queries...)
	matches := make([]metadata.Match, len(queries))
	found := make([]bool, len(queries))
	for i, q := range queries {
		var metas []metadata.Metadata
		for id := range p.titles {
			metas = append(metas, p.meta(id))
		}
		if ranked := metadata.Rank(q, metas); len(ranked) != 0 {
			matches[i], found[i] = ranked[0], true
		}
	}
	return matches, found, nil
}

func TestCheckIDs(t *testing.T) {
	root := t.TempDir()
	library := newLibrary(t)
	fs := afero.NewOsFs()
	for _, dir := range []string{
		"Claymore (2001) [alid-30583]",
		"Berserk (1989) [alid-1]",
		"Gone [alid-999]",
		"Some Manga (2010) [ktid-12345]",
		"Other [ktid-5]",
		"Vagabond (1998)",
		"Nothing Like It",
		"Mal Tag [malid-7]",
	} {
		writeCBZ(t, filepath.Join(root, "mango-mangadex", dir, "[0001.0] One.cbz"), "1")
	}
	// linked by a download, its ids came from the provider
	linked := filepath.Join(root, "mango-mangadex", "Linked [alid-2]")
	writeMarker(t, linked, libmangal.MangaDirInfo{Provider: "mango-mangadex", Manga: mangadata.MangaInfo{ID: "x", Title: "Linked"},
		MetadataIDs: []metadata.ID{{Raw: "2", Source: metadata.IDSourceAnilist, Code: metadata.IDCodeAnilist}}})
	writeCBZ(t, filepath.Join(linked, "[0001.0] One.cbz"), "1")

	if _, err := (Scanner{FS: fs, Library: library}).Scan(root, true); err != nil {
		t.Fatal(err)
	}
	mangas, err := library.Mangas()
	if err != nil {
		t.Fatal(err)
	}

	ani := &fakeIDProvider{name: "Anilist", code: metadata.IDCodeAnilist, titles: map[int]string{30583: "Claymore", 1: "Cowboy Bebop", 2: "Something Else", 656: "Vagabond"}}
	kitsu := &fakeIDProvider{name: "Kitsu", code: metadata.IDCodeKitsu, titles: map[int]string{12345: "Some Manga"}}
	checker := IDChecker{FS: fs, Providers: map[metadata.IDSource]IDProvider{metadata.IDSourceAnilist: ani, metadata.IDSourceKitsu: kitsu}, Suggest: ani}
	checks, err := checker.Check(context.Background(), mangas)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, c := range checks {
		got = append(got, c.String())
	}
	want := []string{
		`Berserk (1989) [alid-1]: alid-1 is "Cowboy Bebop" on Anilist (score 0.18)`,
		`Gone [alid-999]: alid-999 not found on Anilist`,
		`Other [ktid-5]: ktid-5 not found on Kitsu`,
		`Vagabond (1998): no id, Anilist suggests alid-656 "Vagabond" (score 1.00)`,
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
	// one batch per provider, the linked folder isn't checked
	if len(ani.byIDs) != 1 || slices.Contains(ani.byIDs[0], 2) || len(kitsu.byIDs) != 1 {
		t.Errorf("anilist by ids %v, kitsu %v", ani.byIDs, kitsu.byIDs)
	}
	// the mal tag isn't checked, nor searched for
	for _, q := range ani.queries {
		if q.Titles[0] == "Mal Tag" {
			t.Errorf("searched %v", q)
		}
	}
}
