package inline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/client"
	"github.com/luevano/mangal/config"
	"github.com/luevano/mangal/log"
	"github.com/luevano/mangal/notify"
	"github.com/luevano/mangal/util/chapter"
)

func RunDownload(ctx context.Context, args Args) error {
	client, err := client.NewClientByID(ctx, args.Provider)
	if err != nil {
		return notify.SendError(err)
	}

	mangas, err := client.SearchMangas(ctx, args.Query)
	if err != nil {
		return notify.SendError(err)
	}
	if len(mangas) == 0 {
		err = fmt.Errorf("no mangas found with provider ID %q and query %q", args.Provider, args.Query)
		return notify.SendError(err)
	}

	mangaResults, err := getSelectedMangaResults(args, mangas)
	if err != nil {
		return notify.SendError(err)
	}
	if len(mangaResults) != 1 {
		err = fmt.Errorf("invalid manga selector %q, needs to select 1 manga only", args.MangaSelector)
		return notify.SendError(err)
	}

	manga := mangaResults[0].Manga
	useMangaMetadata := false
	if args.PreferProviderMetadata {
		if err = metadata.Validate(manga.Metadata()); err != nil {
			// TODO: this logger is only really used for TUI, better handle logs
			log.Log("provider metadata is preferred but it's not valid: %s", err.Error())
		} else {
			useMangaMetadata = true
		}
	}
	var meta metadata.Metadata
	var found bool

	// If AnilistID is provided via argument, then search for it and replace the manga metadata,
	// error out as it is expected to find some metadata (given the id).
	// Otherwise, if prefer provider metadata is true and the metadata is valid, libmangal will search for metadata.
	if !useMangaMetadata && args.AnilistID != 0 {
		// guaranteed to exist
		ani, _ := client.GetMetadataProvider(metadata.IDSourceAnilist)
		meta, found, err = ani.SearchByID(ctx, args.AnilistID)
		if err != nil {
			return notify.SendError(err)
		}
		if !found {
			err = fmt.Errorf("couldn't find anilist manga with id %d (from argument)", args.AnilistID)
			return notify.SendError(err)
		}
		manga.SetMetadata(meta)
	}

	rawChapters, err := getChapters(ctx, client, args, manga)
	if err != nil {
		return notify.SendError(err)
	}
	// wrapper for keeping track of failed/success downloads
	chapters := make(chapter.Chapters, len(rawChapters))
	for i, ch := range rawChapters {
		chapters[i] = &chapter.Chapter{
			Chapter: ch,
		}
	}

	// Take the download options from the config and apply necessary changes
	downloadOptions := config.DownloadOptions()
	if useMangaMetadata || args.AnilistID != 0 {
		// Re-searching would replace the set metadata here
		downloadOptions.SearchMetadata = false
	}

	// FIX: rework this mess
	// TODO: make configurable
	maxRetries := 10
	retryCount := 0
	for i, ch := range chapters {
		retry := true
		for retry {
			retry = false
			ch.Down, ch.Err = client.DownloadChapter(ctx, ch.Chapter, downloadOptions)
			if ch.Err != nil {
				// cancelled (e.g. ctrl+c), don't keep trying the rest of the chapters
				if ctx.Err() != nil {
					return notify.SendError(ctx.Err())
				}
				errMsg := ch.Err.Error()
				// TODO: handle other responses here too if possible
				if strings.Contains(errMsg, "429") && strings.Contains(errMsg, "Retry-After") {
					retry = true
					retryCount++
					if retryCount > maxRetries {
						// TODO: use as ch.Err?
						err = fmt.Errorf("exceeded max retries (%d) while downloading chapters", maxRetries)
						return notify.SendError(err)
					}

					raTemp := strings.Split(errMsg, ":")
					raParsed, err := strconv.Atoi(strings.TrimSpace(raTemp[len(raTemp)-1]))
					if err != nil {
						// TODO: use as ch.Err?
						err = errors.New("error while parsing Retry-Count from error mesage: " + err.Error())
						return notify.SendError(err)
					}

					retryAfter := time.Duration(min(10, raParsed)) * time.Second
					// TODO: handle different in case that JSONOutput is desired?
					fmt.Printf("429 Too Many Requests (retry #%d). Retrying in %s\n", retryCount, retryAfter)
					time.Sleep(retryAfter)
					continue
				}

				// In case that the error is not due to 429 code, just continue to the next chapter
				// as ch.Down will not be available
				if args.Provider == "mango-mangaplus" && i != len(chapters)-1 {
					time.Sleep(time.Second)
				}
				continue
			}

			if args.JSONOutput {
				dc, err := json.Marshal(ch.Down)
				if err != nil {
					return notify.SendError(err)
				}
				fmt.Println(string(dc))
			} else {
				fmt.Println(ch.Down.Path())
			}
			// To avoid abusing the mangaplus api, since there are no status codes returned to check
			// sleep for a second after each chapter download
			if ch.Down.ChapterStatus == metadata.DownloadStatusNew && args.Provider == "mango-mangaplus" && i != len(chapters)-1 {
				time.Sleep(time.Second)
			}
		}
	}
	return notify.Send(chapters)
}
