package library

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// IDProvider is a metadata provider ids are checked with, a *metadata.ProviderWithCache.
type IDProvider interface {
	Info() metadata.ProviderInfo
	SearchByIDsContext(ctx context.Context, ids []int) (map[int]metadata.Metadata, error)
	FindClosestManyContext(ctx context.Context, queries []metadata.Query) ([]metadata.Match, []bool, error)
}

// IDIssue is what a check found wrong with a manga's metadata id.
type IDIssue uint8

const (
	// IDNotFound is an id the provider doesn't know.
	IDNotFound IDIssue = iota + 1
	// IDMismatch is an id whose metadata has none of the manga's titles.
	IDMismatch
	// IDSuggested is a manga without ids, and the provider's closest match to its title.
	IDSuggested
)

// IDCheck is a manga whose metadata id looks wrong, or that has none.
type IDCheck struct {
	Manga storage.Manga
	Issue IDIssue
	// ID is the checked id, or the suggested one.
	ID metadata.ID
	// Metadata is what the provider has for the id, nil if not found.
	Metadata metadata.Metadata
	// Score is how well Metadata matches the manga's titles, see metadata.Score.
	Score    float64
	Provider string
}

func (c IDCheck) String() string {
	tag := fmt.Sprintf("%sid-%s", c.ID.Code, c.ID.Raw)
	switch c.Issue {
	case IDNotFound:
		return fmt.Sprintf("%s: %s not found on %s", c.Manga.Dir, tag, c.Provider)
	case IDMismatch:
		return fmt.Sprintf("%s: %s is %q on %s (score %.2f)", c.Manga.Dir, tag, c.Metadata.Title(), c.Provider, c.Score)
	default:
		return fmt.Sprintf("%s: no id, %s suggests %s %q (score %.2f)", c.Manga.Dir, c.Provider, tag, c.Metadata.Title(), c.Score)
	}
}

// IDChecker checks the metadata ids library scans guess for the folders they adopt
// (those without a .libmangal.json, see Scanner.Scan): nothing verifies them until a
// download links the folder.
type IDChecker struct {
	FS afero.Fs
	// Providers check the ids of their source, ids of other sources aren't checked.
	Providers map[metadata.IDSource]IDProvider
	// Suggest finds an id for the adopted mangas without any, nil to not.
	Suggest IDProvider
}

// Check reports the adopted mangas' ids not found or unlike the manga's title (its
// library title or folder name), and suggestions for those without ids. Requests are
// batched per provider.
func (c IDChecker) Check(ctx context.Context, mangas []storage.Manga) ([]IDCheck, error) {
	type checked struct {
		manga storage.Manga
		id    metadata.ID
	}
	bySource := map[metadata.IDSource][]checked{}
	var noIDs []storage.Manga
	for _, m := range mangas {
		if !c.adopted(m) {
			continue
		}
		if len(m.MetadataIDs) == 0 {
			if len(query(m).Titles) != 0 {
				noIDs = append(noIDs, m)
			}
			continue
		}
		for _, id := range m.MetadataIDs {
			if c.Providers[id.Source] != nil {
				bySource[id.Source] = append(bySource[id.Source], checked{manga: m, id: id})
			}
		}
	}

	var checks []IDCheck
	for _, source := range []metadata.IDSource{metadata.IDSourceAnilist, metadata.IDSourceMyAnimeList, metadata.IDSourceKitsu, metadata.IDSourceMangaUpdates, metadata.IDSourceAnimePlanet} {
		list := bySource[source]
		if len(list) == 0 {
			continue
		}
		provider := c.Providers[source]
		name := provider.Info().Name
		var ids []int
		for _, ch := range list {
			if n, err := strconv.Atoi(ch.id.Raw); err == nil {
				ids = append(ids, n)
			}
		}
		metas, err := provider.SearchByIDsContext(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("check %s ids: %w", name, err)
		}
		for _, ch := range list {
			n, err := strconv.Atoi(ch.id.Raw)
			meta, ok := metas[n]
			if err != nil || !ok {
				checks = append(checks, IDCheck{Manga: ch.manga, Issue: IDNotFound, ID: ch.id, Provider: name})
				continue
			}
			if score := metadata.Score(query(ch.manga), meta); score < metadata.GoodMatchScore {
				checks = append(checks, IDCheck{Manga: ch.manga, Issue: IDMismatch, ID: ch.id, Metadata: meta, Score: score, Provider: name})
			}
		}
	}

	if c.Suggest == nil || len(noIDs) == 0 {
		return checks, nil
	}
	queries := make([]metadata.Query, len(noIDs))
	for i, m := range noIDs {
		queries[i] = query(m)
	}
	name := c.Suggest.Info().Name
	matches, found, err := c.Suggest.FindClosestManyContext(ctx, queries)
	if err != nil {
		return nil, fmt.Errorf("search %s: %w", name, err)
	}
	for i, m := range noIDs {
		if !found[i] || matches[i].Bad() {
			continue
		}
		checks = append(checks, IDCheck{
			Manga: m, Issue: IDSuggested, ID: matches[i].Metadata.ID(),
			Metadata: matches[i].Metadata, Score: matches[i].Score, Provider: name,
		})
	}
	return checks, nil
}

// adopted is a manga with its own folder and no .libmangal.json in it.
func (c IDChecker) adopted(m storage.Manga) bool {
	if m.Dir == "" {
		return false
	}
	if exists, err := afero.DirExists(c.FS, m.Path()); err != nil || !exists {
		return false
	}
	marked, err := afero.Exists(c.FS, filepath.Join(m.Path(), libmangal.FilenameMangaDirInfo))
	return err == nil && !marked
}

// query is the manga's library title and its folder name without tags.
func query(m storage.Manga) metadata.Query {
	var titles []string
	for _, t := range []string{m.Title, titleFromName(m.Dir)} {
		if t = strings.TrimSpace(t); t != "" && !slices.Contains(titles, t) {
			titles = append(titles, t)
		}
	}
	return metadata.Query{Titles: titles}
}
