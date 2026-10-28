package remote

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// Loaders are the server's providers as libmangal loaders: their clients browse through
// the server (its search, chapters and pages endpoints, with its rate limits and
// logins). Downloads don't go through them (Jobs.Download).
func (c *Client) Loaders(ctx context.Context) ([]libmangal.ProviderLoader, error) {
	r, err := c.API.ListProvidersWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	loaders := make([]libmangal.ProviderLoader, len(list))
	for i, p := range list {
		loaders[i] = Loader{c: c, info: providerOf(p), preferred: deref(p.Preferred)}
	}
	return loaders, nil
}

// Loader is a provider of the server.
type Loader struct {
	c         *Client
	info      libmangal.ProviderInfo
	preferred string
}

// Preferred is the website the server's provider uses first (its providers.mirrors).
func (l Loader) Preferred() string { return l.preferred }

// SetMirror makes the server's provider use the mirror first (its website: "").
func (c *Client) SetMirror(ctx context.Context, provider, mirror string) error {
	r, err := c.API.SetProviderMirrorWithResponse(ctx, provider, apiv1.MirrorRequest{Mirror: mirror})
	if err != nil {
		return fmt.Errorf("mangal serve: %w", err)
	}
	_, err = answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	return err
}

var _ libmangal.ProviderLoader = Loader{}

func (l Loader) String() string               { return l.info.Name }
func (l Loader) Info() libmangal.ProviderInfo { return l.info }

func (l Loader) Load(context.Context) (libmangal.Provider, error) {
	return &provider{c: l.c, info: l.info, chapters: map[string][]apiv1.ProviderChapter{}}, nil
}

// provider asks the server; a manga's chapters are listed once (its volumes come from
// them) until it's searched again.
type provider struct {
	c    *Client
	info libmangal.ProviderInfo

	mu       sync.Mutex
	chapters map[string][]apiv1.ProviderChapter
}

var (
	_ libmangal.Provider          = (*provider)(nil)
	_ libmangal.MangaByIDProvider = (*provider)(nil)
)

func (p *provider) String() string               { return p.info.Name }
func (p *provider) Info() libmangal.ProviderInfo { return p.info }
func (p *provider) SetLogger(*logger.Logger)     {}
func (p *provider) Close() error                 { return nil }

func (p *provider) search(ctx context.Context, params apiv1.SearchProviderParams) ([]mangadata.Manga, error) {
	r, err := p.c.API.SearchProviderWithResponse(ctx, p.info.ID, &params)
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	clear(p.chapters)
	p.mu.Unlock()
	out := make([]mangadata.Manga, len(list))
	for i, m := range list {
		out[i] = newManga(m)
	}
	return out, nil
}

func (p *provider) SearchMangas(ctx context.Context, query string) ([]mangadata.Manga, error) {
	return p.search(ctx, apiv1.SearchProviderParams{Q: &query})
}

func (p *provider) MangaByID(ctx context.Context, id string) (mangadata.Manga, bool, error) {
	list, err := p.search(ctx, apiv1.SearchProviderParams{Id: &id})
	if err != nil || len(list) == 0 {
		return nil, false, err
	}
	return list[0], true, nil
}

func (p *provider) list(ctx context.Context, m *manga) ([]apiv1.ProviderChapter, error) {
	p.mu.Lock()
	list, ok := p.chapters[m.info.ID]
	p.mu.Unlock()
	if ok {
		return list, nil
	}
	r, err := p.c.API.ListProviderChaptersWithResponse(ctx, p.info.ID, &apiv1.ListProviderChaptersParams{MangaId: m.info.ID})
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	if list, err = answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault); err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.chapters[m.info.ID] = list
	p.mu.Unlock()
	return list, nil
}

func (p *provider) MangaVolumes(ctx context.Context, m mangadata.Manga) ([]mangadata.Volume, error) {
	mg, ok := m.(*manga)
	if !ok {
		return nil, fmt.Errorf("manga %q isn't the server's", m)
	}
	list, err := p.list(ctx, mg)
	if err != nil {
		return nil, err
	}
	var numbers []float32
	for _, ch := range list {
		if !slices.Contains(numbers, ch.Volume) {
			numbers = append(numbers, ch.Volume)
		}
	}
	slices.Sort(numbers)
	out := make([]mangadata.Volume, len(numbers))
	for i, n := range numbers {
		out[i] = &volume{manga: mg, info: mangadata.VolumeInfo{Number: n}}
	}
	return out, nil
}

