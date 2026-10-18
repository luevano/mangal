package client

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
)

type chapter struct {
	mangadata.Chapter
	number      float32
	unavailable string
	// manga is the manga's id, "m" when empty
	manga string
}

type volume struct {
	mangadata.Volume
	manga string
}

type manga struct {
	mangadata.Manga
	id string
}

func (c chapter) Volume() mangadata.Volume {
	return volume{manga: cmp.Or(c.manga, "m")}
}

func (v volume) Manga() mangadata.Manga { return manga{id: v.manga} }

func (m manga) Info() mangadata.MangaInfo {
	return mangadata.MangaInfo{ID: m.id, Title: "Manga " + m.id}
}

// noLock is a lockFunc that doesn't lock.
func noLock(ctx context.Context, _ string, _ func(ProviderBusy)) (context.Context, func(), error) {
	return ctx, func() {}, nil
}

func (c chapter) Info() mangadata.ChapterInfo {
	return mangadata.ChapterInfo{Number: c.number, Unavailable: c.unavailable}
}

type rateLimitError struct{ wait time.Duration }

func (e rateLimitError) Error() string                     { return "429" }
func (e rateLimitError) RetryAfterDuration() time.Duration { return e.wait }

func TestDownloadRetry(t *testing.T) {
	chapters := []mangadata.Chapter{chapter{number: 1}, chapter{number: 2, unavailable: "external"}, chapter{number: 3}, chapter{number: 4}}
	// chapter 1: 2 rate limits then ok; 3: always rate limited; 4: fails
	limited := map[float32]int{1: 2, 3: 100}
	get := func(_ context.Context, ch mangadata.Chapter) (*metadata.DownloadedChapter, error) {
		n := ch.Info().Number
		if n == 2 {
			t.Error("unavailable chapter tried")
		}
		if limited[n] > 0 {
			limited[n]--
			return nil, rateLimitError{wait: time.Millisecond}
		}
		if n == 4 {
			return nil, errors.New("broken")
		}
		return &metadata.DownloadedChapter{}, nil
	}

	var waits []int
	var done int
	// waits are floored at 1s, too slow for many: make them instant
	results := withInstantWaits(t, func() Results {
		return download(context.Background(), "p", chapters, func(e Event) {
			switch e.Kind {
			case EventWait:
				waits = append(waits, e.Attempt)
			case EventDone:
				done++
			}
		}, noLock, nil, get)
	})

	if !results[0].Succeed() {
		t.Errorf("chapter 1: %+v", results[0])
	}
	if results[1].Unavailable != "external" || results[1].Failed() || results[1].ToDownload() {
		t.Errorf("chapter 2: %+v", results[1])
	}
	// the retries reset per chapter: 3 gets its own 10
	if !results[2].Failed() || !errors.As(results[2].Err, new(rateLimitError)) {
		t.Errorf("chapter 3: %+v", results[2])
	}
	if !results[3].Failed() {
		t.Errorf("chapter 4: %+v", results[3])
	}
	if len(waits) != 2+maxRetries || waits[2] != 1 {
		t.Errorf("waits = %v", waits)
	}
	if done != 4 {
		t.Errorf("done events = %d", done)
	}
	if len(results.Failed()) != 2 || len(results.Unavailable()) != 1 {
		t.Errorf("failed %d, unavailable %d", len(results.Failed()), len(results.Unavailable()))
	}
}

func TestDownloadCancelledWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	chapters := []mangadata.Chapter{chapter{number: 1}, chapter{number: 2}}
	get := func(context.Context, mangadata.Chapter) (*metadata.DownloadedChapter, error) {
		return nil, rateLimitError{wait: time.Hour}
	}
	start := time.Now()
	results := download(ctx, "p", chapters, func(e Event) {
		if e.Kind == EventWait {
			cancel()
		}
	}, noLock, nil, get)
	if time.Since(start) > 5*time.Second {
		t.Error("the wait ignored the cancel")
	}
	// cancelled chapters aren't failed, they're still to download
	if len(results.ToDownload()) != 2 || len(results.Failed()) != 0 {
		t.Errorf("to download %d, failed %d", len(results.ToDownload()), len(results.Failed()))
	}
}

func withInstantWaits(t *testing.T, f func() Results) Results {
	t.Helper()
	old := newTimer
	newTimer = func(time.Duration) *time.Timer { return time.NewTimer(0) }
	defer func() { newTimer = old }()
	return f()
}

// a rate limit without a delay (mangoplus' 10522 device lockout) isn't retried, and stops
// the download: retrying can't work and may keep the lockout
func TestDownloadNoDelay(t *testing.T) {
	chapters := []mangadata.Chapter{chapter{number: 1}, chapter{number: 2}, chapter{number: 3}}
	tries := map[float32]int{}
	get := func(_ context.Context, ch mangadata.Chapter) (*metadata.DownloadedChapter, error) {
		n := ch.Info().Number
		tries[n]++
		if n == 2 {
			return nil, fmt.Errorf("mangoplus: 10522: %w", libmangal.ErrRateLimited)
		}
		return &metadata.DownloadedChapter{}, nil
	}
	waits := 0
	results := withInstantWaits(t, func() Results {
		return download(context.Background(), "p", chapters, func(e Event) {
			if e.Kind == EventWait {
				waits++
			}
		}, noLock, nil, get)
	})
	if waits != 0 || tries[2] != 1 {
		t.Errorf("retried: %d waits, %d tries", waits, tries[2])
	}
	if !results[0].Succeed() || !results[1].Failed() || !errors.Is(results[1].Err, libmangal.ErrRateLimited) {
		t.Errorf("results %+v %+v", results[0], results[1])
	}
	if tries[3] != 0 || !results[2].ToDownload() {
		t.Errorf("chapter 3 tried %d times: %+v", tries[3], results[2])
	}
}
