package client

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
)

const (
	// maxRetries is how many times a rate limited chapter is tried again.
	maxRetries = 10
	// maxWait caps how long a rate limit is waited out.
	maxWait = 10 * time.Second
)

// RateLimited is implemented by provider errors that say how long to wait,
// e.g. mangodex's RateLimitError.
type RateLimited interface {
	RetryAfterDuration() time.Duration
}

// RetryAfter returns how long to wait if err (or one it wraps) is RateLimited.
func RetryAfter(err error) (time.Duration, bool) {
	var rateLimit RateLimited
	if errors.As(err, &rateLimit) {
		return rateLimit.RetryAfterDuration(), true
	}
	return 0, false
}

// EventKind is what happened to a chapter of Download.
type EventKind int

const (
	// EventStart is sent before the chapter is downloaded.
	EventStart EventKind = iota
	// EventWait is a rate limit being waited out before trying again.
	EventWait
	// EventDone is sent once the chapter is done: downloaded, failed or unavailable.
	EventDone
	// EventBusy is the provider locked by someone else (a watch check, another download):
	// the download waits for it. Busy is set.
	EventBusy
)

// Event is the progress of Download.
type Event struct {
	Kind EventKind
	// Index is the chapter's index in the results.
	Index  int
	Result *Result
	// Attempt (from 1) and Wait are set for EventWait.
	Attempt int
	Wait    time.Duration
	Busy    *ProviderBusy
}

// Download downloads the chapters one by one (see DownloadChapter), skipping the
// unavailable ones. onEvent may be nil. A cancelled ctx stops it: the rest of the
// chapters stay to download.
//
// The provider's lock (ProviderLockName) is held for the whole download, waiting
// (EventBusy) while a watch check or another download has it.
func Download(ctx context.Context, client *libmangal.Client, chapters []mangadata.Chapter, options libmangal.DownloadOptions, onEvent func(Event)) Results {
	return download(ctx, client.Info().ID, chapters, onEvent, lockStored, func(ctx context.Context, ch mangadata.Chapter) (*metadata.DownloadedChapter, error) {
		return downloadChapter(ctx, client, ch, options)
	})
}

type lockFunc func(ctx context.Context, provider string, onWait func(ProviderBusy)) (context.Context, func(), error)

func lockStored(ctx context.Context, provider string, onWait func(ProviderBusy)) (context.Context, func(), error) {
	store, err := providerLockStore()
	if err != nil {
		return ctx, nil, err
	}
	return LockProvider(ctx, store, provider, LockForDownload, onWait)
}

func download(ctx context.Context, provider string, chapters []mangadata.Chapter, onEvent func(Event), lock lockFunc, get getFunc) Results {
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	results := NewResults(chapters)
	// taken before the first chapter to download, so a batch of unavailable ones doesn't wait
	lockedCtx, locked := ctx, false
	for i, r := range results {
		if ctx.Err() != nil {
			break
		}
		if reason := r.Chapter.Info().Unavailable; reason != "" {
			r.Unavailable = reason
			onEvent(Event{Kind: EventDone, Index: i, Result: r})
			continue
		}
		if !locked {
			c, unlock, err := lock(ctx, provider, func(b ProviderBusy) {
				onEvent(Event{Kind: EventBusy, Index: i, Result: r, Busy: &b})
			})
			if err != nil {
				if ctx.Err() == nil {
					r.Err = fmt.Errorf("lock provider: %w", err)
					onEvent(Event{Kind: EventDone, Index: i, Result: r})
				}
				break
			}
			defer unlock()
			lockedCtx, locked = c, true
		}
		onEvent(Event{Kind: EventStart, Index: i, Result: r})
		r.Down, r.Err = retry(lockedCtx, r.Chapter, get, func(attempt int, wait time.Duration) {
			onEvent(Event{Kind: EventWait, Index: i, Result: r, Attempt: attempt, Wait: wait})
		})
		if r.Err != nil && ctx.Err() != nil {
			// cancelled, not failed
			r.Err = nil
			break
		}
		onEvent(Event{Kind: EventDone, Index: i, Result: r})
	}
	return results
}

