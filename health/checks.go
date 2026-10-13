package health

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
)

// checks are every check, in the order they run.
var checks = []check{
	{id: "W1", category: Watches, needs: []part{partWatches}, run: checkRelink},
	{id: "W2", category: Watches, needs: []part{partWatches, partLibrary}, run: checkLibraryIssue},
	{id: "W3", category: Watches, needs: []part{partWatches}, run: checkPaused},
	{id: "W4", category: Watches, needs: []part{partWatches}, run: checkFailing},
	{id: "W5", category: Watches, needs: []part{partWatches}, run: checkStale},
	{id: "W6", category: Watches, needs: []part{partWatches}, run: checkProviderMissing},
	{id: "W8", category: Watches, needs: []part{partWatches}, run: checkWatchOldID},
	{id: "W9", category: Watches, needs: []part{partWatches}, run: checkSchedule},
	{id: "W10", category: Watches, needs: []part{partWatches, partLibrary}, run: checkNotWatched},
	{id: "W11", category: Watches, needs: []part{partWatches, partLibrary}, run: checkWatchedTwice},
	{id: "W12", category: Watches, needs: []part{partWatches, partLibrary, partNetwork}, run: checkNewChapters, network: true},
	{id: "W13", category: Watches, needs: []part{partWatches, partLibrary}, run: checkOldSiteWatches},
	{id: "G1", category: Gaps, needs: []part{partLibrary, partWatches}, run: checkGaps},
	{id: "G3", category: Gaps, needs: []part{partWatches}, run: checkRetry},
	{id: "M1", category: Metadata, needs: []part{partLibrary}, wants: []part{partWatches}, run: checkNoAnilist},
	{id: "M3", category: Metadata, needs: []part{partLibrary}, wants: []part{partFS}, run: checkDisagreements},
	{id: "M4", category: Metadata, needs: []part{partLibrary, partFS}, run: checkBrokenMarkers},
	{id: "M6", category: Metadata, needs: []part{partLibrary}, wants: []part{partWatches}, run: checkDuplicates},
	{id: "L1", category: Library, needs: []part{partLibrary, partFS}, run: checkMissing},
	{id: "L2", category: Library, needs: []part{partLibrary, partFS}, run: checkUnmarked},
	{id: "L3", category: Library, needs: []part{partOldIDs}, run: checkOldIDs},
	{id: "L4", category: Library, needs: []part{partLibrary, partRenames}, run: checkProviderDirs},
	{id: "L5", category: Library, needs: []part{partLibrary, partRenames}, run: checkFolderNames},
	{id: "L6", category: Library, needs: []part{partFS}, run: checkTrash},
	{id: "L7", category: Library, needs: []part{partLibrary}, run: checkUninstalled},
	{id: "L8", category: Library, needs: []part{partLibrary}, wants: []part{partFS}, run: checkEmpty},
	{id: "L9", category: Library, needs: []part{partLibrary, partFS}, run: checkUntracked},
	{id: "L10", category: Library, needs: []part{partLibrary}, wants: []part{partWatches}, run: checkOldSiteSources},
	{id: "A1", category: Global, needs: []part{partAccounts}, wants: []part{partWatches}, run: checkAccounts},
}

// TUI hints name the screen and the action, not the key: the keys are the TUI's
// (tui/base keys), the health screen maps them.
const (
	tuiWatches = "watches"
	tuiLibrary = "library"
)

// W1: watches whose manga id may be wrong, the scheduler skips them.
func checkRelink(s *snapshot) []Finding {
	var out []Finding
	for _, w := range s.watches {
		if !w.NeedsRelink {
			continue
		}
		reason := w.RelinkReason
		if reason == "" {
			reason = "its manga id may be wrong"
		}
		f := Finding{Severity: Error, Subject: watchSubject(w),
			Message: "needs re-link, not checked: " + reason,
			Fix: Fix{
				Commands: []string{fmt.Sprintf("mangal watch move %d", w.ID)},
				Note:     fmt.Sprintf("searches its title on %s; add a provider and a query or mid:<id> to pick another manga", w.Provider),
				TUI:      tuiWatches + ": re-link manga",
			}}
		if m, src, ok := s.libraryElsewhere(w); ok {
			f.Details = []string{fmt.Sprintf("library manga %d %s is on %s: %s", m.ID, m.Title, src.Provider, s.rel(m.Path()))}
			f.Fix.Commands = []string{relinkCommand(w, src), f.Fix.Commands[0]}
			f.Fix.Note = fmt.Sprintf("re-link it to its library manga's source, or search its title on %s", w.Provider)
		}
		out = append(out, f)
	}
	return out
}

