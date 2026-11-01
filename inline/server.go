package inline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/core/remote"
	"github.com/luevano/mangal/v5/util/sanitize"
)

// maxServerLookups is how many results' metadata are searched through a server: no batches
// there, one request per manga and source, a preview of a long list isn't worth them.
const maxServerLookups = 5

// acquireFrom is the client of the provider: the server's when args have one, else this
// machine's.
func acquireFrom(ctx context.Context, args Args) (*libmangal.Client, []string, error) {
	if args.Server == nil {
		return acquire(ctx, args.Provider)
	}
	loader, err := args.Server.Loader(ctx, args.Provider)
	if err != nil {
		return nil, nil, err
	}
	c, err := client.Acquire(ctx, loader)
	return c, nil, err
}

// assignFromServer is assignMetadata through the server: its bindings, its sources and logins.
// Its results are what the API says of a manga (no "anilist" object of older scripts).
func assignFromServer(ctx context.Context, c *libmangal.Client, args Args, results []MangaResult, pending []int) {
	rc := args.Server
	enabled, err := rc.MetadataSources(ctx)
	if err != nil {
		args.warn("metadata sources: %s", err)
		return
	}
	byID := func(src client.MetadataSource, id string) (metadata.Metadata, error) {
		n, err := strconv.Atoi(id)
		if err != nil {
			return nil, err
		}
		meta, found, err := rc.Metadata(src).SearchByIDContext(ctx, n)
		if err == nil && !found {
			err = fmt.Errorf("%s manga %s not found", src.Name, id)
		}
		return meta, err
	}
	if args.AnilistID != 0 {
		src, _ := client.MetadataSourceOf(metadata.IDCodeAnilist)
		meta, err := byID(src, strconv.Itoa(args.AnilistID))
		if err != nil {
			args.warn("anilist: %s", err)
			return
		}
		for _, i := range pending {
			results[i].setMetadata(meta, src.Key)
		}
		return
	}
	if len(pending) > maxServerLookups {
		args.warn("%d results: their metadata isn't searched through a server (--id, or a selector, picks one)", len(pending))
		return
	}
	bindings := rc.Services().Bindings
	for _, i := range pending {
		manga := results[i].Manga
		list, err := bindings.List(ctx, c.Info().ID, manga.Info().ID)
		if err != nil {
			args.warn("manga %q: %s", manga, err)
			continue
		}
		bound := false
		for _, b := range list {
			src, ok := client.MetadataSourceOf(b.Source)
			if !ok {
				continue
			}
			args.status("getting the bound metadata of %q", manga)
			meta, err := byID(src, b.ID)
			if err != nil {
				args.warn("manga %q: %s", manga, err)
				continue
			}
			results[i].setMetadata(meta, src.Key)
			bound = true
			break
		}
		if bound {
			continue
		}
		for _, src := range client.MetadataSources {
			if !slices.Contains(enabled, string(src.Code)) || (src.Code == metadata.IDCodeMangaUpdates && len(pending) > 1) {
				continue
			}
			args.status("looking %q up on %s", manga, src.Name)
			hits, err := rc.Metadata(src).Hits(ctx, manga.Info().Title)
			if err != nil {
				args.warn("%s: %s", src.Key, err)
				continue
			}
			if len(hits) != 0 && !hits[0].Bad {
				results[i].setMetadata(hits[0].Meta, src.Key)
				break
			}
		}
	}
}

// serverDownload queues the download on the server, with its download settings, and prints its
// results when it's over: the paths are the server's. Stopping here (ctrl+c) cancels it.
func serverDownload(ctx context.Context, args Args) error {
	rc := args.Server
	if args.AnilistID != 0 || args.PreferProviderMetadata {
		return errors.New("--anilist-id and --prefer-provider-metadata don't go to a server, its download uses its bindings and settings: mangal bind, or --local")
	}
	job, err := rc.Jobs().Download(ctx, core.DownloadReq{Provider: args.Provider, MangaID: args.ID, Query: args.Query,
		Selector: args.MangaSelector, Chapters: args.ChapterSelector, Origin: "mangal inline"})
	if err != nil {
		return err
	}
	args.status("downloading %s on %s", job.Title, remote.Name(rc.URL))
	job, err = rc.WaitJob(ctx, job)
	if err != nil {
		if ctx.Err() != nil {
			_, _ = rc.Jobs().Cancel(context.WithoutCancel(ctx), job.ID)
		}
		return err
	}
	failed := 0
	var printErr error
	args.print(func() {
		for _, r := range job.Results {
			failed += printServerResult(r, args, &printErr)
		}
	})
	if printErr != nil {
		return printErr
	}
	if job.Status != jobs.Done && job.Error != "" {
		return errors.New(job.Error)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d chapters failed", failed, len(job.Results))
	}
	return nil
}

// printServerResult prints a chapter's result as printEvent does, 1 when it failed.
func printServerResult(r core.JobResult, args Args, printErr *error) int {
	warn := func(format string, a ...any) { fmt.Fprint(os.Stderr, sanitize.Text(fmt.Sprintf(format, a...))) }
	switch {
	case r.Unavailable != "":
		warn("warning: chapter %s skipped, unavailable: %s\n", r.Title, r.Unavailable)
		return 0
	case r.Error != "":
		warn("error: chapter %s: %s\n", r.Title, r.Error)
		return 1
	}
	if r.Skipped != "" {
		warn("warning: chapter %s skipped: %s\n", r.Title, r.Skipped)
	}
	if !args.JSONOutput {
		fmt.Println(r.Path)
		return 0
	}
	b, err := json.Marshal(r)
	if err != nil {
		*printErr = errors.Join(*printErr, err)
		return 0
	}
	fmt.Println(string(b))
	return 0
}
