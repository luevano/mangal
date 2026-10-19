package serve

import (
	"context"
	"net/http"
	"testing"

	"github.com/luevano/libmangal"
	lmlogger "github.com/luevano/libmangal/logger"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/web/apiv1"
)

type idLoader string

func (l idLoader) String() string { return string(l) }
func (l idLoader) Info() libmangal.ProviderInfo {
	return libmangal.ProviderInfo{ID: string(l), Name: string(l), Version: "1.0.0"}
}
func (l idLoader) Load(context.Context) (libmangal.Provider, error) {
	return idProvider{info: l.Info()}, nil
}

type idProvider struct {
	libmangal.Provider
	info libmangal.ProviderInfo
}

func (p idProvider) Info() libmangal.ProviderInfo { return p.info }
func (p idProvider) SetLogger(*lmlogger.Logger)   {}

// TestV1Fill: the plan matches the unavailable chapters the folder doesn't have, apply
// queues the download job of the other provider.
func TestV1Fill(t *testing.T) {
	s, _, _ := newHealthServer(t)
	newCore := s.api.Core
	s.api.Core = func(m watch.Manager) (*core.Core, error) {
		c, err := newCore(m)
		if err != nil {
			return nil, err
		}
		env := c.Env()
		env.Loaders = func() ([]libmangal.ProviderLoader, error) {
			return []libmangal.ProviderLoader{idLoader("fake"), idLoader("other")}, nil
		}
		env.ProviderChapters = func(ctx context.Context, provider, id, query, _ string) (*libmangal.Client, mangadata.Manga, []mangadata.Chapter, func(), error) {
			manga := &fakeManga{info: mangadata.MangaInfo{ID: "opm", Title: "One-Punch Man"}}
			infos := []mangadata.ChapterInfo{{Number: 1}, {Number: 2, Unavailable: "retired"}, {Number: 3, Title: "Three", Unavailable: "retired"}, {Number: 4, Unavailable: "retired"}}
			if provider == "other" {
				manga.info.ID = "o"
				infos = []mangadata.ChapterInfo{{Number: 3, Title: "Chapter 3", URL: "https://other/3"}}
			}
			var chapters []mangadata.Chapter
			for _, info := range infos {
				chapters = append(chapters, &fakeChapter{info: info, vol: &fakeVolume{manga: manga}})
			}
			cl, err := libmangal.NewClientContext(ctx, idLoader(provider), libmangal.DefaultClientOptions())
			return cl, manga, chapters, func() {}, err
		}
		return core.New(context.Background(), env)
	}
	s.api.Jobs = jobs.New(context.Background())
	// queued jobs don't run: nothing downloads
	s.api.Jobs.Shutdown(0)

	var plan apiv1.FillPlan
	body := `{"manga_id":"opm","from":"other"}`
	if code := s.call(t, "POST", "/api/v1/providers/fake/fill", body, &plan); code != http.StatusOK || plan.Job != nil {
		t.Fatalf("plan %d %+v", code, plan)
	}
	if plan.Folder != "/lib/One-Punch Man" || len(plan.Found) != 1 || plan.Found[0].Title != "Three" || *plan.Found[0].Url != "https://other/3" ||
		len(plan.Downloaded) != 2 || len(plan.NotFound) != 0 || *plan.From != "other" {
		t.Errorf("plan %+v", plan)
	}
	if code := s.call(t, "POST", "/api/v1/providers/fake/fill?apply=1", body, &plan); code != http.StatusOK || plan.Job == nil || plan.Job.Title != "One-Punch Man: chapter 3 from other" {
		t.Fatalf("apply %d %+v", code, plan)
	}

	var e ErrorResponse
	if code := s.call(t, "POST", "/api/v1/providers/fake/fill", `{"manga_id":"opm","from":"nope"}`, &e); code != http.StatusNotFound {
		t.Errorf("missing provider: %d %+v", code, e)
	}
	if code := s.call(t, "POST", "/api/v1/providers/fake/fill", `{"manga_id":"opm","from":"fake"}`, &e); code != http.StatusBadRequest {
		t.Errorf("same provider: %d %+v", code, e)
	}
}
