package core

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
)

type (
	infoLoader   struct{ info libmangal.ProviderInfo }
	infoProvider struct {
		libmangal.Provider
		info libmangal.ProviderInfo
	}
	fillManga struct{ info mangadata.MangaInfo }
	fillVol   struct{ manga *fillManga }
	fillCh    struct {
		info mangadata.ChapterInfo
		vol  fillVol
	}
)

func (l infoLoader) String() string               { return l.info.ID }
func (l infoLoader) Info() libmangal.ProviderInfo { return l.info }
func (l infoLoader) Load(context.Context) (libmangal.Provider, error) {
	return infoProvider{info: l.info}, nil
}
func (p infoProvider) Info() libmangal.ProviderInfo { return p.info }
func (p infoProvider) SetLogger(*logger.Logger)     {}
func (m *fillManga) String() string                 { return m.info.Title }
func (m *fillManga) Info() mangadata.MangaInfo      { return m.info }
func (m *fillManga) Metadata() metadata.Metadata    { return nil }
func (m *fillManga) SetMetadata(metadata.Metadata)  {}
func (v fillVol) String() string                    { return "2" }
func (v fillVol) Info() mangadata.VolumeInfo        { return mangadata.VolumeInfo{Number: 2} }
func (v fillVol) Manga() mangadata.Manga            { return v.manga }
func (c fillCh) String() string                     { return c.info.Title }
func (c fillCh) Info() mangadata.ChapterInfo        { return c.info }
func (c fillCh) Volume() mangadata.Volume           { return c.vol }

func TestPlanFill(t *testing.T) {
	c, root := testCore(t)
	ctx := context.Background()
	md := &fillManga{mangadata.MangaInfo{ID: "md", Title: "Dark Gathering"}}
	mp := &fillManga{mangadata.MangaInfo{ID: "mp", Title: "Dark Gathering"}}
	ch := func(m *fillManga, n float32, title, url, unavailable string) mangadata.Chapter {
		return fillCh{mangadata.ChapterInfo{Number: n, Title: title, URL: url, Unavailable: unavailable}, fillVol{m}}
	}
	providers := map[string]struct {
		manga    *fillManga
		chapters []mangadata.Chapter
	}{
		"mango-mangadex": {md, []mangadata.Chapter{
			ch(md, 1, "Lie Low", "", "retired"), ch(md, 2, "Chapter 2", "", "retired"), ch(md, 3, "Third", "", "retired"),
			ch(md, 4, "Fourth", "", "retired"), ch(md, 5, "Fifth", "", ""),
			// another group has 6
			ch(md, 6, "Sixth", "", "external"), ch(md, 6, "Sixth", "", ""),
		}},
		"mango-mangapill": {mp, []mangadata.Chapter{
			ch(mp, 1, "Chapter 1", "https://mangapill.com/1", ""), ch(mp, 2, "Second", "https://mangapill.com/2", ""),
			ch(mp, 3, "Chapter 3", "https://mangapill.com/3", "0 pages"),
		}},
	}
	env := c.env
	env.ProviderChapters = func(ctx context.Context, provider, id, query, _ string) (*libmangal.Client, mangadata.Manga, []mangadata.Chapter, func(), error) {
		p, ok := providers[provider]
		if !ok {
			return nil, nil, nil, nil, errors.New("no provider")
		}
		if id != "" && id != p.manga.info.ID || id == "" && query != "Dark Gathering" {
			t.Errorf("%s: id %q, query %q", provider, id, query)
		}
		cl, err := libmangal.NewClientContext(ctx, infoLoader{libmangal.ProviderInfo{ID: provider, Name: provider + " name", Version: "1.0.0"}}, libmangal.DefaultClientOptions())
		return cl, p.manga, p.chapters, func() {}, err
	}
	c, _ = New(ctx, env)
	folder := filepath.Join(root, "Dark Gathering")
	addManga(t, c, "Dark Gathering", "mango-mangadex", "md", "Dark Gathering", 4)

	req := FillReq{Provider: "mango-mangadex", MangaID: "md", From: RetitleFrom{Provider: "mango-mangapill"}}
	plan, err := c.PlanFill(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Folder != folder || plan.From.ID != "mango-mangapill" || plan.FromManga.ID != "mp" {
		t.Errorf("plan %+v", plan)
	}
	want := []FillChapter{{1, "Lie Low", "Chapter 1", "https://mangapill.com/1"}, {2, "Second", "Second", "https://mangapill.com/2"}}
	if len(plan.Found) != 2 || plan.Found[0] != want[0] || plan.Found[1] != want[1] {
		t.Errorf("found %+v", plan.Found)
	}
	if len(plan.NotFound) != 1 || plan.NotFound[0].Number != 3 || len(plan.Downloaded) != 1 || plan.Downloaded[0].Number != 4 {
		t.Errorf("not found %+v, downloaded %+v", plan.NotFound, plan.Downloaded)
	}

	job := plan.Job("test")
	if job.Title != "Dark Gathering: chapters 1-2 (2) from mango-mangapill name" || job.MangaID != "mp" || len(job.Providers) != 1 || job.Providers[0] != "mango-mangapill" || len(job.Chapters) != 2 {
		t.Errorf("job %+v", job)
	}
	got := job.Chapters[0]
	if got.Info().Title != "Lie Low" || got.Info().URL != "https://mangapill.com/1" || got.Volume().Info().Number != 2 || got.Volume().Manga().Info().ID != "md" {
		t.Errorf("chapter %+v of %+v", got.Info(), got.Volume().Manga().Info())
	}
	if w, ok := got.(mangadata.WrappedChapter); !ok || w.Unwrap().Volume().Manga().Info().ID != "mp" {
		t.Errorf("chapter %T doesn't unwrap to mangapill's", got)
	}

	req.Chapters = "2-3"
	if plan, err = c.PlanFill(ctx, req); err != nil || len(plan.Found) != 1 || plan.Found[0].Number != 2 || len(plan.NotFound) != 1 {
		t.Errorf("2-3: %+v %v", plan, err)
	}
	req.From.Provider = "mango-mangadex"
	if _, err := c.PlanFill(ctx, req); !errors.Is(err, ErrInvalid) {
		t.Errorf("same provider: %v", err)
	}
}
