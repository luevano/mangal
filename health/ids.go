package health

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// removeNames are library bind --remove's names of the sources.
var removeNames = map[metadata.IDCode]string{
	metadata.IDCodeAnilist:     "anilist",
	metadata.IDCodeKitsu:       "kitsu",
	metadata.IDCodeMyAnimeList: "mal",
}

// backupCodes are the sources M2 fills, in the order they're shown.
var backupCodes = []metadata.IDCode{metadata.IDCodeKitsu, metadata.IDCodeMyAnimeList}

// linked are the ids each library manga is linked to (a binding or its own), by source.
func (s *snapshot) linked() map[int64]library.Links {
	out := make(map[int64]library.Links, len(s.mangas))
	for _, m := range s.mangas {
		links := library.Links{}
		for _, src := range library.LinkInfoOf(m, s.sourceBindings(m), nil).Sources {
			if src.Linked() {
				links[src.Code] = src.Current()
			}
		}
		out[m.ID] = links
	}
	return out
}

// adoptedIssue is what the id check found wrong with the manga's linked id of the source;
// the ids it had before (M3's) aren't.
func (s *snapshot) adoptedIssue(m storage.Manga, code metadata.IDCode) (library.IDCheck, bool) {
	for _, ch := range s.idChecks {
		if ch.Manga.ID != m.ID || ch.ID.Code != code || (ch.Issue != library.IDNotFound && ch.Issue != library.IDMismatch) {
			continue
		}
		src := library.LinkInfoOf(m, s.sourceBindings(m), nil).Source(code)
		if src.Linked() && src.Current() == ch.ID.Raw {
			return ch, true
		}
	}
	return library.IDCheck{}, false
}

// loadIDChecks checks the ids of the adopted folders.
func (c Checker) loadIDChecks(ctx context.Context, s *snapshot) error {
	if c.IDCheck == nil {
		return errors.New("not available")
	}
	var err error
	s.idChecks, err = c.IDCheck(ctx, s.mangas)
	return err
}

