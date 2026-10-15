package client

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
)

type fakeMeta struct {
	metadata.Metadata
	id    metadata.ID
	title string
}

func (m *fakeMeta) ID() metadata.ID         { return m.id }
func (m *fakeMeta) Title() string           { return m.title }
func (m *fakeMeta) String() string          { return m.title }
func (m *fakeMeta) ExtraIDs() []metadata.ID { return nil }

// fakeMetaProvider is a metadata source with fixed mangas, recording the calls.
type fakeMetaProvider struct {
	src    MetadataSource
	byID   map[int]string
	logger *logger.Logger
	// err is SearchByID's
	err error

	mu       sync.Mutex
	ids      []int
	searches []string
	progress map[int]int
}

func (p *fakeMetaProvider) String() string { return p.src.Name }
func (p *fakeMetaProvider) Info() metadata.ProviderInfo {
	return metadata.ProviderInfo{ID: p.src.Key, Code: p.src.Code, Source: p.src.Source, Name: p.src.Name, Version: "0.1.0"}
}
func (p *fakeMetaProvider) SetLogger(l *logger.Logger) { p.logger = l }
func (p *fakeMetaProvider) Logger() *logger.Logger     { return p.logger }
func (p *fakeMetaProvider) SearchByID(_ context.Context, id int) (metadata.Metadata, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ids = append(p.ids, id)
	if p.err != nil {
		return nil, false, p.err
	}
	title, ok := p.byID[id]
	if !ok {
		return nil, false, nil
	}
	return &fakeMeta{id: metadata.ID{Raw: strconv.Itoa(id), Source: p.src.Source, Code: p.src.Code}, title: title}, true, nil
}
func (p *fakeMetaProvider) Search(_ context.Context, query string) ([]metadata.Metadata, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.searches = append(p.searches, query)
	return nil, nil
}
func (p *fakeMetaProvider) SetMangaProgress(_ context.Context, id, chapter int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.progress == nil {
		p.progress = map[int]int{}
	}
	p.progress[id] = chapter
	return nil
}
func (p *fakeMetaProvider) Authenticated() bool                 { return true }
func (p *fakeMetaProvider) User() metadata.User                 { return nil }
func (p *fakeMetaProvider) Login(context.Context, string) error { return nil }
func (p *fakeMetaProvider) Logout() error                       { return nil }

type fakeMetaProviders map[metadata.IDSource]*metadata.ProviderWithCache

func (f fakeMetaProviders) GetMetadataProvider(id metadata.IDSource) (*metadata.ProviderWithCache, error) {
	p, ok := f[id]
	if !ok {
		return nil, fmt.Errorf("no metadata provider with source %d", id)
	}
	return p, nil
}

func newFakeMetaProviders(t *testing.T, fakes ...*fakeMetaProvider) fakeMetaProviders {
	t.Helper()
	providers := fakeMetaProviders{}
	for _, f := range fakes {
		opts := metadata.DefaultProviderWithCacheOptions()
		opts.Provider = f
		p, err := metadata.NewProviderWithCache(opts)
		if err != nil {
			t.Fatal(err)
		}
		providers[f.src.Source] = p
	}
	return providers
}

type bindManga struct {
	mangadata.Manga
	id   string
	meta metadata.Metadata
}

func (m *bindManga) Info() mangadata.MangaInfo {
	return mangadata.MangaInfo{ID: m.id, Title: "One Piece"}
}
func (m *bindManga) Metadata() metadata.Metadata        { return m.meta }
func (m *bindManga) SetMetadata(meta metadata.Metadata) { m.meta = meta }
func (m *bindManga) String() string                     { return m.Info().Title }

type bindVolume struct {
	mangadata.Volume
	manga mangadata.Manga
}

func (v bindVolume) Manga() mangadata.Manga { return v.manga }

type bindChapter struct {
	mangadata.Chapter
	manga  mangadata.Manga
	number float32
}

func (c bindChapter) Info() mangadata.ChapterInfo { return mangadata.ChapterInfo{Number: c.number} }
func (c bindChapter) Volume() mangadata.Volume    { return bindVolume{manga: c.manga} }

