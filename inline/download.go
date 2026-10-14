package inline

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/util"
	"github.com/luevano/mangal/v5/util/sanitize"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// download is client.Download, swapped in tests.
var download = client.Download

func RunDownload(ctx context.Context, args Args) error {
	results, err := runDownload(ctx, args)
	// stopped (ctrl+c): nothing to tell Discord, it couldn't be sent anyway
	if ctx.Err() != nil {
		return cmp.Or(err, ctx.Err())
	}
	if err != nil {
		return notify.SendErrorContext(ctx, err)
	}
	if err := notify.SendContext(ctx, results); err != nil {
		return err
	}
	// so scripts can tell from the exit code
	if failed := len(results.Failed()); failed > 0 {
		return fmt.Errorf("%d of %d chapters failed", failed, len(results))
	}
	return nil
}

func runDownload(ctx context.Context, args Args) (client.Results, error) {
	c, mangaResults, err := search(ctx, args)
	if err != nil {
		return nil, err
	}
	if len(mangaResults) != 1 {
		return nil, fmt.Errorf("manga selector %q selects %d mangas, it needs to select 1", args.MangaSelector, len(mangaResults))
	}

	manga := mangaResults[0].Manga
	// first, scrapers read the manga's metadata from its page with the volumes
	chapters, err := chapters(ctx, c, args, manga)
	if err != nil {
		return nil, err
	}

	useMangaMetadata := false
	if args.PreferProviderMetadata {
		_, useMangaMetadata = providerMetadata(manga)
	}

	// Take the download options from the config and apply necessary changes
	downloadOptions := config.DownloadOptions()
	if useMangaMetadata || args.AnilistID != 0 {
		// Re-searching would replace the set metadata here, and so would a bound one
		downloadOptions.SearchMetadata = false
		ctx = client.WithoutBinding(ctx)
	}
	if downloadOptions.SearchMetadata || (!useMangaMetadata && args.AnilistID != 0) {
		if err := anilist.EnsureLogin(ctx); err != nil {
			// the search goes on without the login
			log.Debug("anilist login: %s", err)
		}
	}

	// If AnilistID is provided via argument, then search for it and replace the manga metadata,
	// error out as it is expected to find some metadata (given the id).
	// Otherwise, if prefer provider metadata is true and the metadata is valid, libmangal will search for metadata.
	if !useMangaMetadata && args.AnilistID != 0 {
		ani, err := c.GetMetadataProvider(metadata.IDSourceAnilist)
		if err != nil {
			return nil, err
		}
		meta, found, err := ani.SearchByIDContext(ctx, args.AnilistID)
		if err != nil {
			return nil, fmt.Errorf("get anilist manga %d: %w", args.AnilistID, err)
		}
		if !found {
			return nil, fmt.Errorf("anilist manga %d (from the argument) not found", args.AnilistID)
		}
		manga.SetMetadata(meta)
	}

	var printErr error
	results := download(ctx, c, chapters, downloadOptions, func(e client.Event) {
		chapter := func() string {
			return fmt.Sprintf("downloading chapter %s (%d/%d)", stringutil.FormatFloa32(e.Result.Chapter.Info().Number), e.Index+1, len(chapters))
		}
		switch e.Kind {
		case client.EventStart:
			args.status("%s", chapter())
		case client.EventProgress:
			args.transient("%s: %s", chapter(), e)
		default:
			if e.Kind == client.EventDone && args.Status != nil {
				// the chapter's status is over
				args.Status.Clear()
			}
			args.print(func() { printErr = errors.Join(printErr, printEvent(e, args)) })
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, printErr
}

// printEvent prints a download event: the chapters' paths (or json) on stdout, the rest
// on stderr.
func printEvent(e client.Event, args Args) error {
	r := e.Result
	// chapter titles and errors come from the providers
	warn := func(format string, a ...any) { fmt.Fprint(os.Stderr, sanitize.Text(fmt.Sprintf(format, a...))) }
	switch {
	case e.Kind == client.EventBusy:
		warn("%s\n", e.Busy.Message())
	case e.Kind == client.EventWait:
		// stderr, stdout may be json
		warn("chapter %s: rate limited (retry #%d), retrying in %s\n", r.Chapter, e.Attempt, e.Wait)
	case e.Kind != client.EventDone:
	case r.Unavailable != "":
		warn("warning: chapter %s skipped, unavailable: %s\n", r.Chapter, r.Unavailable)
	case r.Err != nil:
		warn("error: %s\n", util.ErrorTree(fmt.Errorf("chapter %s: %w", r.Chapter, r.Err)))
	default:
		if r.Down.Skipped != "" {
			warn("warning: chapter %s skipped: %s\n", r.Chapter, r.Down.Skipped)
		}
		if !args.JSONOutput {
			fmt.Println(r.Down.Path())
			return nil
		}
		dc, err := json.Marshal(r.Down)
		if err != nil {
			return err
		}
		fmt.Println(string(dc))
	}
	return nil
}