// DownloadChapter downloads the chapter and records it in the library, unless it went to
// the temp dir (read without download_on_read). The manga's default metadata binding (if
// any, see WithoutBinding) is its metadata. A chapter that's already there (maybe
// from another provider, see DownloadedChapter.Skipped) isn't downloaded again. A rate
// limit is waited out up to 10 times (EventWait), the provider's lock too (EventBusy, see
// Download); onEvent may be nil.
func DownloadChapter(ctx context.Context, client *libmangal.Client, chapter mangadata.Chapter, options libmangal.DownloadOptions, onEvent func(Event)) (*metadata.DownloadedChapter, error) {
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	r := &Result{Chapter: chapter}
	ctx, unlock, err := lockStored(ctx, client.Info().ID, func(b ProviderBusy) {
		onEvent(Event{Kind: EventBusy, Result: r, Busy: &b})
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("lock provider: %w", err)
	}
	defer unlock()
	return retry(ctx, chapter, func(ctx context.Context, ch mangadata.Chapter) (*metadata.DownloadedChapter, error) {
		return downloadChapter(ctx, client, ch, options)
	}, func(attempt int, wait time.Duration) {
		onEvent(Event{Kind: EventWait, Result: r, Attempt: attempt, Wait: wait})
	})
}

// newTimer is swapped in tests
var newTimer = time.NewTimer

type getFunc func(context.Context, mangadata.Chapter) (*metadata.DownloadedChapter, error)

func retry(ctx context.Context, chapter mangadata.Chapter, get getFunc, onWait func(attempt int, wait time.Duration)) (*metadata.DownloadedChapter, error) {
	for attempt := 1; ; attempt++ {
		down, err := get(ctx, chapter)
		wait, limited := RetryAfter(err)
		if !limited || ctx.Err() != nil {
			return down, err
		}
		if attempt > maxRetries {
			return nil, fmt.Errorf("rate limited %d times: %w", maxRetries, err)
		}
		// no hot loop when the provider doesn't say how long
		wait = max(time.Second, min(maxWait, wait))
		if onWait != nil {
			onWait(attempt, wait)
		}
		timer := newTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func downloadChapter(ctx context.Context, client *libmangal.Client, chapter mangadata.Chapter, options libmangal.DownloadOptions) (*metadata.DownloadedChapter, error) {
	if !bindingsOff(ctx) {
		bound, err := ApplyBinding(ctx, client, chapter.Volume().Manga())
		if err != nil {
			return nil, err
		}
		if bound {
			// a search would replace it
			options.SearchMetadata = false
		}
	}
	down, err := client.DownloadChapterContext(ctx, chapter, options)
	if err != nil && options.Strict && metadata.Validate(chapter.Volume().Manga().Metadata()) != nil {
		return nil, fmt.Errorf("no valid metadata, bind the manga (mangal bind, or A in the TUI) or set download.metadata.strict = false: %w", err)
	}
	if err != nil || options.Directory == path.Temp() {
		return down, err
	}
	if down.Skipped != "" {
		log.Info("chapter %q skipped: %s", chapter, down.Skipped)
	}

	// the chapter is downloaded either way, a failed record is only logged
	if err := recordDownload(client, chapter, options, down); err != nil {
		log.Error("%s", err)
	}
	return down, nil
}

func recordDownload(client *libmangal.Client, chapter mangadata.Chapter, options libmangal.DownloadOptions, down *metadata.DownloadedChapter) error {
	library, err := cache.Library()
	if err != nil {
		return err
	}
	// a skipped chapter keeps who downloaded it
	if down.Skipped != "" {
		if recorded, err := library.HasDownload(down.Path()); err != nil || recorded {
			return err
		}
	}

	folder := down.Directory
	if options.CreateVolumeDir {
		folder = filepath.Dir(folder)
	}
	parent, dir := folder, ""
	if options.CreateMangaDir {
		parent, dir = filepath.Dir(folder), filepath.Base(folder)
	}

	manga := chapter.Volume().Manga().Info()
	info := chapter.Info()
	return library.RecordDownload(parent, dir, storage.Download{
		Path:         down.Path(),
		Provider:     client.Info().ID,
		MangaID:      manga.ID,
		MangaTitle:   manga.Title,
		Volume:       chapter.Volume().Info().Number,
		Chapter:      info.Number,
		ChapterTitle: info.Title,
		ChapterURL:   info.URL,
		Format:       options.Format.String(),
		Pages:        down.Pages,
		Downloaded:   time.Now(),
	})
}
