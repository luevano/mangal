package client

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/libmangal/netutil"
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

// noDelay is a rate limit without a delay: it isn't retried and stops a Download (e.g.
// mangoplus' 10522 locks the device out for minutes, retrying keeps it locked).
func noDelay(err error) bool {
	wait, limited := netutil.RetryAfterOf(err)
	return limited && wait <= 0
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
	// EventProgress is where the chapter's download is (stage, pages done), Progress is set.
	// Events never overlap, but this one may come from another goroutine.
	EventProgress
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
	// Progress is set for EventProgress.
	Progress *libmangal.DownloadProgress
}

// String is the progress as a short line, e.g. "23/57 pages".
func (e Event) String() string {
	if e.Progress == nil {
		return ""
	}
	switch p := e.Progress; p.Stage {
	case libmangal.DownloadStageMetadata:
		return "searching metadata"
	case libmangal.DownloadStagePages:
		return "getting the pages"
	case libmangal.DownloadStageImages:
		return fmt.Sprintf("%d/%d pages", p.Done, p.Total)
	case libmangal.DownloadStageSaving:
		return fmt.Sprintf("saving %d pages", p.Total)
	}
	return string(e.Progress.Stage)
}

// Download downloads the chapters one by one (see DownloadChapter), skipping the
// unavailable ones. onEvent may be nil. A cancelled ctx, or a rate limit without a
// delay (that chapter fails), stops it: the rest of the chapters stay to download.
//
// The provider's lock (ProviderLockName) is held for the whole download, waiting
// (EventBusy) while a watch check or another download has it; each chapter waits
// (EventBusy) while a library operation runs (see Ops.Lock).
func Download(ctx context.Context, client *libmangal.Client, chapters []mangadata.Chapter, options libmangal.DownloadOptions, onEvent func(Event)) Results {
	return downloadWith(ctx, client, chapters, options, onEvent, downloadChapter)
}