func openBindings(t *testing.T) storage.Bindings {
	t.Helper()
	data, err := storage.OpenData(t.TempDir() + "/" + storage.FilenameData)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { data.Close() })
	return storage.Bindings{DB: data}
}

var (
	srcAnilist, _ = MetadataSourceOf(metadata.IDCodeAnilist)
	srcKitsu, _   = MetadataSourceOf(metadata.IDCodeKitsu)
)

// bind binds as core.BindReq does: the metadata checked, then set.
func bind(ctx context.Context, bindings storage.Bindings, c metadataProvider, provider, mangaID string, manga mangadata.Manga, code metadata.IDCode, id string) (metadata.Metadata, error) {
	meta, err := boundMetadata(ctx, c, code, id)
	if err != nil {
		return nil, err
	}
	return meta, SetBound(bindings, provider, mangaID, manga, code, id, meta)
}

func TestApplyBinding(t *testing.T) {
	ctx := context.Background()
	failed.at = map[string]time.Time{}
	bindings := openBindings(t)
	ani := &fakeMetaProvider{src: srcAnilist, byID: map[int]string{30013: "One Piece (AniList)"}}
	kitsu := &fakeMetaProvider{src: srcKitsu, byID: map[int]string{11: "One Piece (Kitsu)"}}
	providers := newFakeMetaProviders(t, ani, kitsu)
	manga := &bindManga{id: "2"}

	if bound, err := applyBinding(ctx, bindings, "mango-mangapill", providers, manga); err != nil || bound || manga.meta != nil {
		t.Fatalf("unbound manga: %v, %v, %v", bound, err, manga.meta)
	}

	// AniList is the default whatever the order of binding, the other sources aren't asked
	if _, err := bind(ctx, bindings, providers, "mango-mangapill", "2", manga, metadata.IDCodeKitsu, "11"); err != nil {
		t.Fatal(err)
	}
	if manga.meta == nil || manga.meta.Title() != "One Piece (Kitsu)" {
		t.Fatalf("bind didn't set the default's metadata: %v", manga.meta)
	}
	if _, err := bind(ctx, bindings, providers, "mango-mangapill", "2", manga, metadata.IDCodeAnilist, "30013"); err != nil {
		t.Fatal(err)
	}
	if manga.meta.Title() != "One Piece (AniList)" {
		t.Fatalf("bind didn't set the new default's metadata: %v", manga.meta)
	}
	manga.meta = nil
	kitsu.ids = nil
	bound, err := applyBinding(ctx, bindings, "mango-mangapill", providers, manga)
	if err != nil || !bound || manga.meta == nil || manga.meta.ID().Code != metadata.IDCodeAnilist || manga.meta.ID().Raw != "30013" {
		t.Fatalf("applied %v, %v, %v", bound, err, manga.meta)
	}
	if len(kitsu.searches)+len(ani.searches)+len(kitsu.ids) != 0 {
		t.Errorf("searched: %v %v %v", kitsu.searches, ani.searches, kitsu.ids)
	}
	// already set: no request (the provider's cache aside)
	ani.ids = nil
	if bound, err := applyBinding(ctx, bindings, "mango-mangapill", providers, manga); err != nil || !bound || len(ani.ids) != 0 {
		t.Errorf("applied again: %v, %v, ids %v", bound, err, ani.ids)
	}

	// an AniList id that isn't found falls back to Kitsu, and Kitsu stays for the next chapters
	if err := bindings.Set(storage.Binding{Provider: "mango-mangapill", MangaID: "2", Source: metadata.IDCodeAnilist, ID: "999999999"}); err != nil {
		t.Fatal(err)
	}
	manga.meta = nil
	bound, err = applyBinding(ctx, bindings, "mango-mangapill", providers, manga)
	if err != nil || !bound || manga.meta == nil || manga.meta.ID().Code != metadata.IDCodeKitsu || manga.meta.ID().Raw != "11" {
		t.Fatalf("fallback %v, %v, %v", bound, err, manga.meta)
	}
	ani.ids, kitsu.ids = nil, nil
	if bound, err := applyBinding(ctx, bindings, "mango-mangapill", providers, manga); err != nil || !bound || len(ani.ids)+len(kitsu.ids) != 0 ||
		manga.meta.ID().Code != metadata.IDCodeKitsu {
		t.Errorf("fallback again: %v, %v, ids %v %v", bound, err, ani.ids, kitsu.ids)
	}

	// a binding that's gone (and no backup) is an error, not a silent search
	if err := bindings.Set(storage.Binding{Provider: "mango-mangapill", MangaID: "3", Source: metadata.IDCodeAnilist, ID: "404"}); err != nil {
		t.Fatal(err)
	}
	if _, err := applyBinding(ctx, bindings, "mango-mangapill", providers, &bindManga{id: "3"}); err == nil {
		t.Error("missing bound manga applied")
	}
	// a source without a provider (MyAnimeList not configured)
	if _, err := bind(ctx, bindings, providers, "p", "m", nil, metadata.IDCodeMyAnimeList, "1"); err == nil {
		t.Error("bound to an unavailable source")
	}
	if _, err := bind(ctx, bindings, providers, "p", "m", nil, metadata.IDCodeAnilist, "abc"); err == nil {
		t.Error("bound to a non-integer id")
	}
}

