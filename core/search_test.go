package core

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
)

func TestSearch(t *testing.T) {
	c, _ := testCore(t)
	slow := make(chan struct{})
	var running, most atomic.Int32
	c.env.SearchProvider = func(ctx context.Context, provider, query string) (libmangal.ProviderInfo, []mangadata.Manga, error) {
		n := running.Add(1)
		defer running.Add(-1)
		for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
		}
		info := libmangal.ProviderInfo{ID: provider, Name: provider + " name"}
		switch provider {
		case "slow":
			select {
			case <-slow:
			case <-ctx.Done():
				return info, nil, ctx.Err()
			}
		case "failing":
			return info, nil, errors.New("down")
		}
		return info, []mangadata.Manga{&fillManga{mangadata.MangaInfo{ID: provider + "-1", Title: query}}}, nil
	}

	// the fast and failing ones come before the slow one answers
	results := c.Search(context.Background(), SearchReq{Providers: []string{"slow", "fast", "failing"}, Query: "Dark Gathering"})
	var got []string
	for range 2 {
		r := <-results
		got = append(got, r.Provider)
		switch r.Provider {
		case "fast":
			if r.Err != nil || len(r.Mangas) != 1 || r.Mangas[0].Info().Title != "Dark Gathering" || r.Info.Name != "fast name" {
				t.Errorf("fast: %+v", r)
			}
		case "failing":
			if r.Err == nil {
				t.Error("failing: no error")
			}
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"failing", "fast"}) {
		t.Errorf("first results %q", got)
	}
	close(slow)
	if r := <-results; r.Provider != "slow" || r.Err != nil {
		t.Errorf("slow: %+v", r)
	}
	if _, ok := <-results; ok {
		t.Error("not closed")
	}

	// bounded
	most.Store(0)
	slow = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	results = c.Search(ctx, SearchReq{Providers: []string{"slow", "slow", "slow", "slow", "slow"}, Concurrency: 2})
	deadline := time.Now().Add(time.Second)
	for running.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if m := most.Load(); m != 2 {
		t.Errorf("%d at once, want 2", m)
	}
	// cancelled: the running ones and the waiting ones end
	cancel()
	n := 0
	for r := range results {
		n++
		if !errors.Is(r.Err, context.Canceled) {
			t.Errorf("cancelled: %+v", r)
		}
	}
	if n != 5 {
		t.Errorf("%d results, want 5", n)
	}
}