// libraryElsewhere is the library manga of a watch in another provider's directory, with
// the source of it a re-link can use (installed, a real manga id).
func (s *snapshot) libraryElsewhere(w watch.Watch) (storage.Manga, storage.Source, bool) {
	if !w.LibraryElsewhere {
		return storage.Manga{}, storage.Source{}, false
	}
	match := storage.MatchLibrary(s.mangas, s.providerInfo(w.Provider), w.MangaID, storage.BindingIDs(w.Bindings), s.providerDirs)
	if !match.Elsewhere {
		return storage.Manga{}, storage.Source{}, false
	}
	src, ok := installedSource(s, match.Manga)
	return match.Manga, src, ok
}

func relinkCommand(w watch.Watch, src storage.Source) string {
	return fmt.Sprintf("mangal watch move %d %s mid:%s", w.ID, src.Provider, quote(src.MangaID))
}

// W2: watches without their library manga: elsewhere (move it), downloads not in the
// library (scan), or nothing matching yet (a note).
func checkLibraryIssue(s *snapshot) []Finding {
	var out []Finding
	for _, w := range s.watches {
		switch {
		case w.LibraryElsewhere && w.NeedsRelink:
			// W1 has it: its manga id may be wrong, a library move would take it over
			f := Finding{Severity: Info, Subject: watchSubject(w), Message: "needs a re-link first (W1), " + w.LibraryIssue,
				Fix: Fix{Commands: []string{fmt.Sprintf("mangal watch move %d", w.ID)}, TUI: tuiWatches + ": re-link manga",
					Note: "re-link it, then move the library manga to it if it's still elsewhere"}}
			if m, src, ok := s.libraryElsewhere(w); ok {
				f.Details = []string{fmt.Sprintf("library manga %d %s is on %s: %s", m.ID, m.Title, src.Provider, s.rel(m.Path()))}
				f.Fix.Commands = []string{relinkCommand(w, src)}
				f.Fix.Note = "re-links it to its library manga's source"
			}
			out = append(out, f)
		case w.LibraryElsewhere:
			f := Finding{Severity: Error, Subject: watchSubject(w), Message: "not in library, not checked: " + w.LibraryIssue}
			match := storage.MatchLibrary(s.mangas, s.providerInfo(w.Provider), w.MangaID, storage.BindingIDs(w.Bindings), s.providerDirs)
			if match.Elsewhere && !strings.HasPrefix(w.MangaID, storage.PlaceholderPrefix) {
				f.Details = []string{fmt.Sprintf("library manga %d %s: %s", match.Manga.ID, match.Manga.Title, s.rel(match.Manga.Path()))}
				f.Fix.Commands = []string{fmt.Sprintf("mangal library move %d %s mid:%s", match.Manga.ID, w.Provider, quote(w.MangaID))}
				f.Fix.Note = "moves the library manga to the watch's manga (its folder into the provider's directory), the watch is linked then"
			}
			f.Fix.TUI = tuiLibrary + ": move to a provider"
			out = append(out, f)
		case w.LibraryIssue != "":
			out = append(out, Finding{Severity: Error, Subject: watchSubject(w),
				Message: "not in library, not scheduled: " + w.LibraryIssue,
				Fix: Fix{
					Commands: []string{"mangal library scan"},
					Note:     fmt.Sprintf("finds moved folders and files; if the downloads were deleted on purpose, check it anyway: mangal watch check %d", w.ID),
					TUI:      tuiLibrary + ": scan",
				}})
		case w.LibraryNote != "" && w.LibraryManga == 0 && !w.NeedsRelink && !w.ProviderMissing:
			f := Finding{Severity: Info, Subject: watchSubject(w), Message: w.LibraryNote}
			if len(w.Bindings) == 0 {
				f.Fix = Fix{Commands: []string{fmt.Sprintf("mangal watch set %d --anilist-id <id>", w.ID)},
					Note: "with an AniList id it finds its library manga by it", TUI: tuiWatches + ": edit (AniList id)"}
			} else {
				f.Fix.Note = "its first check downloads into a new folder; a folder of it elsewhere needs a move (mangal library move)"
			}
			out = append(out, f)
		}
	}
	return out
}

// W3: paused watches.
func checkPaused(s *snapshot) []Finding {
	var out []Finding
	for _, w := range s.watches {
		if w.Enabled {
			continue
		}
		out = append(out, Finding{Severity: Info, Subject: watchSubject(w), Message: "paused",
			Fix: Fix{Commands: []string{fmt.Sprintf("mangal watch resume %d", w.ID)}, TUI: tuiWatches + ": pause/resume",
				Action: &Action{Kind: ActionResume, Watch: w.ID}}})
	}
	return out
}

