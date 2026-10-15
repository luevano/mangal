package inline

import (
	"context"
	"fmt"
	"os"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	lmanilist "github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/anilist"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/util/cache"
	"github.com/luevano/mangal/v5/util/sanitize"
)

// SourceProvider is the metadata_source of the provider's own metadata
// (--prefer-provider-metadata).
const SourceProvider = "provider"

// setMetadata makes meta the result's metadata, source is a client.MetadataSource key or
// SourceProvider.
func (r *MangaResult) setMetadata(meta metadata.Metadata, source string) {
	r.Metadata, r.MetadataSource = meta, source
	if ani, ok := meta.(*lmanilist.Manga); ok && source == "anilist" {
		r.Anilist = ani
	}
}

// providerMetadata is the manga's own metadata when it's valid. Scrapers only have it once
// the manga page was read (its volumes).
func providerMetadata(manga mangadata.Manga) (metadata.Metadata, bool) {
	meta := manga.Metadata()
	if err := metadata.Validate(meta); err != nil {
		log.Warn("manga %q: provider metadata is preferred but it's not valid: %s", manga, err)
		return nil, false
	}
	return meta, true
}

// warn prints a warning on stderr (and logs it), the results are still useful without the
// metadata.
func (a Args) warn(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	log.Warn("%s", msg)
	a.print(func() { fmt.Fprintf(os.Stderr, "warning: %s\n", sanitize.Text(msg)) })
}

// assignMetadata sets the results' metadata: the provider's when preferred and valid, the
// AniList manga of --anilist-id, the manga's bindings (mangal bind), else the closest match
// of each metadata provider in priority order (AniList, then Kitsu and MyAnimeList when
// configured), the next one only for the mangas the previous didn't match well.
//
// --no-metadata (--anilist-disable) leaves out all but the provider's metadata. It's only a preview, the download does its own search.
func assignMetadata(ctx context.Context, c *libmangal.Client, args Args, results []MangaResult) error {
	var pending []int
	for i, r := range results {
		if args.PreferProviderMetadata {
			if meta, ok := providerMetadata(r.Manga); ok {
				results[i].setMetadata(meta, SourceProvider)
				continue
			}
		}
		pending = append(pending, i)
	}
	if len(pending) == 0 || args.AnilistDisable {
		return nil
	}
	if err := anilist.EnsureLogin(ctx); err != nil {
		log.Debug("anilist login: %s", err)
	}

	if args.AnilistID != 0 {
		meta, err := client.BoundMetadata(ctx, c, metadata.IDCodeAnilist, fmt.Sprint(args.AnilistID))
		if err != nil {
			args.warn("anilist: %s", err)
			return nil
		}
		for _, i := range pending {
			results[i].setMetadata(meta, "anilist")
		}
		return nil
	}

	pending, err := assignBound(ctx, c, args, results, pending)
	if err != nil {
		return err
	}
	assignMatches(ctx, c, args, results, pending)
	return nil
}

// assignBound sets the metadata of the bound mangas: their default binding's (or the next
// that can be fetched), the unbound ones are left.
func assignBound(ctx context.Context, c *libmangal.Client, args Args, results []MangaResult, pending []int) ([]int, error) {
	bindings, err := cache.Bindings()
	if err != nil {
		return nil, err
	}
	var unbound []int
	for _, i := range pending {
		manga := results[i].Manga
		list, err := bindings.Get(c.Info().ID, manga.Info().ID)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			unbound = append(unbound, i)
			continue
		}
		args.status("getting the bound metadata of %q", manga)
		meta, err := client.FirstBound(ctx, c, list, manga.String())
		if err != nil {
			args.warn("manga %q: %s", manga, err)
			continue
		}
		src, _ := client.MetadataSourceOf(meta.ID().Code)
		results[i].setMetadata(meta, src.Key)
		if results[i].Anilist != nil {
			continue
		}
		// anilist stays the AniList binding, default or not
		for _, b := range list {
			if b.Source != metadata.IDCodeAnilist {
				continue
			}
			meta, err := client.BoundMetadata(ctx, c, b.Source, b.ID)
			if err != nil {
				args.warn("anilist: %s", err)
			} else if ani, ok := meta.(*lmanilist.Manga); ok {
				results[i].Anilist = ani
			}
		}
	}
	return unbound, nil
}

// assignMatches sets the metadata of the mangas to their closest good match, trying each
// metadata provider by priority with the mangas the previous one didn't match.
func assignMatches(ctx context.Context, c *libmangal.Client, args Args, results []MangaResult, pending []int) {
	for _, src := range client.MetadataSources {
		if len(pending) == 0 || ctx.Err() != nil {
			return
		}
		p, err := c.GetMetadataProvider(src.Source)
		if err != nil {
			// not enabled or configured
			continue
		}
		args.status("looking up %d manga(s) on %s", len(pending), src.Name)
		mangas := make([]mangadata.Manga, len(pending))
		for j, i := range pending {
			mangas[j] = results[i].Manga
		}
		// all at once, AniList allows few requests a minute
		matches, found, err := c.MatchMangasContext(ctx, p, mangas)
		if err != nil {
			args.warn("%s: %s", src.Key, err)
			continue
		}
		var next []int
		for j, i := range pending {
			switch {
			case j >= len(matches) || !found[j]:
				next = append(next, i)
			case matches[j].Bad():
				if ani, ok := matches[j].Metadata.(*lmanilist.Manga); ok && results[i].AnilistBadMatch == nil {
					results[i].AnilistBadMatch = &BadMatch{Score: matches[j].Score, Anilist: ani}
				}
				next = append(next, i)
			default:
				results[i].setMetadata(matches[j].Metadata, src.Key)
			}
		}
		pending = next
	}
}
