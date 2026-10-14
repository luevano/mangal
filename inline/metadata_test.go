package inline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/adrg/xdg"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	lmanilist "github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

func TestMain(m *testing.M) {
	// the dbs open once per process: before any test, keep them away from the user's
	dir, err := os.MkdirTemp("", "mangal-inline-test-")
	if err != nil {
		panic(err)
	}
	os.Setenv("MANGAL_CACHE_PATH", filepath.Join(dir, "cache"))
	os.Setenv("MANGAL_AUTH_KEYRING", "false")
	os.Setenv("MANGAL_PROVIDERS_PATH", filepath.Join(dir, "providers"))
	xdg.DataHome = filepath.Join(dir, "data")
	xdg.StateHome = filepath.Join(dir, "state")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// kitsuMeta is Kitsu metadata.
type kitsuMeta struct {
	*mangadata.Metadata
	id int
}

func (m *kitsuMeta) ID() metadata.ID {
	return metadata.ID{Raw: strconv.Itoa(m.id), Source: metadata.IDSourceKitsu, Code: metadata.IDCodeKitsu}
}

func aniManga(id int, title string) *lmanilist.Manga {
	m := &lmanilist.Manga{IDProvider: id}
	m.TitleGroup.English = title
	return m
}

// fakeMetaProvider is a metadata source with fixed search results and ids.
type fakeMetaProvider struct {
	src     client.MetadataSource
	byQuery map[string][]metadata.Metadata
	byID    map[int]metadata.Metadata
	logger  *logger.Logger
}

func (p *fakeMetaProvider) String() string { return p.src.Name }
func (p *fakeMetaProvider) Info() metadata.ProviderInfo {
	return metadata.ProviderInfo{ID: p.src.Key, Code: p.src.Code, Source: p.src.Source, Name: p.src.Name, Version: "0.1.0"}
}
func (p *fakeMetaProvider) SetLogger(l *logger.Logger) { p.logger = l }
func (p *fakeMetaProvider) Logger() *logger.Logger     { return p.logger }
func (p *fakeMetaProvider) SearchByID(_ context.Context, id int) (metadata.Metadata, bool, error) {
	m, ok := p.byID[id]
	return m, ok, nil
}
func (p *fakeMetaProvider) Search(_ context.Context, query string) ([]metadata.Metadata, error) {
	return p.byQuery[query], nil
}
func (p *fakeMetaProvider) SetMangaProgress(context.Context, int, int) error { return nil }
func (p *fakeMetaProvider) Authenticated() bool                              { return false }
func (p *fakeMetaProvider) User() metadata.User                              { return nil }
func (p *fakeMetaProvider) Login(context.Context, string) error              { return nil }
func (p *fakeMetaProvider) Logout() error                                    { return nil }

// metaManga is a manga whose metadata the provider sets once its page (volumes) is read,
// like a scraper's.
type metaManga struct {
	fakeManga
	meta metadata.Metadata
}

func (m *metaManga) Metadata() metadata.Metadata        { return m.meta }
func (m *metaManga) SetMetadata(meta metadata.Metadata) { m.meta = meta }

type metaProvider struct{ fakeProvider }

func (*metaProvider) SearchMangas(context.Context, string) ([]mangadata.Manga, error) {
	return []mangadata.Manga{&metaManga{fakeManga: fakeManga{mangadata.MangaInfo{Title: "Frieren", ID: "frieren"}}}}, nil
}
func (*metaProvider) MangaVolumes(_ context.Context, m mangadata.Manga) ([]mangadata.Volume, error) {
	mm := m.(*metaManga)
	if mm.meta == nil {
		mm.meta = &mangadata.Metadata{
			EnglishTitle:      "Frieren",
			AuthorList:        []string{"Yamada Kanehito"},
			DateStart:         metadata.Date{Year: 2020},
			PublicationStatus: metadata.StatusReleasing,
			ProviderID:        "frieren",
			ProviderIDCode:    "fk",
		}
	}
	return []mangadata.Volume{fakeVolume{&mm.fakeManga}}, nil
}

type metaLoader struct{ fakeLoader }

func (metaLoader) Load(context.Context) (libmangal.Provider, error) { return &metaProvider{}, nil }

// useClient makes c the provider's client for the run, with the metadata providers.
func useClient(t *testing.T, loader libmangal.ProviderLoader, metas ...*fakeMetaProvider) *libmangal.Client {
	t.Helper()
	ctx := context.Background()
	c, err := libmangal.NewClientContext(ctx, loader, libmangal.DefaultClientOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range metas {
		opts := metadata.DefaultProviderWithCacheOptions()
		opts.Provider = f
		p, err := metadata.NewProviderWithCache(opts)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.SetMetadataProvider(p); err != nil {
			t.Fatal(err)
		}
	}
	old := acquire
	t.Cleanup(func() { acquire = old })
	acquire = func(context.Context, string) (*libmangal.Client, []string, error) { return c, nil, nil }
	return c
}

func sourceOf(t *testing.T, key string) client.MetadataSource {
	t.Helper()
	src, err := client.ParseMetadataSource(key)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// runJSON is the first result of inline json, decoded.
func runJSON(t *testing.T, args Args) map[string]any {
	t.Helper()
	var runErr error
	out := stdout(t, func() { runErr = RunJSON(context.Background(), args) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	var got struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	if len(got.Results) == 0 {
		t.Fatalf("no results:\n%s", out)
	}
	return got.Results[0]
}

func TestJSONMetadataFallsBackToKitsu(t *testing.T) {
	ani := &fakeMetaProvider{src: sourceOf(t, "anilist"), byQuery: map[string][]metadata.Metadata{
		// too unlike the manga: a bad match
		"Frieren": {aniManga(1, "Something Else Entirely")},
	}}
	kitsu := &fakeMetaProvider{src: sourceOf(t, "kitsu"), byQuery: map[string][]metadata.Metadata{
		"Frieren": {&kitsuMeta{Metadata: &mangadata.Metadata{EnglishTitle: "Frieren"}, id: 42}},
	}}
	useClient(t, fakeLoader{}, ani, kitsu)

	r := runJSON(t, Args{Query: "frieren", Provider: "fake", MangaSelector: "first", ChapterSelector: "all"})
	if r["metadata_source"] != "kitsu" || r["metadata"] == nil {
		t.Fatalf("metadata %v from %v, want kitsu's", r["metadata"], r["metadata_source"])
	}
	if r["anilist"] != nil {
		t.Errorf("anilist %v, want null", r["anilist"])
	}
	bad, _ := r["anilist_bad_match"].(map[string]any)
	if bad == nil || bad["anilist"].(map[string]any)["id"] != 1.0 {
		t.Errorf("anilist_bad_match %v", r["anilist_bad_match"])
	}
}

func TestJSONMetadataAnilistMatch(t *testing.T) {
	ani := &fakeMetaProvider{src: sourceOf(t, "anilist"), byQuery: map[string][]metadata.Metadata{
		"Frieren": {aniManga(7, "Frieren")},
	}}
	kitsu := &fakeMetaProvider{src: sourceOf(t, "kitsu")}
	useClient(t, fakeLoader{}, ani, kitsu)

	r := runJSON(t, Args{Query: "frieren", Provider: "fake", MangaSelector: "first", ChapterSelector: "all"})
	if r["metadata_source"] != "anilist" {
		t.Fatalf("metadata_source %v", r["metadata_source"])
	}
	for _, key := range []string{"metadata", "anilist"} {
		if m, _ := r[key].(map[string]any); m == nil || m["id"] != 7.0 {
			t.Errorf("%s %v, want AniList 7", key, r[key])
		}
	}
}

func TestJSONMetadataBinding(t *testing.T) {
	ani := &fakeMetaProvider{src: sourceOf(t, "anilist"), byID: map[int]metadata.Metadata{9: aniManga(9, "Frieren AniList")}}
	kitsu := &fakeMetaProvider{src: sourceOf(t, "kitsu"), byID: map[int]metadata.Metadata{
		5: &kitsuMeta{Metadata: &mangadata.Metadata{EnglishTitle: "Frieren Kitsu"}, id: 5},
	}}
	useClient(t, fakeLoader{}, ani, kitsu)
	bindings, err := cache.Bindings()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []storage.Binding{
		{Provider: "fake", MangaID: "side", Source: metadata.IDCodeKitsu, ID: "5"},
		{Provider: "fake", MangaID: "side", Source: metadata.IDCodeAnilist, ID: "9"},
	} {
		if err := bindings.Set(b); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = bindings.Remove("fake", "side", metadata.IDCodeKitsu)
		_, _ = bindings.Remove("fake", "side", metadata.IDCodeAnilist)
	})

	// the AniList binding is the default (priority), the Kitsu one a backup
	r := runJSON(t, Args{Query: "frieren", Provider: "fake", MangaSelector: "1", ChapterSelector: "all"})
	if r["metadata_source"] != "anilist" {
		t.Fatalf("metadata_source %v, want anilist (the default binding)", r["metadata_source"])
	}
	if m, _ := r["anilist"].(map[string]any); m == nil || m["id"] != 9.0 {
		t.Errorf("anilist %v", r["anilist"])
	}
}

// TestJSONPreferProviderMetadata: a scraper manga only has its metadata once its page was read.
func TestJSONPreferProviderMetadata(t *testing.T) {
	ani := &fakeMetaProvider{src: sourceOf(t, "anilist"), byQuery: map[string][]metadata.Metadata{
		"Frieren": {aniManga(7, "Frieren")},
	}}
	useClient(t, metaLoader{}, ani)

	r := runJSON(t, Args{Query: "frieren", Provider: "fake", MangaSelector: "first", ChapterSelector: "all", PreferProviderMetadata: true})
	if r["metadata_source"] != SourceProvider {
		t.Fatalf("metadata_source %v, want the provider's", r["metadata_source"])
	}
	if m, _ := r["metadata"].(map[string]any); m == nil || m["provider_id"] != "frieren" {
		t.Errorf("metadata %v", r["metadata"])
	}
	if r["anilist"] != nil {
		t.Errorf("anilist %v, not searched with valid provider metadata", r["anilist"])
	}
}

// TestDownloadPreferProviderMetadata: the provider's metadata is checked after the volumes
// were read, so a scraper's counts.
func TestDownloadPreferProviderMetadata(t *testing.T) {
	useClient(t, metaLoader{})
	var options libmangal.DownloadOptions
	var got []mangadata.Chapter
	old := download
	t.Cleanup(func() { download = old })
	download = func(_ context.Context, _ *libmangal.Client, chapters []mangadata.Chapter, o libmangal.DownloadOptions, _ func(client.Event)) client.Results {
		options, got = o, chapters
		return nil
	}

	if _, err := runDownload(context.Background(), Args{Query: "frieren", Provider: "fake", MangaSelector: "first", ChapterSelector: "last", PreferProviderMetadata: true}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("%d chapters", len(got))
	}
	if options.SearchMetadata {
		t.Error("metadata searched, the provider's is valid and preferred")
	}
}