// W4: watches whose last checks failed; backing off (checked less often) is an error.
func checkFailing(s *snapshot) []Finding {
	var out []Finding
	for _, w := range s.watches {
		if w.Failures == 0 || !w.Enabled {
			continue
		}
		f := Finding{Severity: Warning, Subject: watchSubject(w), Message: fmt.Sprintf("the last %d checks failed", w.Failures),
			Fix: Fix{
				Commands: []string{fmt.Sprintf("mangal watch check %d", w.ID), fmt.Sprintf("mangal watch log --watch %d", w.ID)},
				Note:     fmt.Sprintf("check it now, or see its checks; if the manga moved: mangal watch move %d", w.ID),
				TUI:      tuiWatches + ": check now, details",
			}}
		if w.Failures == 1 {
			f.Message = "the last check failed"
		}
		if s.backoff > 0 && w.Failures >= s.backoff {
			f.Severity = Error
			f.Message += " (backing off: checked less often)"
		}
		if w.LastError != "" {
			f.Details = []string{"last error: " + w.LastError}
		}
		out = append(out, f)
	}
	return out
}

// W5: enabled watches without a new chapter for a long time (as watch.Service.Stale).
func checkStale(s *snapshot) []Finding {
	var out []Finding
	cutoff := s.now.Add(-s.staleAfter)
	for _, w := range s.watches {
		if !w.Enabled || w.NeedsRelink || w.ProviderMissing {
			continue
		}
		last, what := w.LastNewAt, "no new chapter since"
		if last.IsZero() {
			last, what = w.Added, "no new chapter since it was added on"
		}
		if !last.Before(cutoff) {
			continue
		}
		days := int(s.now.Sub(last) / (24 * time.Hour))
		f := Finding{Severity: Info, Subject: watchSubject(w),
			Message: fmt.Sprintf("%s %s (%d days)", what, last.Format(time.DateOnly), days),
			Fix: Fix{
				Commands: []string{fmt.Sprintf("mangal watch pause %d", w.ID), fmt.Sprintf("mangal watch move %d", w.ID)},
				Note:     "finished or on hiatus: pause it; still published elsewhere: move it",
				TUI:      tuiWatches + ": pause/resume, re-link manga",
			}}
		if status := library.StatusLabel(w.Status); status != "" {
			f.Message += ", " + status + " on AniList"
			if library.StatusDone(w.Status) {
				f.Fix.Commands, f.Fix.Note = f.Fix.Commands[:1], status+": pause it"
			}
		}
		out = append(out, f)
	}
	return out
}

// W6: watches of providers that aren't installed.
func checkProviderMissing(s *snapshot) []Finding {
	var out []Finding
	for _, w := range s.watches {
		if !w.ProviderMissing {
			continue
		}
		sev := Error
		if !w.Enabled {
			sev = Warning
		}
		out = append(out, Finding{Severity: sev, Subject: watchSubject(w),
			Message: fmt.Sprintf("provider %s isn't installed, not checked", w.Provider),
			Fix: Fix{
				Commands: []string{fmt.Sprintf("mangal watch move %d <provider>", w.ID)},
				Note:     "install the provider again, or move the watch to another one",
				TUI:      tuiWatches + ": re-link manga (another provider)",
			}})
	}
	return out
}

// W13: watches whose manga id is from the provider's old site (its downloads' chapter
// URLs): moved by a provider id migration that kept the ids, never re-linked (watches
// migrated before migrations flagged them). A check would fail with not found.
func checkOldSiteWatches(s *snapshot) []Finding {
	var out []Finding
	for _, w := range s.watches {
		if w.NeedsRelink {
			continue
		}
		old, ok := s.oldSite(w.Provider, w.MangaID)
		if !ok {
			continue
		}
		name := s.providerInfo(w.Provider).Name
		f := Finding{Severity: Warning, Subject: watchSubject(w),
			Message: fmt.Sprintf("its manga id %s is %s's, %s may not have it", w.MangaID, old, name),
			Details: []string{fmt.Sprintf("its downloads' chapter URLs are all on %s's site", old)},
			Fix: Fix{
				Commands: []string{fmt.Sprintf("mangal watch move %d", w.ID)},
				Note:     fmt.Sprintf("searches its title on %s and re-links it", name),
				TUI:      tuiWatches + ": re-link manga",
			}}
		if !w.Enabled {
			f.Severity = Info
		}
		out = append(out, f)
	}
	return out
}

// L10: library sources whose manga id is from the provider's old site (as W13), not of
// a watch W13 reports.
func checkOldSiteSources(s *snapshot) []Finding {
	watched := map[[2]string]bool{}
	for _, w := range s.watches {
		watched[[2]string{w.Provider, w.MangaID}] = true
	}
	var out []Finding
	for _, m := range s.mangas {
		for _, src := range m.Sources {
			if watched[[2]string{src.Provider, src.MangaID}] {
				continue
			}
			old, ok := s.oldSite(src.Provider, src.MangaID)
			if !ok {
				continue
			}
			name := s.providerInfo(src.Provider).Name
			out = append(out, Finding{Severity: Info, Subject: mangaSubject(m),
				Message: fmt.Sprintf("its source %s %s is %s's manga id, %s may not have it", src.Provider, src.MangaID, old, name),
				Details: []string{fmt.Sprintf("its downloads' chapter URLs are all on %s's site", old)},
				Fix: Fix{
					Commands: []string{fmt.Sprintf("mangal library move %d %s", m.ID, src.Provider)},
					Note:     fmt.Sprintf("searches its title on %s and gives it that manga as its source (the old one stays for the old downloads)", name),
					TUI:      tuiLibrary + ": move to a provider",
				}})
		}
	}
	return out
}