// downloadWith is Download getting each chapter with get.
func downloadWith(ctx context.Context, client *libmangal.Client, chapters []mangadata.Chapter, options libmangal.DownloadOptions, onEvent func(Event),
	get func(context.Context, *libmangal.Client, mangadata.Chapter, libmangal.DownloadOptions) (*metadata.DownloadedChapter, error),
) Results {
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	// the chapters go one at a time: the progress is the started one's
	var started Event
	track := func(e Event) {
		if e.Kind == EventStart {
			started = e
		}
		onEvent(e)
	}
	options.OnProgress = func(p libmangal.DownloadProgress) {
		onEvent(Event{Kind: EventProgress, Index: started.Index, Result: started.Result, Progress: &p})
	}
	return download(ctx, client.Info().ID, chapters, track, lockStored, chapterStored, func(ctx context.Context, ch mangadata.Chapter) (*metadata.DownloadedChapter, error) {
		return get(ctx, client, ch, options)
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

// chapterFunc marks a chapter of the provider as being written (LockChapter), done unmarks it.
type chapterFunc func(ctx context.Context, provider string, onWait func(LibraryBusy)) (done func(), err error)

func chapterStored(ctx context.Context, provider string, onWait func(LibraryBusy)) (func(), error) {
	store, err := providerLockStore()
	if err != nil {
		return nil, err
	}
	return LockChapter(ctx, store, provider, onWait)
}

func download(ctx context.Context, provider string, chapters []mangadata.Chapter, onEvent func(Event), lock lockFunc, mark chapterFunc, get getFunc) Results {
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
		if locked && lockedCtx.Err() != nil {
			r.Err = fmt.Errorf("download stopped: %w", context.Cause(lockedCtx))
			onEvent(Event{Kind: EventDone, Index: i, Result: r})
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
		done, err := markChapter(lockedCtx, provider, r, i, onEvent, mark)
		if err != nil {
			if ctx.Err() == nil {
				r.Err = err
				onEvent(Event{Kind: EventDone, Index: i, Result: r})
			}
			break
		}
		onEvent(Event{Kind: EventStart, Index: i, Result: r})
		r.Down, r.Err = retry(lockedCtx, r.Chapter, get, func(attempt int, wait time.Duration) {
			onEvent(Event{Kind: EventWait, Index: i, Result: r, Attempt: attempt, Wait: wait})
		})
		done()
		if r.Err != nil && ctx.Err() != nil {
			// cancelled, not failed
			r.Err = nil
			break
		}
		if r.Err != nil && lockedCtx.Err() != nil {
			r.Err = fmt.Errorf("download stopped: %w", context.Cause(lockedCtx))
			onEvent(Event{Kind: EventDone, Index: i, Result: r})
			break
		}
		if noDelay(r.Err) {
			r.Err = fmt.Errorf("%w (stopped: the site didn't say how long to wait)", r.Err)
			onEvent(Event{Kind: EventDone, Index: i, Result: r})
			break
		}
		onEvent(Event{Kind: EventDone, Index: i, Result: r})
	}
	return results
}

// markChapter marks the chapter as being written, after waiting (EventBusy) while a library
// operation runs: the chapter would go where it no longer belongs.
func markChapter(lockedCtx context.Context, provider string, r *Result, i int, onEvent func(Event), mark chapterFunc) (func(), error) {
	if mark == nil {
		return func() {}, nil
	}
	done, err := mark(lockedCtx, provider, func(b LibraryBusy) {
		onEvent(Event{Kind: EventBusy, Index: i, Result: r, Busy: &ProviderBusy{Provider: provider, Holder: b.Holder, Library: true}})
	})
	if err != nil && lockedCtx.Err() != nil {
		err = context.Cause(lockedCtx)
	}
	if err != nil {
		return nil, fmt.Errorf("wait for the library: %w", err)
	}
	return done, nil
}

// DownloadChapter downloads the chapter and records it in the library, unless it went to
// the temp dir (read without download_on_read). The manga's default metadata binding (if
// any, see WithoutBinding) is its metadata. A chapter that's already there (maybe
// from another provider, see DownloadedChapter.Skipped) isn't downloaded again. A rate
// limit is waited out up to 10 times (EventWait), not one without a delay; the
// provider's lock and a library operation too (EventBusy, see Download). onEvent may be nil.
func DownloadChapter(ctx context.Context, client *libmangal.Client, chapter mangadata.Chapter, options libmangal.DownloadOptions, onEvent func(Event)) (*metadata.DownloadedChapter, error) {
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	r := &Result{Chapter: chapter}
	lockedCtx, unlock, err := lockStored(ctx, client.Info().ID, func(b ProviderBusy) {
		onEvent(Event{Kind: EventBusy, Result: r, Busy: &b})
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("lock provider: %w", err)
	}
	defer unlock()
	done, err := markChapter(lockedCtx, client.Info().ID, r, 0, onEvent, chapterStored)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	defer done()
	options.OnProgress = func(p libmangal.DownloadProgress) {
		onEvent(Event{Kind: EventProgress, Result: r, Progress: &p})
	}
	down, err := retry(lockedCtx, chapter, func(ctx context.Context, ch mangadata.Chapter) (*metadata.DownloadedChapter, error) {
		return downloadChapter(ctx, client, ch, options)
	}, func(attempt int, wait time.Duration) {
		onEvent(Event{Kind: EventWait, Result: r, Attempt: attempt, Wait: wait})
	})
	if err != nil && ctx.Err() == nil && lockedCtx.Err() != nil {
		err = fmt.Errorf("download stopped: %w", context.Cause(lockedCtx))
	}
	return down, err
}

// newTimer is swapped in tests
var newTimer = time.NewTimer

type getFunc func(context.Context, mangadata.Chapter) (*metadata.DownloadedChapter, error)

func retry(ctx context.Context, chapter mangadata.Chapter, get getFunc, onWait func(attempt int, wait time.Duration)) (*metadata.DownloadedChapter, error) {
	for attempt := 1; ; attempt++ {
		down, err := get(ctx, chapter)
		wait, limited := netutil.RetryAfterOf(err)
		if !limited || wait <= 0 || ctx.Err() != nil {
			return down, err
		}
		if attempt > maxRetries {
			return nil, fmt.Errorf("rate limited %d times: %w", maxRetries, err)
		}
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
	folder := down.Directory
	if options.CreateVolumeDir {
		folder = filepath.Dir(folder)
	}
	parent, dir := folder, ""
	if options.CreateMangaDir {
		parent, dir = filepath.Dir(folder), filepath.Base(folder)
	}
	manga := chapter.Volume().Manga().Info()
	home := storage.Source{Provider: client.Info().ID, MangaID: manga.ID, Title: manga.Title}
	return recordIn(parent, dir, home, client.Info().ID, manga, chapter, options.Format, down)
}

// recordIn records the chapter of the provider's manga, downloaded into the folder of
// home (storage.Library.RecordFill).
func recordIn(parent, dir string, home storage.Source, provider string, manga mangadata.MangaInfo, chapter mangadata.Chapter, format libmangal.Format, down *metadata.DownloadedChapter) error {
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
	info := chapter.Info()
	return library.RecordFill(parent, dir, home, storage.Download{
		Path:         down.Path(),
		Provider:     provider,
		MangaID:      manga.ID,
		MangaTitle:   manga.Title,
		Volume:       chapter.Volume().Info().Number,
		Chapter:      info.Number,
		ChapterTitle: info.Title,
		ChapterURL:   info.URL,
		Format:       format.String(),
		Pages:        down.Pages,
		Downloaded:   time.Now(),
	})
}
