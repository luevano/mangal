package serve

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/web/apiv1"
)

// TestV1SourceURL: a source's url is looked up and kept, a gone one is a 404 marked on
// the source, the bulk lookup is a job.
func TestV1SourceURL(t *testing.T) {
	s, _, _ := newHealthServer(t)
	inner := s.api.Core
	s.api.Core = func(m watch.Manager) (*core.Core, error) {
		c, err := inner(m)
		if err != nil {
			return nil, err
		}
		env := c.Env()
		env.ProviderManga = func(_ context.Context, provider, id, _ string) (libmangal.ProviderInfo, mangadata.MangaInfo, bool, error) {
			return libmangal.ProviderInfo{ID: provider, Name: "Fake"}, mangadata.MangaInfo{ID: id, URL: "https://fake.example/" + id}, id != "gone", nil
		}
		env.Sleep = func(context.Context, time.Duration) error { return nil }
		return core.New(context.Background(), env)
	}
	if err := s.src.library.RecordDownload("/lib", "Gone", storage.Download{Path: "/lib/Gone/1.cbz", Provider: "fake", MangaID: "gone", MangaTitle: "Gone", Chapter: 1, Format: "CBZ"}); err != nil {
		t.Fatal(err)
	}
	gone, _, _ := s.src.library.MangaBySource("fake", "gone")

	var got apiv1.SourceURL
	if code := s.call(t, "GET", "/api/v1/library/mangas/1/source-url?provider=fake&manga_id=opm", "", &got); code != http.StatusOK || got.Url != "https://fake.example/opm" {
		t.Errorf("url %d %+v", code, got)
	}
	var detail apiv1.LibraryMangaDetail
	if s.call(t, "GET", "/api/v1/library/mangas/1", "", &detail); detail.Manga.Sources[0].Url == nil || *detail.Manga.Sources[0].Url != got.Url {
		t.Errorf("not kept %+v", detail.Manga.Sources)
	}
	var e ErrorResponse
	if code := s.call(t, "GET", fmt.Sprintf("/api/v1/library/mangas/%d/source-url?provider=fake&manga_id=gone&refresh=true", gone.ID), "", &e); code != http.StatusNotFound || !strings.Contains(e.Error, "isn't on Fake any more") {
		t.Errorf("gone %d %+v", code, e)
	}
	if s.call(t, "GET", fmt.Sprintf("/api/v1/library/mangas/%d", gone.ID), "", &detail); detail.Manga.Sources[0].Gone == nil {
		t.Errorf("not marked gone %+v", detail.Manga.Sources)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s.api.Jobs = jobs.New(ctx)
	var job apiv1.Job
	if code := s.call(t, "POST", "/api/v1/library/urls", `{"all":true}`, &job); code != http.StatusAccepted || job.Kind != "urls" {
		t.Fatalf("lookup %d %+v", code, job)
	}
	deadline := time.Now().Add(5 * time.Second)
	for job.Status != "done" && job.Status != "failed" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		s.call(t, "GET", fmt.Sprintf("/api/v1/jobs/%d", job.Id), "", &job)
	}
	if job.Status != "done" || job.Message == nil || *job.Message != "1 found, 1 gone, 0 failed" || job.Done == nil || *job.Done != 2 {
		t.Errorf("job %+v", job)
	}
}