// W8: watches under an old provider id.
func checkWatchOldID(s *snapshot) []Finding {
	var out []Finding
	for _, w := range s.watches {
		l, alias, ok := libmangal.FindLoader(s.loaders, w.Provider)
		if !ok || alias == nil {
			continue
		}
		out = append(out, Finding{Severity: Warning, Subject: watchSubject(w),
			Message: fmt.Sprintf("under the old provider id %s, now %s", w.Provider, l.Info().ID),
			Fix: Fix{
				Commands: []string{"mangal migrate"},
				Note:     fmt.Sprintf("lists what moves to the new id, --apply moves it; the watch needs a re-link after (mangal watch move %d)", w.ID),
				TUI:      "home: migrate",
			}})
	}
	return out
}

// W9: watches whose own schedule doesn't parse (watch.schedule is used instead).
func checkSchedule(s *snapshot) []Finding {
	var out []Finding
	for _, w := range s.watches {
		if w.Schedule == "" {
			continue
		}
		if _, err := watch.ParseSchedule(w.Schedule); err != nil {
			out = append(out, Finding{Severity: Warning, Subject: watchSubject(w),
				Message: fmt.Sprintf("invalid schedule %q (%s), watch.schedule is used", w.Schedule, err),
				Fix: Fix{
					Commands: []string{fmt.Sprintf("mangal watch set %d --schedule ''", w.ID)},
					Note:     "goes back to watch.schedule, or give a valid cron schedule",
					TUI:      tuiWatches + ": edit (schedule)",
				}})
		}
	}
	return out
}

// W10: library mangas no watch keeps up to date, as one summary by publication status;
// the mangas are its items (ongoing or unknown first, the ones to watch). Finished and
// cancelled ones are fine unwatched.
func checkNotWatched(s *snapshot) []Finding {
	var items []Finding
	var done, hiatus, open int
	for _, m := range s.unwatched() {
		status := library.StatusLabel(m.Status)
		f := Finding{Check: "W10", Category: Watches, Severity: Info, Subject: mangaSubject(m),
			Message: fmt.Sprintf("not watched, %s", plural(m.Chapters, "chapter")),
			Fix:     Fix{TUI: "mangas (a provider's search): watch"}}
		switch {
		case library.StatusDone(m.Status):
			done++
			f.Message = fmt.Sprintf("%s, not watched (fine)", status)
		case m.Status == string(metadata.StatusHiatus):
			hiatus++
			f.Message = "on hiatus, not watched"
		default:
			open++
			if status == "" {
				status = "status unknown"
			}
			f.Message = status + ", not watched"
		}
		if src, ok := installedSource(s, m); ok {
			f.Fix.Commands = []string{fmt.Sprintf("mangal watch add %s mid:%s", src.Provider, quote(src.MangaID))}
			f.Fix.Action = &Action{Kind: ActionWatch, Manga: m.ID, Provider: src.Provider, MangaID: src.MangaID}
		} else {
			f.Fix.Commands = []string{fmt.Sprintf("mangal watch add <provider> %s", quote(m.Title))}
			f.Fix.Note = "no provider source (an adopted folder): search it on a provider"
			if len(m.Sources) != 0 {
				f.Fix.Note = "its providers aren't installed: search it on another one"
			}
			for _, src := range m.Sources {
				if old, ok := s.oldSite(src.Provider, src.MangaID); ok {
					f.Fix.Commands = []string{fmt.Sprintf("mangal watch add %s %s", src.Provider, quote(m.Title))}
					f.Fix.Note = fmt.Sprintf("its source's manga id is %s's (L10): search it on %s", old, src.Provider)
					break
				}
			}
		}
		items = append(items, f)
	}
	if len(items) == 0 {
		return nil
	}
	rank := func(f Finding) int {
		switch {
		case strings.Contains(f.Message, "(fine)"):
			return 2
		case strings.HasPrefix(f.Message, "on hiatus"):
			return 1
		}
		return 0
	}
	slices.SortStableFunc(items, func(a, b Finding) int { return rank(a) - rank(b) })
	var parts []string
	if open != 0 {
		parts = append(parts, fmt.Sprintf("%d ongoing/unknown", open))
	}
	if hiatus != 0 {
		parts = append(parts, fmt.Sprintf("%d on hiatus", hiatus))
	}
	if done != 0 {
		parts = append(parts, fmt.Sprintf("%d finished", done))
	}
	what := "mangas aren't watched"
	if len(items) == 1 {
		what = "manga isn't watched"
	}
	f := Finding{Severity: Info, Subject: Subject{Kind: "library", Name: "library"},
		Message: fmt.Sprintf("%d %s: %s", len(items), what, strings.Join(parts, ", ")),
		Items:   items,
		Fix: Fix{
			Commands: []string{"mangal health --only W10"},
			Note:     "lists them with the command that watches each; mangal health --network looks for new chapters of the ongoing ones",
			TUI:      "health: open the finding",
		}}
	if s.network {
		f.Fix.Note = "lists them with the command that watches each; W12 has those with new chapters"
	}
	return []Finding{f}
}

