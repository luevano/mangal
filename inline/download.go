package inline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/util"
)

func RunDownload(ctx context.Context, args Args) error {
	results, err := runDownload(ctx, args)
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
	useMangaMetadata := false
	if args.PreferProviderMetadata {
		if err = metadata.Validate(manga.Metadata()); err != nil {
			log.Warn("provider metadata is preferred but it's not valid: %s", err)
		} else {
			useMangaMetadata = true
		}
	}

	// Take the download options from the config and apply necessary changes
	downloadOptions := config.DownloadOptions()
	if useMangaMetadata || args.AnilistID != 0 {
		// Re-searching would replace the set metadata here
		downloadOptions.SearchMetadata = false
	}
	if downloadOptions.SearchMetadata || (!useMangaMetadata && args.AnilistID != 0) {
		_ = anilist.EnsureLogin(ctx)
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

	chapters, err := chapters(ctx, c, args, manga)
	if err != nil {
		return nil, err
	}

	var printErr error
	results := client.Download(ctx, c, chapters, downloadOptions, func(e client.Event) {
		r := e.Result
		switch {
		case e.Kind == client.EventWait:
			// stderr, stdout may be json
			fmt.Fprintf(os.Stderr, "chapter %s: rate limited (retry #%d), retrying in %s\n", r.Chapter, e.Attempt, e.Wait)
		case e.Kind != client.EventDone:
		case r.Unavailable != "":
			fmt.Fprintf(os.Stderr, "warning: chapter %s skipped, unavailable: %s\n", r.Chapter, r.Unavailable)
		case r.Err != nil:
			fmt.Fprintf(os.Stderr, "error: %s\n", util.ErrorTree(fmt.Errorf("chapter %s: %w", r.Chapter, r.Err)))
		default:
			if r.Down.Skipped != "" {
				fmt.Fprintf(os.Stderr, "warning: chapter %s skipped: %s\n", r.Chapter, r.Down.Skipped)
			}
			if !args.JSONOutput {
				fmt.Println(r.Down.Path())
				return
			}
			dc, err := json.Marshal(r.Down)
			if err != nil {
				printErr = err
				return
			}
			fmt.Println(string(dc))
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, printErr
}
