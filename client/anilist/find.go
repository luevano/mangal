package anilist

import (
	"context"

	"github.com/luevano/libmangal/metadata"
	lmanilist "github.com/luevano/libmangal/metadata/anilist"
)

// FindClosest is the AniList manga closest to the title, unless it's a bad match (see
// metadata.Match.Bad); the match is returned either way when found.
func FindClosest(ctx context.Context, title string) (*lmanilist.Manga, metadata.Match, bool, error) {
	ani, err := Anilist()
	if err != nil {
		return nil, metadata.Match{}, false, err
	}
	_ = EnsureLogin(ctx)
	match, found, err := ani.FindClosestContext(ctx, metadata.Query{Titles: []string{title}})
	if err != nil || !found {
		return nil, match, false, err
	}
	manga, ok := match.Metadata.(*lmanilist.Manga)
	if !ok || match.Bad() {
		return nil, match, false, nil
	}
	return manga, match, true, nil
}

// MetadataByIDs is the AniList metadata of the first AniList id of ids that's found, nil
// if none is.
func MetadataByIDs(ctx context.Context, ids []metadata.ID) (metadata.Metadata, error) {
	var ani *metadata.ProviderWithCache
	for _, id := range ids {
		if id.Source != metadata.IDSourceAnilist {
			continue
		}
		if ani == nil {
			var err error
			if ani, err = Anilist(); err != nil {
				return nil, err
			}
			_ = EnsureLogin(ctx)
		}
		meta, found, err := ani.SearchByIDContext(ctx, id.Value())
		if err != nil || found {
			return meta, err
		}
	}
	return nil, nil
}

// MetadataByIDsBatch is MetadataByIDs for several mangas, with the AniList ids of all of
// them looked up together (cache first, then batched requests).
func MetadataByIDsBatch(ctx context.Context, ids [][]metadata.ID) ([]metadata.Metadata, error) {
	return metadataByIDsBatch(ctx, ids, true, (*metadata.ProviderWithCache).SearchByIDsContext)
}

// MetadataByIDsFresh is MetadataByIDsBatch past the cache: every id is asked again
// (batched), the cache gets the answers.
func MetadataByIDsFresh(ctx context.Context, ids [][]metadata.ID) ([]metadata.Metadata, error) {
	return metadataByIDsBatch(ctx, ids, true, (*metadata.ProviderWithCache).RefreshByIDsContext)
}

// MetadataByIDsCached is MetadataByIDsBatch from the cache only, no request: mangas
// whose metadata isn't cached (or expired) get nil.
func MetadataByIDsCached(ctx context.Context, ids [][]metadata.ID) ([]metadata.Metadata, error) {
	return metadataByIDsBatch(ctx, ids, false, func(p *metadata.ProviderWithCache, _ context.Context, ids []int) (map[int]metadata.Metadata, error) {
		return p.CachedByIDs(ids)
	})
}

func metadataByIDsBatch(ctx context.Context, ids [][]metadata.ID, login bool, get func(*metadata.ProviderWithCache, context.Context, []int) (map[int]metadata.Metadata, error)) ([]metadata.Metadata, error) {
	var all []int
	for _, mangaIDs := range ids {
		for _, id := range mangaIDs {
			if id.Source == metadata.IDSourceAnilist {
				all = append(all, id.Value())
			}
		}
	}
	metas := make([]metadata.Metadata, len(ids))
	if len(all) == 0 {
		return metas, nil
	}
	ani, err := Anilist()
	if err != nil {
		return nil, err
	}
	if login {
		_ = EnsureLogin(ctx)
	}
	found, err := get(ani, ctx, all)
	if err != nil {
		return nil, err
	}
	for i, mangaIDs := range ids {
		for _, id := range mangaIDs {
			if meta, ok := found[id.Value()]; ok && id.Source == metadata.IDSourceAnilist {
				metas[i] = meta
				break
			}
		}
	}
	return metas, nil
}
