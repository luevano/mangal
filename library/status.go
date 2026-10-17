package library

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
)

// StatusLabel is a publication status (storage.Manga.Status) for people: finished,
// ongoing, hiatus, cancelled or upcoming; "" when unknown.
func StatusLabel(status string) string {
	switch metadata.Status(status) {
	case metadata.StatusFinished:
		return "finished"
	case metadata.StatusReleasing:
		return "ongoing"
	case metadata.StatusHiatus:
		return "hiatus"
	case metadata.StatusCancelled:
		return "cancelled"
	case metadata.StatusNotYetReleased:
		return "upcoming"
	}
	return ""
}

// StatusDone is true for a manga nothing new comes out of: finished or cancelled.
func StatusDone(status string) bool {
	return status == string(metadata.StatusFinished) || status == string(metadata.StatusCancelled)
}

// How long a looked up status is trusted before a fetch asks again: an ongoing one may
// end, an ended one rarely changes.
const (
	statusFresh     = 24 * time.Hour
	statusDoneFresh = 30 * 24 * time.Hour
	// cachedFresh: the metadata cache keeps entries this long, an older status is
	// refreshed from the cache when it has the manga
	cachedFresh = 48 * time.Hour
)

// StatusFinder fills in the publication status of library mangas (storage.Manga.Status)
// from their AniList metadata: the cached one, or fetched when asked.
type StatusFinder struct {
	Library storage.Library
	// Cached is the cached AniList metadata of the ids, without a request; nil skips it.
	Cached func(ids []int) (map[int]metadata.Metadata, error)
	// Fetch gets the AniList metadata of the ids (cache first, the rest batched).
	Fetch func(ctx context.Context, ids []int) (map[int]metadata.Metadata, error)
	// Now is time.Now when nil.
	Now        func() time.Time
	OnProgress func(Progress)
}

// Update fills in the statuses of the mangas linked to AniList, in place, and saves
// those it found: from the cache the unknown ones and those looked up more than two
// days ago; with fetch, also from AniList the unknown ones and those looked up more than
// a day ago (a month for finished and cancelled ones). bindings are every metadata
// binding (their sources' links count).
func (f StatusFinder) Update(ctx context.Context, mangas []storage.Manga, bindings []storage.Binding, fetch bool) error {
	now := time.Now()
	if f.Now != nil {
		now = f.Now()
	}
	bySource := map[[2]string][]storage.Binding{}
	for _, b := range bindings {
		k := [2]string{b.Provider, b.MangaID}
		bySource[k] = append(bySource[k], b)
	}
	anilist := map[int][]int{} // AniList id -> indexes of mangas
	var cachedIDs, fetchIDs []int
	for i, m := range mangas {
		var own []storage.Binding
		for _, src := range m.Sources {
			own = append(own, bySource[[2]string{src.Provider, src.MangaID}]...)
		}
		id, err := strconv.Atoi(LinkInfoOf(m, own, nil).Source(metadata.IDCodeAnilist).Current())
		if err != nil || id <= 0 {
			continue
		}
		if _, seen := anilist[id]; !seen {
			age := now.Sub(m.StatusUpdated)
			if m.Status == "" || age > cachedFresh {
				cachedIDs = append(cachedIDs, id)
			}
			fresh := statusFresh
			if StatusDone(m.Status) {
				fresh = statusDoneFresh
			}
			if fetch && (m.Status == "" || age > fresh) {
				fetchIDs = append(fetchIDs, id)
			}
		}
		anilist[id] = append(anilist[id], i)
	}
	found := map[int]metadata.Metadata{}
	if f.Cached != nil && len(cachedIDs) != 0 {
		cached, err := f.Cached(cachedIDs)
		if err != nil {
			return fmt.Errorf("cached AniList metadata: %w", err)
		}
		found = cached
	}
	if fetch && f.Fetch != nil {
		var missing []int
		for _, id := range fetchIDs {
			if _, ok := found[id]; !ok {
				missing = append(missing, id)
			}
		}
		if len(missing) != 0 {
			report(f.OnProgress, Progress{Phase: fmt.Sprintf("looking up the status of %d mangas on AniList", len(missing))})
			fetched, err := f.Fetch(ctx, missing)
			if err != nil {
				return fmt.Errorf("AniList metadata: %w", err)
			}
			for id, meta := range fetched {
				found[id] = meta
			}
		}
	}
	statuses := map[int64]string{}
	for id, meta := range found {
		if meta == nil {
			continue
		}
		status := string(meta.Status())
		for _, i := range anilist[id] {
			mangas[i].Status, mangas[i].StatusUpdated = status, now
			statuses[mangas[i].ID] = status
		}
	}
	return f.Library.SetStatuses(statuses, now)
}

// HoldLabel is a hold mark (storage.Manga.Hold) for people: "on hold: hiatus", "on hold:
// ended"; "" for none.
func HoldLabel(hold string) string {
	if hold == "" {
		return ""
	}
	return "on hold: " + hold
}