func TestReadSyncsBoundIDs(t *testing.T) {
	ctx := context.Background()
	bindings := openBindings(t)
	ani := &fakeMetaProvider{src: srcAnilist}
	kitsu := &fakeMetaProvider{src: srcKitsu}
	providers := newFakeMetaProviders(t, ani, kitsu)
	for _, b := range []storage.Binding{
		{Provider: "p", MangaID: "2", Source: metadata.IDCodeKitsu, ID: "11"},
		{Provider: "p", MangaID: "2", Source: metadata.IDCodeAnilist, ID: "30013"},
		{Provider: "p", MangaID: "2", Source: metadata.IDCodeMyAnimeList, ID: "13"},
	} {
		if err := bindings.Set(b); err != nil {
			t.Fatal(err)
		}
	}
	chapter := bindChapter{manga: &bindManga{id: "2"}, number: 12.5}

	// MyAnimeList's sync is off: neither synced here nor left to libmangal
	bound, rest, err := boundSyncs(bindings, "p", chapter, libmangal.ReadOptions{SaveAnilist: true, SaveKitsu: true})
	if err != nil || len(bound) != 2 || rest != (libmangal.ReadOptions{}) {
		t.Fatalf("bound %+v, rest %+v, %v", bound, rest, err)
	}
	if err := syncBound(ctx, providers, chapter, bound); err != nil {
		t.Fatal(err)
	}
	if ani.progress[30013] != 12 || kitsu.progress[11] != 12 || len(ani.progress)+len(kitsu.progress) != 2 {
		t.Errorf("progress anilist %v, kitsu %v", ani.progress, kitsu.progress)
	}

	// an unbound manga is left to libmangal
	other := bindChapter{manga: &bindManga{id: "9"}, number: 1}
	bound, rest, err = boundSyncs(bindings, "p", other, libmangal.ReadOptions{SaveAnilist: true})
	if err != nil || len(bound) != 0 || !rest.SaveAnilist {
		t.Errorf("unbound: %+v, %+v, %v", bound, rest, err)
	}
}

// a rate limited binding (the shared sentinel, no delay) stops: no fallback to the next
func TestFirstBoundRateLimited(t *testing.T) {
	failed.at = map[string]time.Time{}
	ani := &fakeMetaProvider{src: srcAnilist, err: fmt.Errorf("anilist: %w", libmangal.ErrRateLimited)}
	kitsu := &fakeMetaProvider{src: srcKitsu, byID: map[int]string{11: "One Piece (Kitsu)"}}
	bindings := []storage.Binding{{Source: metadata.IDCodeAnilist, ID: "30013"}, {Source: metadata.IDCodeKitsu, ID: "11"}}
	_, err := firstBound(context.Background(), newFakeMetaProviders(t, ani, kitsu), bindings, "One Piece")
	if !errors.Is(err, libmangal.ErrRateLimited) || len(kitsu.ids) != 0 {
		t.Errorf("%v, kitsu asked %v", err, kitsu.ids)
	}
}
