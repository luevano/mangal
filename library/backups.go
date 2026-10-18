package library

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
)

// Backup is the Kitsu and MyAnimeList links a library manga linked to AniList can get from
// it: AniList's MyAnimeList id, the Kitsu id of its sources' links (MangaDex has them), else
// the one Kitsu maps the AniList id to.
type Backup struct {
	Manga   storage.Manga
	AniList string
	// Fill are the sources it has no id of yet.
	Fill Links
	// Conflicts are the sources it has another id of, Old its id and New the one found.
	Conflicts []LinkChange
	// NoKitsu and NoMAL are sources nothing gave an id of.
	NoKitsu, NoMAL bool
	// Disagree are sources linking another AniList id (their links aren't used), as
	// "mango-mangadex links AniList 1".
	Disagree []string
	// Pick is the Kitsu link to pick when a MangaDex source and Kitsu's mapping disagree
	// (saved, see storage.LinkPick), nil if there's none.
	Pick *storage.LinkPick
	// Failed are the sources whose links couldn't be got, with why (the others are used).
	Failed []string
}

// BackupFinder finds the backups of library mangas.
type BackupFinder struct {
	Files
	// AniList looks up the AniList metadata of several mangas at once (batched), each by one
	// of its ids: a result per manga, nil when there's none.
	AniList func(ctx context.Context, ids [][]metadata.ID) ([]metadata.Metadata, error)
	// ProviderIDs are the metadata ids a provider gives one of its mangas (MangaDex's
	// links), nil when it gives none. Nil asks no provider.
	ProviderIDs func(ctx context.Context, src storage.Source) ([]metadata.ID, error)
	// KitsuIDs maps AniList ids to Kitsu ids (Kitsu's mappings), unknown ones left out.
	// Nil looks nothing up on Kitsu.
	KitsuIDs func(ctx context.Context, anilist []int) (map[int]int, error)
}

// kitsuBatch is how many AniList ids each KitsuIDs call gets, for the progress.
const kitsuBatch = 20

