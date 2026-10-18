package inline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/selector"
)

func RunJSON(ctx context.Context, args Args) error {
	client, mangaResults, err := search(ctx, args)
	if err != nil {
		return err
	}

	if args.ChapterPopulate {
		for i, mangaResult := range mangaResults {
			chapters, err := chapters(ctx, client, args, mangaResult.Manga)
			var selErr *selector.Error
			// one manga without the chapters doesn't fail the others
			if err != nil && len(mangaResults) > 1 && ctx.Err() == nil {
				if !errors.Is(err, selector.ErrNoChapters) && !errors.As(err, &selErr) {
					args.warn("manga %q: %s", mangaResult.Manga, err)
				}
				chapters, err = []mangadata.Chapter{}, nil
			}
			if err != nil {
				return err
			}
			mangaResults[i].Chapters = &chapters
		}
	} else if args.PreferProviderMetadata {
		// scrapers read the manga's metadata from its page, with the volumes
		for _, r := range mangaResults {
			if metadata.Validate(r.Manga.Metadata()) == nil {
				continue
			}
			args.status("getting the metadata of %q from %s", r.Manga, args.Provider)
			if _, err := client.MangaVolumesContext(ctx, r.Manga); err != nil {
				return fmt.Errorf("volumes of %q: %w", r.Manga, err)
			}
		}
	}

	// only a preview of the metadata, the download searches it again
	if err := assignMetadata(ctx, client, args, mangaResults); err != nil {
		return err
	}

	queryResultJSON, err := json.Marshal(QueryResult{
		QueryParams: args,
		Results:     mangaResults,
	})
	if err != nil {
		return err
	}

	args.print(func() { fmt.Println(string(queryResultJSON)) })
	return nil
}
