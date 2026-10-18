package jobs

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmprogress "github.com/luevano/libmangal/progress"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/util/cache"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// swapped in tests
var (
	downloadChapters = client.Download
	lockStore        = cache.Watches
	holdClient       = client.HoldByID
)

// DownloadSpec is the download of the manga's chapters with the client's provider, as a
// job of it (client.Download: the provider's lock, rate-limit retries, the library). The
// job holds the provider's current client while it runs (a reload doesn't close it under
// the download). title names the job, origin is the screen that starts it.
func DownloadSpec(c *libmangal.Client, manga mangadata.Manga, chapters []mangadata.Chapter, options libmangal.DownloadOptions, title, origin string) Spec {
	provider := c.Info().ID
	return Spec{
		Kind:      KindDownload,
		Title:     title,
		Providers: []string{provider},
		MangaID:   manga.Info().ID,
		Origin:    origin,
		Chapters:  chapters,
		Run: func(ctx context.Context, p *Progress) error {
			c, _, release, err := holdClient(ctx, provider)
			if err != nil {
				return err
			}
			defer release()
			return RunDownload(ctx, p, func(ctx context.Context, chapters []mangadata.Chapter, onEvent func(client.Event)) client.Results {
				return downloadChapters(ctx, c, chapters, options, onEvent)
			})
		},
	}
}

// RunDownload downloads the job's batch with download, its events reported through p.
// The error says how many chapters failed (wrapping the first one's error, e.g.
// storage.ErrLockLost when another mangal took the provider over).
func RunDownload(ctx context.Context, p *Progress, download func(context.Context, []mangadata.Chapter, func(client.Event)) client.Results) error {
	batch := p.Batch()
	chapters := make([]mangadata.Chapter, len(batch))
	for i, r := range batch {
		chapters[i] = r.Chapter
	}
	p.Step(0, len(batch))
	number := func(i int) string {
		if i < 0 || i >= len(batch) {
			return ""
		}
		return "chapter " + stringutil.FormatFloat32(batch[i].Chapter.Info().Number)
	}
	ctx = lmprogress.WithReporter(ctx, func(s lmprogress.Status) { p.Note(s.String()) })
	download(ctx, chapters, func(e client.Event) {
		switch e.Kind {
		case client.EventBusy:
			p.Waiting(DescribeBusy(*e.Busy))
		case client.EventStart:
			p.Running(fmt.Sprintf("downloading %s (%d/%d)", number(e.Index), e.Index+1, len(batch)))
			p.Step(e.Index, len(batch))
		case client.EventProgress:
			p.Message(number(e.Index) + ": " + e.String())
			if pr := e.Progress; pr.Total > 0 && pr.Stage == libmangal.DownloadStageImages {
				p.Part(float64(pr.Done) / float64(pr.Total))
			}
		case client.EventWait:
			p.Message(fmt.Sprintf("%s: rate limited, retry #%d in %s", number(e.Index), e.Attempt, e.Wait))
		case client.EventDone:
			if e.Index >= 0 && e.Index < len(batch) && e.Result != nil {
				p.Result(batch[e.Index], *e.Result)
			}
			p.Step(e.Index+1, len(batch))
		}
	})
	var failed []error
	for _, r := range batch {
		if r.Err != nil {
			failed = append(failed, r.Err)
		}
	}
	if len(failed) == 0 {
		p.Message(Summary(batch))
		return nil
	}
	return fmt.Errorf("%s failed: %w", stringutil.Quantify(len(failed), "chapter", "chapters"), failed[0])
}

// Summary is a download's results in a line: "3 downloaded, 1 unavailable".
func Summary(results client.Results) string {
	var parts []string
	if n := len(results.Succeed()); n > 0 {
		parts = append(parts, fmt.Sprintf("%d downloaded", n))
	}
	if n := len(results.Failed()); n > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", n))
	}
	if n := len(results.Unavailable()); n > 0 {
		parts = append(parts, fmt.Sprintf("%d unavailable", n))
	}
	if n := len(results.ToDownload()); n > 0 {
		parts = append(parts, fmt.Sprintf("%d left", n))
	}
	if len(parts) == 0 {
		return "nothing to download"
	}
	return strings.Join(parts, ", ")
}

// DescribeBusy is "waiting for <provider> (<what>, <who>)": who holds the provider's lock,
// e.g. "waiting for mangapill (watch check, mangal serve pid 123)"; for a library
// operation "waiting for the library (rename, this mangal)".
func DescribeBusy(b client.ProviderBusy) string {
	if b.Library {
		return "waiting for the library (" + describeHolder(cmp.Or(b.Holder.Purpose, "change"), b.Holder) + ")"
	}
	what := "download"
	if b.Holder.Purpose == client.LockForWatch {
		what = "watch check"
	}
	return "waiting for " + b.Provider + " (" + describeHolder(what, b.Holder) + ")"
}

func describeHolder(what string, h storage.LockHolder) string {
	host, _ := os.Hostname()
	who := fmt.Sprintf("mangal pid %d", h.PID)
	switch {
	case h.Host != "" && h.Host != host:
		who = fmt.Sprintf("mangal pid %d on %s", h.PID, h.Host)
	case h.PID == os.Getpid():
		who = "this mangal"
	case isServe(h):
		who = fmt.Sprintf("mangal serve pid %d", h.PID)
	}
	return what + ", " + who
}

// isServe is whether the holder is the running mangal serve (it holds the serve lock).
func isServe(h storage.LockHolder) bool {
	store, err := lockStore()
	if err != nil {
		return false
	}
	serve, ok, err := store.Holder(watch.ServeLock, watch.ServeLockStale)
	return err == nil && ok && serve.PID == h.PID && serve.Host == h.Host
}
