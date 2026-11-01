package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/core/jobs"
)

func TestDownloadReq(t *testing.T) {
	ctx := context.Background()
	manga := &fillManga{mangadata.MangaInfo{ID: "m", Title: "Berserk"}}
	var chapters []mangadata.Chapter
	for i, url := range []string{"https://x/1", "", "https://x/3"} {
		chapters = append(chapters, fillCh{mangadata.ChapterInfo{Number: float32(i + 1), Title: "c", URL: url}, fillVol{manga}})
	}
	cl, err := libmangal.NewClientContext(ctx, infoLoader{libmangal.ProviderInfo{ID: "p", Name: "p", Version: "1.0.0"}}, libmangal.DefaultClientOptions())
	if err != nil {
		t.Fatal(err)
	}
	m := jobs.New(ctx)
	t.Cleanup(func() { m.Shutdown(time.Second) })
	l := localJobs{m: m, opts: JobsOptions{Find: func(_ context.Context, provider, id, query, _ string) (*libmangal.Client, mangadata.Manga, []mangadata.Chapter, func(), error) {
		return cl, manga, chapters, func() {}, nil
	}}}

	for _, req := range []DownloadReq{
		{Provider: "p", Chapters: "1"},
		{Provider: "p", MangaID: "m"},
	} {
		if _, err := l.downloadSpec(ctx, req); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", req, err)
		}
	}
	for _, tc := range []struct {
		req  DownloadReq
		want int
	}{
		{DownloadReq{Provider: "p", MangaID: "m", Chapters: "2-"}, 2},
		{DownloadReq{Provider: "p", MangaID: "m", ChapterRefs: []ChapterRef{ChapterRefOf(chapters[1]), ChapterRefOf(chapters[2]), ChapterRefOf(chapters[2])}}, 2},
		{DownloadReq{Provider: "p", MangaID: "m", ChapterRefs: []ChapterRef{{Number: 9}}}, -1},
		{DownloadReq{Provider: "p", MangaID: "m", Chapters: "9"}, -1},
	} {
		have, err := l.resolve(ctx, tc.req)
		switch {
		case tc.want < 0 && !errors.Is(err, ErrInvalid):
			t.Errorf("%+v: %v", tc.req, err)
		case tc.want >= 0 && (err != nil || len(have.Chapters) != tc.want):
			t.Errorf("%+v: %v %v", tc.req, have, err)
		}
	}

	spec, err := l.downloadSpec(ctx, DownloadReq{Provider: "p", Origin: "o", Have: &DownloadHave{cl, manga, chapters[:1]}})
	if err != nil || spec.Title != "Berserk: chapter 1" || spec.Origin != "o" || spec.MangaID != "m" {
		t.Fatalf("spec %+v: %v", spec, err)
	}
	j := JobOf(m.Add(jobs.Spec{Kind: jobs.KindDownload, Providers: []string{"p"}, MangaID: "m", Chapters: chapters[:1], Run: func(ctx context.Context, _ *jobs.Progress) error {
		<-ctx.Done()
		return nil
	}}))
	if _, ok := l.ChapterStatus("p", "m", ChapterRefOf(chapters[0])); !ok {
		t.Error("no status of the chapter in the job")
	}
	if _, ok := l.ChapterStatus("p", "m", ChapterRefOf(chapters[1])); ok {
		t.Error("status of a chapter not in the job")
	}
	sub, cancel := context.WithCancel(ctx)
	events, _ := l.Subscribe(sub, 0)
	if e := <-events; e.Kind != JobAdded || e.Job.ID != j.ID {
		t.Errorf("event %+v", e)
	}
	l.Cancel(ctx, j.ID)
	for e := range events {
		if e.Kind == JobFinished {
			break
		}
	}
	cancel()
	for range events {
	}
}