// Find works out the backups of the mangas linked to AniList, those with nothing to fill
// or change left out unless they lack a source (NoKitsu, NoMAL).
func (b BackupFinder) Find(ctx context.Context, mangas []storage.Manga) ([]Backup, error) {
	type todo struct {
		info LinkInfo
		al   string
	}
	var todos []todo
	for _, m := range mangas {
		info, err := b.LinkInfo(m)
		if err != nil {
			return nil, err
		}
		s := info.Source(metadata.IDCodeAnilist)
		if !s.Linked() {
			continue
		}
		todos = append(todos, todo{info: info, al: s.Current()})
	}
	if len(todos) == 0 {
		return nil, nil
	}
	ids := make([][]metadata.ID, len(todos))
	for i, t := range todos {
		id, _ := storage.Binding{Source: metadata.IDCodeAnilist, ID: t.al}.MetadataID()
		ids[i] = []metadata.ID{id}
	}
	report(b.OnProgress, Progress{Phase: "looking up AniList metadata"})
	metas, err := b.AniList(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("AniList metadata: %w", err)
	}
	if len(metas) != len(todos) {
		return nil, fmt.Errorf("AniList metadata: %d results for %d mangas", len(metas), len(todos))
	}

	als := make([]string, len(todos))
	for i, t := range todos {
		als[i] = t.al
	}
	kitsuIDs, err := b.kitsuIDs(ctx, als)
	if err != nil {
		return nil, err
	}

	var backups []Backup
	for i, t := range todos {
		found := Links{}
		var disagree, failed []string
		// the Kitsu id of a MangaDex source that agrees on the AniList id
		var kitsuProvider, kitsuFrom string
		if metas[i] != nil {
			if mal := idOf(metas[i].ExtraIDs(), metadata.IDCodeMyAnimeList); mal != "" {
				found[metadata.IDCodeMyAnimeList] = mal
			}
		}
		if b.ProviderIDs != nil {
			for _, src := range t.info.Manga.Sources {
				report(b.OnProgress, Progress{Phase: "looking up provider links", Done: i + 1, Total: len(todos), Item: t.info.Manga.Title})
				pids, err := b.ProviderIDs(ctx, src)
				switch {
				case err != nil && ctx.Err() != nil:
					return nil, fmt.Errorf("links of %s %q: %w", src.Provider, src.MangaID, err)
				case err != nil:
					// one provider down doesn't stop the others
					failed = append(failed, fmt.Sprintf("%s %s (%s)", src.Provider, src.MangaID, err))
					continue
				}
				// only when the provider agrees on the AniList id
				if al := idOf(pids, metadata.IDCodeAnilist); al != "" && al != t.al {
					disagree = append(disagree, fmt.Sprintf("%s links AniList %s", src.Provider, al))
					continue
				}
				if kt := idOf(pids, metadata.IDCodeKitsu); kt != "" && kitsuProvider == "" {
					kitsuProvider, kitsuFrom = kt, src.Provider
				}
				if mal := idOf(pids, metadata.IDCodeMyAnimeList); mal != "" && found[metadata.IDCodeMyAnimeList] == "" {
					found[metadata.IDCodeMyAnimeList] = mal
				}
			}
		}
		mapped := kitsuIDs[t.al]
		var pick *storage.LinkPick
		switch {
		case kitsuProvider != "" && mapped != "" && kitsuProvider != mapped:
			// neither is used, the user picks one; one of them linked already settles it
			kt := t.info.Source(metadata.IDCodeKitsu)
			if kt.Linked() && (kt.Current() == kitsuProvider || kt.Current() == mapped) {
				break
			}
			if !kt.Linked() {
				pick = &storage.LinkPick{Manga: t.info.Manga.ID, Source: metadata.IDCodeKitsu, AniList: t.al,
					Candidates: []storage.LinkCandidate{{ID: kitsuProvider, From: kitsuFrom}, {ID: mapped, From: storage.FromKitsu}}}
			}
			if pick, err = b.savePick(t.info.Manga.ID, pick); err != nil {
				return nil, err
			}
			if pick == nil || !pick.Dismissed {
				disagree = append(disagree, fmt.Sprintf("%s links Kitsu %s and Kitsu maps it to %s", kitsuFrom, kitsuProvider, mapped))
			}
		case kitsuProvider != "":
			found[metadata.IDCodeKitsu] = kitsuProvider
		case mapped != "":
			found[metadata.IDCodeKitsu] = mapped
		}
		if pick == nil && b.ProviderIDs != nil && b.KitsuIDs != nil && len(failed) == 0 {
			// both asked and they agree (or it's linked): nothing left to pick
			if _, err := b.savePick(t.info.Manga.ID, nil); err != nil {
				return nil, err
			}
		}
		if pick != nil && pick.Dismissed {
			pick = nil
		}
		backup := Backup{Manga: t.info.Manga, AniList: t.al, Fill: Links{}, Disagree: disagree, Pick: pick, Failed: failed}
		for _, code := range []metadata.IDCode{metadata.IDCodeKitsu, metadata.IDCodeMyAnimeList} {
			s := t.info.Source(code)
			id := found[code]
			switch {
			case id == "":
				if !s.Linked() && (code != metadata.IDCodeKitsu || kitsuProvider == "") {
					if code == metadata.IDCodeKitsu {
						backup.NoKitsu = true
					} else {
						backup.NoMAL = true
					}
				}
			case !s.Linked():
				backup.Fill[code] = id
			case s.Current() != id:
				backup.Conflicts = append(backup.Conflicts, LinkChange{Code: code, Old: []string{s.Current()}, New: id})
			}
		}
		if len(backup.Fill) != 0 || len(backup.Conflicts) != 0 || backup.NoKitsu || backup.NoMAL || len(disagree) != 0 {
			backups = append(backups, backup)
		}
	}
	return backups, nil
}

// savePick saves the manga's Kitsu pick, or clears it when nil; it returns the pick as
// saved (dismissed or not).
func (b BackupFinder) savePick(manga int64, pick *storage.LinkPick) (*storage.LinkPick, error) {
	if b.Library.DB == nil {
		return pick, nil
	}
	if pick == nil {
		return nil, b.Library.ClearLinkPick(manga, metadata.IDCodeKitsu)
	}
	saved, err := b.Library.SetLinkPick(*pick)
	return &saved, err
}

