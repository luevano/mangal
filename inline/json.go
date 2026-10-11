package inline

import (
	"context"
	"encoding/json"
	"fmt"
)

func RunJSON(ctx context.Context, args Args) error {
	client, mangaResults, err := search(ctx, args)
	if err != nil {
		return err
	}

	// TODO: refactor to handle metadata in general, not just anilist
	if !args.AnilistDisable {
		// It is only assigned to the result to preview which anilist manga would be set,
		// it needs to be passed to the download too to actually bind the title and id
		if err := assignAnilist(ctx, client, args, mangaResults); err != nil {
			return err
		}
	}

	if args.ChapterPopulate {
		for i, mangaResult := range mangaResults {
			chapters, err := chapters(ctx, client, args, mangaResult.Manga)
			if err != nil {
				return err
			}
			mangaResults[i].Chapters = &chapters
		}
	}

	queryResultJSON, err := json.Marshal(QueryResult{
		QueryParams: args,
		Results:     mangaResults,
	})
	if err != nil {
		return err
	}

	fmt.Println(string(queryResultJSON))
	return nil
}
