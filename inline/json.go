package inline

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/luevano/mangal/client"
)

func RunJSON(ctx context.Context, args Args) error {
	client, err := client.NewClientByID(ctx, args.Provider)
	if err != nil {
		return err
	}

	mangas, err := client.SearchMangasContext(ctx, args.Query)
	if err != nil {
		return err
	}
	if len(mangas) == 0 {
		return fmt.Errorf("no mangas found with provider ID %q and query %q", args.Provider, args.Query)
	}

	mangaResults, err := getSelectedMangaResults(args, mangas)
	if err != nil {
		return err
	}

	// TODO: refactor to handle metadata in general, not just anilist
	if !args.AnilistDisable {
		// It is only assigned to the result to preview which anilist manga would be set,
		// it needs to be passed to the download too to actually bind the title and id
		assignAnilist(ctx, client, args, &mangaResults)
	}

	if args.ChapterPopulate {
		for i, mangaResult := range mangaResults {
			chapters, err := getChapters(ctx, client, args, mangaResult.Manga)
			if err != nil {
				return err
			}
			mangaResults[i].Chapters = &chapters
		}
	}

	queryResult := QueryResult{
		QueryParams: args,
		Results:     mangaResults,
	}
	queryResultJSON, err := json.Marshal(queryResult)
	if err != nil {
		return err
	}

	fmt.Println(string(queryResultJSON))
	return nil
}