// unwatched are the library mangas with chapters no watch keeps up to date.
func (s *snapshot) unwatched() []storage.Manga {
	watched := map[int64]bool{}
	for _, w := range s.watches {
		if id := s.watchedManga(w); id != 0 {
			watched[id] = true
		}
	}
	var out []storage.Manga
	for _, m := range s.mangas {
		if !watched[m.ID] && m.Chapters != 0 {
			out = append(out, m)
		}
	}
	return out
}

// installedSource is the manga's current source, else the first one, whose provider is
// installed.
func installedSource(s *snapshot, m storage.Manga) (storage.Source, bool) {
	for _, src := range m.Sources {
		if s.installed(src.Provider) && !strings.HasPrefix(src.MangaID, storage.PlaceholderPrefix) && src.MangaID != "" {
			if _, old := s.oldSite(src.Provider, src.MangaID); !old {
				return src, true
			}
		}
	}
	return storage.Source{}, false
}

// W11: library mangas watched by more than one enabled watch (two providers downloading
// into one folder).
func checkWatchedTwice(s *snapshot) []Finding {
	byManga := map[int64][]watch.Watch{}
	for _, w := range s.watches {
		if !w.Enabled {
			continue
		}
		if id := s.watchedManga(w); id != 0 {
			byManga[id] = append(byManga[id], w)
		}
	}
	var out []Finding
	for _, m := range s.mangas {
		ws := byManga[m.ID]
		if len(ws) < 2 {
			continue
		}
		// keep the watch of its current source, else the first
		keep := 0
		if len(m.Sources) != 0 {
			for i, w := range ws {
				if w.Provider == m.Sources[0].Provider && w.MangaID == m.Sources[0].MangaID {
					keep = i
				}
			}
		}
		f := Finding{Severity: Warning, Subject: mangaSubject(m), Message: fmt.Sprintf("watched on %d providers", len(ws)),
			Fix: Fix{Note: fmt.Sprintf("keep watch %d, pause or stop the others", ws[keep].ID), TUI: tuiWatches + ": pause/resume, stop watching"}}
		for i, w := range ws {
			mark := "     "
			if i == keep {
				mark = "keep "
			}
			f.Details = append(f.Details, fmt.Sprintf("%swatch %d %s (%s)", mark, w.ID, w.Title, w.Provider))
			if i != keep {
				f.Fix.Commands = append(f.Fix.Commands, fmt.Sprintf("mangal watch pause %d", w.ID))
			}
		}
		out = append(out, f)
	}
	return out
}

// G1: chapter numbers missing between the first and last downloaded ones: whole numbers
// only (a .5 is never missing), not up to a watch's from, not in a watch's skip list.
func checkGaps(s *snapshot) []Finding {
	var out []Finding
	for _, m := range s.mangas {
		have := s.numbers[m.ID]
		if len(have) < 2 {
			continue
		}
		ws := s.watchesOf(m)
		var from float32 = -1
		var skip []float32
		gaps := false
		for _, w := range ws {
			if w.From != nil && *w.From > from {
				from = *w.From
			}
			skip = append(skip, w.Skip...)
			gaps = gaps || w.Gaps && w.Enabled
		}
		missing := missingNumbers(have, from, skip)
		if len(missing) == 0 {
			continue
		}
		f := Finding{Severity: Warning, Subject: mangaSubject(m),
			Message: fmt.Sprintf("%s missing: %s", plural(len(missing), "chapter"), formatRanges(missing))}
		if len(ws) == 0 {
			f.Fix.Note = "not watched: download them, or watch it with gap filling"
			if src, ok := installedSource(s, m); ok {
				f.Fix.Commands = []string{fmt.Sprintf("mangal watch add %s mid:%s --gaps", src.Provider, quote(src.MangaID))}
			}
			f.Fix.TUI = tuiLibrary + ": open the manga"
		} else {
			w := ws[0]
			for _, x := range ws {
				if x.Enabled {
					w = x
					break
				}
			}
			if gaps {
				f.Severity = Info
				f.Message += " (gap filling is on)"
			}
			f.Fix.Commands = []string{fmt.Sprintf("mangal watch check %d --gaps", w.ID)}
			f.Fix.Note = "downloads the ones its provider has"
			if len(missing) <= 20 {
				f.Fix.Note += fmt.Sprintf("; chapters that don't exist: mangal watch set %d --skip %s", w.ID, joinInts(missing, ","))
			}
			f.Fix.TUI = tuiWatches + ": fill gaps"
		}
		out = append(out, f)
	}
	return out
}