func (p *provider) VolumeChapters(ctx context.Context, v mangadata.Volume) ([]mangadata.Chapter, error) {
	vol, ok := v.(*volume)
	if !ok {
		return nil, fmt.Errorf("volume %q isn't the server's", v)
	}
	list, err := p.list(ctx, vol.manga)
	if err != nil {
		return nil, err
	}
	var out []mangadata.Chapter
	for _, ch := range list {
		if ch.Volume == vol.info.Number {
			out = append(out, &chapter{volume: vol, info: mangadata.ChapterInfo{Title: ch.Title, URL: deref(ch.Url), Number: ch.Number,
				Unavailable: deref(ch.Unavailable)}})
		}
	}
	return out, nil
}

func (p *provider) ChapterPages(ctx context.Context, c mangadata.Chapter) ([]mangadata.Page, error) {
	ch, ok := c.(*chapter)
	if !ok {
		return nil, fmt.Errorf("chapter %q isn't the server's", c)
	}
	vol := ch.volume.info.Number
	r, err := p.c.API.ListProviderPagesWithResponse(ctx, p.info.ID, &apiv1.ListProviderPagesParams{MangaId: ch.volume.manga.info.ID,
		Chapter: ch.info.Number, Volume: &vol})
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	list, err := answer(r.StatusCode(), r.Body, r.JSON200, r.JSONDefault)
	if err != nil {
		return nil, err
	}
	out := make([]mangadata.Page, len(list.Pages))
	for i, pg := range list.Pages {
		out[i] = &page{chapter: ch, index: pg.Index, ext: path.Ext(deref(pg.Name))}
	}
	return out, nil
}

func (p *provider) GetPageImage(ctx context.Context, pg mangadata.Page) ([]byte, error) {
	page, ok := pg.(*page)
	if !ok {
		return nil, fmt.Errorf("page %q isn't the server's", pg)
	}
	ch := page.chapter
	vol := ch.volume.info.Number
	r, err := p.c.API.GetProviderPageWithResponse(ctx, p.info.ID, page.index, &apiv1.GetProviderPageParams{
		MangaId: ch.volume.manga.info.ID, Chapter: ch.info.Number, Volume: &vol})
	if err != nil {
		return nil, fmt.Errorf("mangal serve: %w", err)
	}
	if r.StatusCode() != 200 {
		return nil, errorOf(r.StatusCode(), r.Body, r.JSONDefault)
	}
	return r.Body, nil
}

type manga struct {
	info mangadata.MangaInfo
	meta lmmeta.Metadata
}

func newManga(m apiv1.Manga) *manga {
	info := mangadata.MangaInfo{ID: m.Id, Title: m.Title, URL: deref(m.Url), Cover: deref(m.Cover), Banner: deref(m.Banner)}
	// the provider's metadata isn't on the API: what the search says
	return &manga{info: info, meta: &mangadata.Metadata{EnglishTitle: info.Title, CoverImage: info.Cover, BannerImage: info.Banner,
		SourceURL: info.URL}}
}

func (m *manga) String() string                   { return m.info.Title }
func (m *manga) Info() mangadata.MangaInfo        { return m.info }
func (m *manga) Metadata() lmmeta.Metadata        { return m.meta }
func (m *manga) SetMetadata(meta lmmeta.Metadata) { m.meta = meta }

type volume struct {
	manga *manga
	info  mangadata.VolumeInfo
}

func (v *volume) String() string             { return fmt.Sprintf("%g", v.info.Number) }
func (v *volume) Info() mangadata.VolumeInfo { return v.info }
func (v *volume) Manga() mangadata.Manga     { return v.manga }

type chapter struct {
	volume *volume
	info   mangadata.ChapterInfo
}

func (c *chapter) String() string              { return c.info.Title }
func (c *chapter) Info() mangadata.ChapterInfo { return c.info }
func (c *chapter) Volume() mangadata.Volume    { return c.volume }

type page struct {
	chapter *chapter
	index   int
	ext     string
}

func (p *page) String() string             { return fmt.Sprintf("page %d", p.index+1) }
func (p *page) Extension() string          { return strings.ToLower(p.ext) }
func (p *page) Chapter() mangadata.Chapter { return p.chapter }