// loadFromMAL looks up the AniList ids of the MyAnimeList ids of the mangas M1 has no
// suggestion for.
func (c Checker) loadFromMAL(ctx context.Context, s *snapshot) error {
	byMAL := map[int][]int64{}
	var ids []int
	for _, m := range s.mangas {
		if linked, suggest, _ := s.anilistSuggestion(m); linked || suggest != "" {
			continue
		}
		mal := library.LinkInfoOf(m, s.sourceBindings(m), nil).Source(metadata.IDCodeMyAnimeList)
		n, err := strconv.Atoi(mal.Current())
		if !mal.Linked() || err != nil || n <= 0 {
			continue
		}
		if _, ok := byMAL[n]; !ok {
			ids = append(ids, n)
		}
		byMAL[n] = append(byMAL[n], m.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	c.progress(fmt.Sprintf("looking up the AniList ids of %s", stringutil.Quantify(len(ids), "MyAnimeList id", "MyAnimeList ids")))
	found, err := c.AnilistFromMAL(ctx, ids)
	if err != nil {
		return err
	}
	for mal, al := range found {
		for _, id := range byMAL[mal] {
			s.fromMAL[id] = strconv.Itoa(al)
		}
	}
	return nil
}

// loadBackups looks up the backup ids of the mangas linked to AniList (a right id) that
// M2 fills (no Kitsu id, or no MyAnimeList id when it suggests those) or whose Kitsu id
// M5 found wrong.
func (c Checker) loadBackups(ctx context.Context, s *snapshot) error {
	if c.Backups == nil {
		return errors.New("not available")
	}
	var todo []storage.Manga
	for _, m := range s.mangas {
		info := library.LinkInfoOf(m, s.sourceBindings(m), nil)
		if !info.Source(metadata.IDCodeAnilist).Linked() {
			continue
		}
		if _, wrong := s.adoptedIssue(m, metadata.IDCodeAnilist); wrong {
			continue
		}
		need := s.running["M2"] && (!info.Source(metadata.IDCodeKitsu).Linked() || s.mal && !info.Source(metadata.IDCodeMyAnimeList).Linked())
		if _, wrong := s.adoptedIssue(m, metadata.IDCodeKitsu); wrong && s.running["M5"] {
			need = true
		}
		if need {
			todo = append(todo, m)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	c.progress(fmt.Sprintf("looking up the backup ids of %s", stringutil.Quantify(len(todo), "manga", "mangas")))
	backups, err := c.Backups(ctx, todo)
	if err != nil {
		return err
	}
	for _, b := range backups {
		s.backups[b.Manga.ID] = b
	}
	return nil
}

// backupLinks are the links the backup fills that M2 suggests.
func (s *snapshot) backupLinks(b library.Backup) library.Links {
	links := library.Links{}
	for code, id := range b.Fill {
		if code == metadata.IDCodeKitsu || code == metadata.IDCodeMyAnimeList && s.mal {
			links[code] = id
		}
	}
	return links
}

// describeLinks is the links as "Kitsu 1 and MyAnimeList 2".
func describeLinks(links library.Links) string {
	var parts []string
	for _, code := range backupCodes {
		if id, ok := links[code]; ok {
			parts = append(parts, linkNames[code]+" "+id)
		}
	}
	return strings.Join(parts, " and ")
}

func bindCommand(manga int64, links library.Links) string {
	var cmd strings.Builder
	fmt.Fprintf(&cmd, "mangal library bind %d", manga)
	for _, code := range backupCodes {
		if id, ok := links[code]; ok {
			cmd.WriteString(" ")
			cmd.WriteString(linkFlags[code])
			cmd.WriteString(" ")
			cmd.WriteString(id)
		}
	}
	return cmd.String()
}

func backupsMessage(n int) string {
	return fmt.Sprintf("%s linked to AniList can get backup ids", stringutil.Quantify(n, "manga", "mangas"))
}

// M2 (network): library mangas linked to AniList without a Kitsu (or MyAnimeList) id,
// with the ids AniList, their MangaDex sources and Kitsu's mappings give: one summary,
// an item per manga.
func checkBackups(s *snapshot) []Finding {
	summary := Finding{Severity: Info, Subject: Subject{Kind: "library", Name: "library"}, Network: true}
	var ids []int64
	noKitsu := 0
	for _, m := range s.mangas {
		b, ok := s.backups[m.ID]
		if !ok {
			continue
		}
		if b.NoKitsu {
			noKitsu++
		}
		links := s.backupLinks(b)
		if len(links) == 0 {
			continue
		}
		ids = append(ids, m.ID)
		summary.Items = append(summary.Items, Finding{Severity: Info, Subject: mangaSubject(m), Network: true,
			Message: "can get " + describeLinks(links),
			Details: []string{"linked to AniList " + b.AniList},
			Fix: Fix{
				Commands: []string{bindCommand(m.ID, links)},
				TUI:      tuiLibrary + ": links",
				Action:   &Action{Kind: ActionBackups, Manga: m.ID, Links: links},
			}})
	}
	if len(ids) == 0 {
		return nil
	}
	slices.SortStableFunc(summary.Items, func(a, b Finding) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Subject.Name), strings.ToLower(b.Subject.Name)), cmp.Compare(a.Subject.ID, b.Subject.ID))
	})
	for i, item := range summary.Items {
		ids[i] = item.Subject.ID
	}
	summary.Message = backupsMessage(len(ids))
	summary.Details = []string{"from AniList's MyAnimeList ids, their MangaDex sources' links and Kitsu's mappings"}
	if noKitsu != 0 {
		summary.Details = append(summary.Details, fmt.Sprintf("no Kitsu id found for %s", stringutil.Quantify(noKitsu, "manga", "mangas")))
	}
	summary.Fix = Fix{Note: "--only M2 lists them, each with its command", TUI: "health: set the backup ids",
		Action: &Action{Kind: ActionBackups, Mangas: ids}}
	if s.mal {
		summary.Fix.Commands = []string{"mangal library bind --backups"}
		summary.Fix.Note = "fills them all; --only M2 lists them, each with its command"
	}
	return []Finding{summary}
}

