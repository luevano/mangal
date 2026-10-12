package library

import (
	"context"
	"fmt"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
)

// Backup is the Kitsu and MyAnimeList links a library manga linked to AniList can get from
// it: AniList's MyAnimeList id, the Kitsu id of its sources' links (MangaDex has them).
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
}

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

	var backups []Backup
	for i, t := range todos {
		found := Links{}
		var disagree []string
		if metas[i] != nil {
			if mal := idOf(metas[i].ExtraIDs(), metadata.IDCodeMyAnimeList); mal != "" {
				found[metadata.IDCodeMyAnimeList] = mal
			}
		}
		if b.ProviderIDs != nil {
			for _, src := range t.info.Manga.Sources {
				report(b.OnProgress, Progress{Phase: "looking up provider links", Done: i + 1, Total: len(todos), Item: t.info.Manga.Title})
				pids, err := b.ProviderIDs(ctx, src)
				if err != nil {
					return nil, fmt.Errorf("links of %s %q: %w", src.Provider, src.MangaID, err)
				}
				// only when the provider agrees on the AniList id
				if al := idOf(pids, metadata.IDCodeAnilist); al != "" && al != t.al {
					disagree = append(disagree, fmt.Sprintf("%s links AniList %s", src.Provider, al))
					continue
				}
				if kt := idOf(pids, metadata.IDCodeKitsu); kt != "" && found[metadata.IDCodeKitsu] == "" {
					found[metadata.IDCodeKitsu] = kt
				}
				if mal := idOf(pids, metadata.IDCodeMyAnimeList); mal != "" && found[metadata.IDCodeMyAnimeList] == "" {
					found[metadata.IDCodeMyAnimeList] = mal
				}
			}
		}
		backup := Backup{Manga: t.info.Manga, AniList: t.al, Fill: Links{}, Disagree: disagree}
		for _, code := range []metadata.IDCode{metadata.IDCodeKitsu, metadata.IDCodeMyAnimeList} {
			s := t.info.Source(code)
			id := found[code]
			switch {
			case id == "":
				if !s.Linked() {
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

// Links are the links to set: the fills, and the conflicts' new ids when replace.
func (b Backup) Links(replace bool) Links {
	links := Links{}
	for code, id := range b.Fill {
		links[code] = id
	}
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
		lines = append(lines, fmt.Sprintf("%d %q: linked to AniList %s, but %s (check it: mangal library bind %d)", b.Manga.ID, b.Manga.Title, b.AniList, d, b.Manga.ID))
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
