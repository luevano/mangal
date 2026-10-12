package library

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/storage"
)

// duplicateSources are the metadata sources duplicates are found by, AniList first.
var duplicateSources = []metadata.IDCode{metadata.IDCodeAnilist, metadata.IDCodeKitsu, metadata.IDCodeMyAnimeList}

var duplicateNames = map[metadata.IDCode]string{
	metadata.IDCodeAnilist:     "AniList",
	metadata.IDCodeKitsu:       "Kitsu",
	metadata.IDCodeMyAnimeList: "MyAnimeList",
}

// DuplicateManga is a library manga of a Duplicate.
type DuplicateManga struct {
	Manga   storage.Manga
	Watched bool
}

// Duplicate is a metadata id more than one library manga has: the same manga in two
// folders, to merge.
type Duplicate struct {
	ID metadata.IDCode
	// Raw is the shared id.
	Raw string
	// Mangas are the library mangas with it, the suggested keep first: watched or linked
	// (with a source) first, then the one with more chapters.
	Mangas []DuplicateManga
}

// Keep is the suggested manga to keep.
func (d Duplicate) Keep() storage.Manga { return d.Mangas[0].Manga }

// Source is the name of the id's source.
func (d Duplicate) Source() string { return duplicateNames[d.ID] }

// Duplicates are the metadata ids (AniList, Kitsu, MyAnimeList) shared by library mangas:
// their own ids and their sources' bindings. AniList ones first; a Kitsu or MyAnimeList id
// of mangas an earlier one has already grouped is left out. watched tells the watched
// mangas, may be nil.
func Duplicates(mangas []storage.Manga, bindings []storage.Binding, watched func(storage.Manga) bool) []Duplicate {
	bySource := map[[2]string][]storage.Binding{}
	for _, b := range bindings {
		k := [2]string{b.Provider, b.MangaID}
		bySource[k] = append(bySource[k], b)
	}
	type key struct {
		code metadata.IDCode
		raw  string
	}
	have := map[key][]int{}
	var keys []key
	add := func(i int, code metadata.IDCode, raw string) {
		if !slices.Contains(duplicateSources, code) || raw == "" {
			return
		}
		k := key{code, raw}
		if slices.Contains(have[k], i) {
			return
		}
		if len(have[k]) == 0 {
			keys = append(keys, k)
		}
		have[k] = append(have[k], i)
	}
	for i, m := range mangas {
		for _, id := range m.MetadataIDs {
			add(i, id.Code, id.Raw)
		}
		for _, src := range m.Sources {
			for _, b := range bySource[[2]string{src.Provider, src.MangaID}] {
				add(i, b.Source, b.ID)
			}
		}
	}
	slices.SortFunc(keys, func(a, b key) int {
		if c := cmp.Compare(slices.Index(duplicateSources, a.code), slices.Index(duplicateSources, b.code)); c != 0 {
			return c
		}
		an, aErr := strconv.Atoi(a.raw)
		bn, bErr := strconv.Atoi(b.raw)
		if aErr == nil && bErr == nil {
			return cmp.Compare(an, bn)
		}
		return cmp.Compare(a.raw, b.raw)
	})

	var dups []Duplicate
	grouped := map[[2]int64]bool{}
	for _, k := range keys {
		idx := have[k]
		if len(idx) < 2 {
			continue
		}
		d := Duplicate{ID: k.code, Raw: k.raw}
		for _, i := range idx {
			dm := DuplicateManga{Manga: mangas[i]}
			if watched != nil {
				dm.Watched = watched(mangas[i])
			}
			d.Mangas = append(d.Mangas, dm)
		}
		slices.SortStableFunc(d.Mangas, compareKeep)
		// every pair grouped already (by an AniList id) isn't news
		news := false
		for a := range d.Mangas {
			for b := a + 1; b < len(d.Mangas); b++ {
				if !grouped[pair(d.Mangas[a].Manga.ID, d.Mangas[b].Manga.ID)] {
					news = true
				}
			}
		}
		if !news {
			continue
		}
		for a := range d.Mangas {
			for b := a + 1; b < len(d.Mangas); b++ {
				grouped[pair(d.Mangas[a].Manga.ID, d.Mangas[b].Manga.ID)] = true
			}
		}
		dups = append(dups, d)
	}
	return dups
}

func pair(a, b int64) [2]int64 {
	if a > b {
		a, b = b, a
	}
	return [2]int64{a, b}
}

// compareKeep orders the mangas to keep first: watched, linked, more chapters, older.
func compareKeep(a, b DuplicateManga) int {
	rank := func(m DuplicateManga) int {
		switch {
		case m.Watched:
			return 0
		case len(m.Manga.Sources) != 0:
			return 1
		}
		return 2
	}
	if c := cmp.Compare(rank(a), rank(b)); c != 0 {
		return c
	}
	if c := cmp.Compare(b.Manga.Chapters, a.Manga.Chapters); c != 0 {
		return c
	}
	return cmp.Compare(a.Manga.ID, b.Manga.ID)
}

// DuplicateIDs are the library mangas of the duplicates, with the suggested keep of
// each (0 for a keep itself), from the first duplicate each is in.
func DuplicateIDs(dups []Duplicate) map[int64]int64 {
	ids := map[int64]int64{}
	for _, d := range dups {
		keep := d.Keep().ID
		for _, m := range d.Mangas {
			if _, ok := ids[m.Manga.ID]; ok {
				continue
			}
			if m.Manga.ID == keep {
				ids[m.Manga.ID] = 0
			} else {
				ids[m.Manga.ID] = keep
			}
		}
	}
	return ids
}

// DuplicateLines describe the duplicates (folders relative to root): a header per source
// with how many ids, then per id its mangas, the suggested keep first, and the merge
// command. Empty when there are none.
func DuplicateLines(dups []Duplicate, root string) []string {
	var lines []string
	for _, code := range duplicateSources {
		var of []Duplicate
		for _, d := range dups {
			if d.ID == code {
				of = append(of, d)
			}
		}
		if len(of) == 0 {
			continue
		}
		ids := "ids are"
		if len(of) == 1 {
			ids = "id is"
		}
		lines = append(lines, fmt.Sprintf("%d %s %s in more than one folder (merge them: mangal library merge <keep> <other>):", len(of), duplicateNames[code], ids))
		for _, d := range of {
			lines = append(lines, fmt.Sprintf("  %s %s:", d.Source(), d.Raw))
			for i, m := range d.Mangas {
				mark := "     "
				if i == 0 {
					mark = "keep "
				}
				lines = append(lines, "    "+mark+duplicateLine(m, root))
			}
			for _, m := range d.Mangas[1:] {
				lines = append(lines, fmt.Sprintf("    mangal library merge %d %d", d.Keep().ID, m.Manga.ID))
			}
		}
	}
	return lines
}

func duplicateLine(m DuplicateManga, root string) string {
	var how []string
	if len(m.Manga.Sources) == 0 {
		how = append(how, "no source")
	} else {
		how = append(how, SourcesSummary(m.Manga.Sources))
	}
	if m.Watched {
		how = append(how, "watched")
	}
	folder := m.Manga.Path()
	if rel, err := filepath.Rel(root, folder); err == nil && root != "" && isInside(root, folder) {
		folder = rel
	}
	return fmt.Sprintf("%d  %s  %d chapters  %s  %s", m.Manga.ID, m.Manga.Title, m.Manga.Chapters, strings.Join(how, ", "), folder)
}