// kitsuIDs looks the AniList ids up on Kitsu (KitsuIDs), in batches for the progress.
// The result maps an AniList id to its Kitsu id, as strings.
func (b BackupFinder) kitsuIDs(ctx context.Context, anilist []string) (map[string]string, error) {
	found := map[string]string{}
	if b.KitsuIDs == nil {
		return found, nil
	}
	var ids []int
	for _, al := range anilist {
		if n, err := strconv.Atoi(al); err == nil && n > 0 {
			ids = append(ids, n)
		}
	}
	for start := 0; start < len(ids); start += kitsuBatch {
		chunk := ids[start:min(start+kitsuBatch, len(ids))]
		report(b.OnProgress, Progress{Phase: "looking up Kitsu ids", Done: start, Total: len(ids)})
		mapped, err := b.KitsuIDs(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("kitsu ids of AniList %v: %w", chunk, err)
		}
		for al, kt := range mapped {
			found[strconv.Itoa(al)] = strconv.Itoa(kt)
		}
	}
	return found, nil
}

// Summary sums the backups up: mangas to fill, links that differ, and those nothing gave
// a Kitsu or MyAnimeList id.
func (b BackupFinder) Summary(backups []Backup) string {
	fill, conflicts, noKitsu, noMAL := BackupSummary(backups)
	kitsu := fmt.Sprintf("no Kitsu id for %d", noKitsu)
	switch {
	case noKitsu == 0:
	case b.KitsuIDs == nil:
		kitsu += " (no MangaDex source with a Kitsu link, Kitsu isn't asked: metadata.kitsu.enabled is off)"
	default:
		kitsu += " (Kitsu maps none of their AniList ids, no MangaDex source links one)"
	}
	return fmt.Sprintf("%d mangas to fill, %d links differ; %s, no MyAnimeList id for %d", fill, conflicts, kitsu, noMAL)
}

// Links are the links to set: the fills, and the conflicts' new ids when replace.
func (b Backup) Links(replace bool) Links {
	links := Links{}
	maps.Copy(links, b.Fill)
	if replace {
		for _, c := range b.Conflicts {
			links[c.Code] = c.New
		}
	}
	return links
}

// Lines describe the backup: what it fills and what disagrees.
func (b Backup) Lines() []string {
	var lines []string
	for _, code := range []metadata.IDCode{metadata.IDCodeKitsu, metadata.IDCodeMyAnimeList} {
		if id, ok := b.Fill[code]; ok {
			lines = append(lines, fmt.Sprintf("%d %q: %s %s", b.Manga.ID, b.Manga.Title, sourceName(code), id))
		}
	}
	for _, c := range b.Conflicts {
		lines = append(lines, fmt.Sprintf("%d %q: %s is %s, AniList %s says %s (not replaced unless asked)", b.Manga.ID, b.Manga.Title, sourceName(c.Code), c.Old[0], b.AniList, c.New))
	}
	for _, d := range b.Disagree {
		hint := "check it"
		if b.Pick != nil && strings.Contains(d, " Kitsu ") {
			hint = "saved, pick one"
		}
		lines = append(lines, fmt.Sprintf("%d %q: linked to AniList %s, but %s (%s: mangal library bind %d)", b.Manga.ID, b.Manga.Title, b.AniList, d, hint, b.Manga.ID))
	}
	for _, f := range b.Failed {
		lines = append(lines, fmt.Sprintf("%d %q: couldn't get the links of %s, try again later", b.Manga.ID, b.Manga.Title, f))
	}
	return lines
}

// BackupSummary counts the backups: mangas filled, conflicts, and those that got nothing
// for Kitsu or MyAnimeList.
func BackupSummary(backups []Backup) (fill, conflicts, noKitsu, noMAL int) {
	for _, b := range backups {
		if len(b.Fill) != 0 {
			fill++
		}
		conflicts += len(b.Conflicts)
		if b.NoKitsu {
			noKitsu++
		}
		if b.NoMAL {
			noMAL++
		}
	}
	return fill, conflicts, noKitsu, noMAL
}
