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
	_ = EnsureLogin(ctx)
	found, err := ani.SearchByIDsContext(ctx, all)
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