// keepBackups is M2's summary with the items whose manga still has no id of one of their
// links, false when none is left.
func keepBackups(f Finding, links map[int64]library.Links) (Finding, bool) {
	if links == nil {
		return f, false
	}
	var items []Finding
	var ids []int64
	for _, item := range f.Items {
		a := item.Fix.Action
		if a == nil {
			continue
		}
		still := library.Links{}
		for code, id := range a.Links {
			if links[a.Manga][code] == "" {
				still[code] = id
			}
		}
		if len(still) == 0 {
			continue
		}
		if len(still) != len(a.Links) {
			action := *a
			action.Links = still
			item.Fix.Action = &action
			item.Message = "can get " + describeLinks(still)
			item.Fix.Commands = []string{bindCommand(a.Manga, still)}
		}
		items, ids = append(items, item), append(ids, a.Manga)
	}
	if len(items) == 0 {
		return f, false
	}
	f.Items = items
	f.Message = backupsMessage(len(items))
	if f.Fix.Action != nil {
		action := *f.Fix.Action
		action.Mangas = ids
		f.Fix.Action = &action
	}
	return f, true
}

// M5 (network): the ids of adopted folders (no .libmangal.json: a scan guessed them,
// nothing checked them) their source doesn't know or whose manga isn't like the folder's
// (library.IDChecker), with the id an AniList search or the backups give.
func checkAdoptedIDs(s *snapshot) []Finding {
	var out []Finding
	byID := map[int64]storage.Manga{}
	for _, m := range s.mangas {
		byID[m.ID] = m
	}
	var seen []string
	for _, ch := range s.idChecks {
		m, ok := byID[ch.Manga.ID]
		if !ok {
			continue
		}
		code := ch.ID.Code
		if _, ok := s.adoptedIssue(m, code); !ok || slices.Contains(seen, fmt.Sprint(m.ID, code)) {
			continue
		}
		seen = append(seen, fmt.Sprint(m.ID, code))
		name, flag, raw := linkNames[code], linkFlags[code], ch.ID.Raw
		f := Finding{Severity: Warning, Subject: mangaSubject(m), Part: string(code) + "/" + raw, Network: true,
			Details: []string{"a scan took it from the folder, nothing checked it (no .libmangal.json)"},
			Fix:     Fix{TUI: tuiLibrary + ": links"}}
		if ch.Issue == library.IDNotFound {
			f.Message = fmt.Sprintf("%s %s isn't on %s", name, raw, name)
		} else {
			f.Message = fmt.Sprintf("%s %s is %q, not like its title (score %.2f)", name, raw, ch.Metadata.Title(), ch.Score)
		}
		var suggest, from string
		switch code {
		case metadata.IDCodeAnilist:
			if match, ok := s.anilist[m.ID]; ok && !match.Bad() && match.Metadata.ID().Raw != raw {
				suggest = match.Metadata.ID().Raw
				from = fmt.Sprintf("an AniList search found %s %q (score %.2f)", suggest, match.Metadata.Title(), match.Score)
			}
		default:
			if b, ok := s.backups[m.ID]; ok {
				for _, cf := range b.Conflicts {
					if cf.Code == code && cf.New != raw {
						suggest = cf.New
						from = fmt.Sprintf("AniList %s gives %s %s", b.AniList, name, cf.New)
					}
				}
			}
		}
		remove := fmt.Sprintf("mangal library bind %d --remove %s", m.ID, removeNames[code])
		if suggest != "" {
			f.Details = append(f.Details, from)
			f.Fix.Commands = []string{fmt.Sprintf("mangal library bind %d %s %s", m.ID, flag, suggest), remove}
			f.Fix.Note = fmt.Sprintf("link %s, or remove the wrong id", suggest)
			f.Fix.Action = &Action{Kind: ActionLink, Manga: m.ID, Source: code, IDs: []string{suggest}}
		} else {
			f.Fix.Commands = []string{fmt.Sprintf("mangal library bind %d %s <id>", m.ID, flag), remove}
			f.Fix.Note = "search the right one in the TUI's links screen, or remove the wrong id"
		}
		out = append(out, f)
	}
	return out
}