// maxGapSpan is the most numbers between two chapters that count as missing: more is a
// numbering outlier (a date as the chapter number), not a gap.
const maxGapSpan = 1000

// missingNumbers are the whole numbers strictly between the first and last of have
// (sorted) that it doesn't have, above from and not in skip; not between two chapters
// more than maxGapSpan apart.
func missingNumbers(have []float32, from float32, skip []float32) []int {
	var missing []int
	for i := 1; i < len(have); i++ {
		lo, hi := float64(have[i-1]), float64(have[i])
		if math.IsNaN(lo) || math.IsInf(lo, 0) || math.IsInf(hi, 0) || hi-lo > maxGapSpan {
			continue
		}
		for n := int(math.Floor(lo)) + 1; float64(n) < hi; n++ {
			f := float32(n)
			if f <= from || slices.Contains(skip, f) {
				continue
			}
			missing = append(missing, n)
		}
	}
	return missing
}

// formatRanges is "2-5, 8, 10-12".
func formatRanges(ns []int) string {
	var parts []string
	for i := 0; i < len(ns); {
		j := i
		for j+1 < len(ns) && ns[j+1] == ns[j]+1 {
			j++
		}
		if j == i {
			parts = append(parts, strconv.Itoa(ns[i]))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", ns[i], ns[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, ", ")
}

func joinInts(ns []int, sep string) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, sep)
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// G3: chapters the last check failed to download, retried by the next one.
func checkRetry(s *snapshot) []Finding {
	var out []Finding
	for _, w := range s.watches {
		if len(w.Retry) == 0 {
			continue
		}
		out = append(out, Finding{Severity: Warning, Subject: watchSubject(w),
			Message: fmt.Sprintf("%s failed to download, retried by the next check: %s", plural(len(w.Retry), "chapter"), watch.FormatNumbers(w.Retry)),
			Fix:     Fix{Commands: []string{fmt.Sprintf("mangal watch check %d", w.ID)}, TUI: tuiWatches + ": check now"}})
	}
	return out
}

var linkFlags = map[metadata.IDCode]string{
	metadata.IDCodeAnilist:     "--anilist",
	metadata.IDCodeKitsu:       "--kitsu",
	metadata.IDCodeMyAnimeList: "--mal",
}

var linkNames = map[metadata.IDCode]string{
	metadata.IDCodeAnilist:     "AniList",
	metadata.IDCodeKitsu:       "Kitsu",
	metadata.IDCodeMyAnimeList: "MyAnimeList",
}

// M1: library mangas without an AniList link, with the id the folder or a watch has.
func checkNoAnilist(s *snapshot) []Finding {
	var out []Finding
	for _, m := range s.mangas {
		info := library.LinkInfoOf(m, s.sourceBindings(m), nil)
		al := info.Source(metadata.IDCodeAnilist)
		if al.Linked() {
			continue
		}
		f := Finding{Severity: Warning, Subject: mangaSubject(m), Message: "no AniList link",
			Fix: Fix{TUI: tuiLibrary + ": links (AniList search)"}}
		var others []string
		for _, code := range []metadata.IDCode{metadata.IDCodeKitsu, metadata.IDCodeMyAnimeList} {
			if src := info.Source(code); src.Linked() {
				others = append(others, linkNames[code]+" "+src.Current())
			}
		}
		if len(others) != 0 {
			f.Severity = Info
			f.Message += " (linked to " + strings.Join(others, ", ") + ")"
		}
		suggest, from := al.Suggested(), "the folder name says"
		if suggest == "" {
			for _, w := range s.watchesOf(m) {
				if w.AnilistID != 0 {
					suggest, from = strconv.Itoa(w.AnilistID), fmt.Sprintf("watch %d is bound to", w.ID)
					break
				}
			}
		}
		if suggest != "" {
			f.Details = []string{fmt.Sprintf("%s AniList %s", from, suggest)}
			f.Fix.Commands = []string{fmt.Sprintf("mangal library bind %d --anilist %s", m.ID, suggest)}
			f.Fix.Action = &Action{Kind: ActionLink, Manga: m.ID, Source: metadata.IDCodeAnilist, IDs: []string{suggest}}
		} else {
			f.Fix.Commands = []string{"mangal library scan --check", fmt.Sprintf("mangal library bind %d --anilist <id>", m.ID)}
			f.Fix.Note = "the check suggests AniList ids for folders without one (network)"
		}
		out = append(out, f)
	}
	return out
}

// M3: library mangas whose links disagree: their own ids, their sources' bindings, the
// folder tag (library.LinkSource.Disagreements, the .libmangal.json only in the fs tier);
// and the links to pick the backups saved (a MangaDex Kitsu link against Kitsu's mapping).
func checkDisagreements(s *snapshot) []Finding {
	var out []Finding
	byID := map[int64]storage.Manga{}
	for _, m := range s.mangas {
		byID[m.ID] = m
	}
	for _, p := range s.picks {
		m, ok := byID[p.Manga]
		if !ok {
			continue
		}
		name, flag := linkNames[p.Source], linkFlags[p.Source]
		var froms []string
		f := Finding{Severity: Warning, Subject: mangaSubject(m), Part: "pick-" + string(p.Source), Fix: Fix{TUI: tuiLibrary + ": links (pick)"}}
		for _, c := range p.Candidates {
			froms = append(froms, fmt.Sprintf("%s (%s)", c.ID, c.FromName()))
			f.Fix.Commands = append(f.Fix.Commands, fmt.Sprintf("mangal library bind %d %s %s --apply", m.ID, flag, c.ID))
		}
		f.Message = fmt.Sprintf("%s: pick between %s", name, strings.Join(froms, " and "))
		f.Details = []string{fmt.Sprintf("found on %s for AniList %s, nothing linked meanwhile", p.Found.Format(time.DateOnly), p.AniList)}
		f.Fix.Note = fmt.Sprintf("linking either settles it; neither: mangal library bind %d --dismiss", m.ID)
		f.Fix.Action = &Action{Kind: ActionPick, Manga: m.ID, Source: p.Source, IDs: p.IDs()}
		out = append(out, f)
	}
	for _, m := range s.mangas {
		var marker *libmangal.MangaDirInfo
		if s.fs != nil {
			marker = s.fs.markers[m.ID]
		}
		info := library.LinkInfoOf(m, s.sourceBindings(m), marker)
		for _, src := range info.Sources {
			dis := src.Disagreements()
			if len(dis) == 0 {
				continue
			}
			cur, flag, name := src.Current(), linkFlags[src.Code], linkNames[src.Code]
			f := Finding{Severity: Warning, Subject: mangaSubject(m), Part: string(src.Code),
				Message: fmt.Sprintf("%s links disagree: %s (%s)", name, cur, src.Where()),
				Fix:     Fix{TUI: tuiLibrary + ": links"}}
			tagOnly := len(dis) == 1 && src.Tag != "" && src.Tag != cur
			f.Fix.Action = &Action{Kind: ActionLink, Manga: m.ID, Source: src.Code, IDs: []string{cur}}
			switch {
			case tagOnly && src.Code == metadata.IDCodeAnilist:
				f.Fix.Action.IDs = append(f.Fix.Action.IDs, src.Tag)
				f.Fix.Commands = []string{fmt.Sprintf("mangal library bind %d --rename", m.ID), fmt.Sprintf("mangal library bind %d %s %s", m.ID, flag, src.Tag)}
				f.Fix.Note = fmt.Sprintf("the folder name says %s: rename the folder if %s is right, else link %s", src.Tag, cur, src.Tag)
			case tagOnly:
				f.Fix.Action.IDs = []string{src.Tag, cur}
				f.Fix.Commands = []string{fmt.Sprintf("mangal library bind %d %s %s", m.ID, flag, src.Tag)}
				f.Fix.Note = fmt.Sprintf("the folder name says %s: link it, or rename the folder if %s is right", src.Tag, cur)
			default:
				keep := fmt.Sprintf("mangal library bind %d %s %s", m.ID, flag, cur)
				if src.Code == metadata.IDCodeAnilist && src.Tag != "" && src.Tag != cur {
					// the folder tag only changes with the folder's name
					keep += " --rename"
				}
				f.Fix.Commands = []string{keep}
				f.Fix.Note = fmt.Sprintf("linking replaces the other ids: keep %s", cur)
				if sug := src.Suggested(); sug != "" && sug != cur {
					f.Fix.Action.IDs = append(f.Fix.Action.IDs, sug)
					f.Fix.Commands = append(f.Fix.Commands, fmt.Sprintf("mangal library bind %d %s %s", m.ID, flag, sug))
					f.Fix.Note += fmt.Sprintf(", or the folder's %s", sug)
				}
			}
			out = append(out, f)
		}
	}
	return append(out, markerFindings(s)...)
}

// M6: library mangas sharing a metadata id (library.Duplicates), with the suggested keep.
func checkDuplicates(s *snapshot) []Finding {
	watched := func(m storage.Manga) bool { return len(s.watchesOf(m)) != 0 }
	var out []Finding
	for _, d := range library.Duplicates(s.mangas, s.allBindings, watched) {
		keep := d.Keep()
		action := &Action{Kind: ActionMerge}
		for _, dm := range d.Mangas {
			action.Mangas = append(action.Mangas, dm.Manga.ID)
		}
		f := Finding{Severity: Warning, Subject: mangaSubject(keep),
			Message: fmt.Sprintf("%s %s is in %d folders, keep %d", d.Source(), d.Raw, len(d.Mangas), keep.ID),
			Fix: Fix{
				Note:   "the keep is watched, then linked, then has more chapters; swap the ids to keep the other",
				TUI:    tuiLibrary + ": duplicates only, merge (into the keep)",
				Action: action,
			}}
		for i, dm := range d.Mangas {
			mark := "     "
			if i == 0 {
				mark = "keep "
			}
			f.Details = append(f.Details, mark+dm.Line(s.root))
			if i != 0 {
				f.Fix.Commands = append(f.Fix.Commands, fmt.Sprintf("mangal library merge %d %d", keep.ID, dm.Manga.ID))
			}
		}
		out = append(out, f)
	}
	return out
}

// L3: data under old provider ids.
func checkOldIDs(s *snapshot) []Finding {
	var out []Finding
	for _, id := range s.oldIDs {
		out = append(out, Finding{Severity: Warning, Subject: Subject{Kind: "provider", Name: id.Old},
			Message: fmt.Sprintf("data under the old provider id %s, now %s (%s)", id.Old, id.New, id.Name),
			Details: []string{fmt.Sprintf("%d library mangas, %d downloads, %d read chapters, %d watches, %d metadata bindings, %d cache entries",
				id.Sources, id.Downloads, id.Reads, id.Watches, id.Bindings, id.Cache)},
			Fix: Fix{
				Commands: []string{"mangal migrate"},
				Note:     "lists what moves to the new id, --apply moves it (moved watches need a re-link)",
				TUI:      "home: migrate",
			}})
	}
	return out
}

// L7: library mangas whose sources' providers are all uninstalled.
func checkUninstalled(s *snapshot) []Finding {
	var out []Finding
	for _, m := range s.mangas {
		if len(m.Sources) == 0 {
			continue
		}
		var providers []string
		installed := false
		for _, src := range m.Sources {
			installed = installed || s.installed(src.Provider)
			if !slices.Contains(providers, src.Provider) {
				providers = append(providers, src.Provider)
			}
		}
		if installed {
			continue
		}
		out = append(out, Finding{Severity: Warning, Subject: mangaSubject(m),
			Message: fmt.Sprintf("none of its providers is installed (%s)", strings.Join(providers, ", ")),
			Fix: Fix{
				Commands: []string{fmt.Sprintf("mangal library move %d <provider>", m.ID)},
				Note:     "install one again, or move it to another provider",
				TUI:      tuiLibrary + ": move to a provider",
			}})
	}
	return out
}

// A1: logins that expired, Lua providers that didn't load, notifications without a
// webhook, the storage's notices.
func checkAccounts(s *snapshot) []Finding {
	var out []Finding
	a := s.accounts
	for _, l := range a.Logins {
		sev := Warning
		if !l.Needed {
			sev = Info
		}
		out = append(out, Finding{Severity: sev, Subject: Subject{Kind: "account", Name: l.Service}, Message: l.Message,
			Fix: Fix{Commands: []string{l.Command}, TUI: "accounts: log in"}})
	}
	for _, err := range a.Skipped {
		out = append(out, Finding{Severity: Warning, Subject: Subject{Kind: "account", Name: "Lua providers"}, Part: err.Error(),
			Message: "skipped " + err.Error(),
			Fix:     Fix{Commands: []string{"mangal providers up"}, Note: "update the providers, or remove the broken one: mangal providers rm <dir>"}})
	}
	if a.Notify != "" && a.Notify != watch.NotifyNone && !a.Webhook && (!s.watchesLoaded || len(s.watches) != 0) {
		out = append(out, Finding{Severity: Warning, Subject: Subject{Kind: "account", Name: "Discord"},
			Message: fmt.Sprintf("watch.notify is %q but no Discord webhook is set: nothing is sent", a.Notify),
			Fix: Fix{
				Commands: []string{"mangal notify webhook", "mangal config set watch.notify none"},
				Note:     "set the webhook, or turn the notifications off",
				TUI:      "accounts: set the Discord webhook",
			}})
	}
	for _, n := range a.Notices {
		out = append(out, Finding{Severity: Info, Subject: Subject{Kind: "account", Name: "storage"}, Part: n, Message: n})
	}
	return out
}
