// Package health audits the library and the watches: each check looks at one snapshot of
// the data and reports findings with the command that fixes them.
package health

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/spf13/afero"
)

// Severity is how bad a finding is.
type Severity string

const (
	// Info is worth knowing, nothing is wrong.
	Info Severity = "info"
	// Warning is something to fix, nothing stopped working.
	Warning Severity = "warning"
	// Error is something that stopped working (a watch that isn't checked any more).
	Error Severity = "error"
)

func (s Severity) rank() int {
	switch s {
	case Error:
		return 0
	case Warning:
		return 1
	}
	return 2
}

// Category groups the checks.
type Category string

const (
	Watches  Category = "watches"
	Gaps     Category = "gaps"
	Metadata Category = "metadata"
	Library  Category = "library"
	Global   Category = "global"
)

// Categories are the categories in the order they're shown.
var Categories = []Category{Watches, Gaps, Metadata, Library, Global}

// Subject is what a finding is about.
type Subject struct {
	// Kind is watch, manga (a library manga), provider, account, library (all of it),
	// folder (a manga folder not in the library) or directory (a provider directory).
	Kind string `json:"kind"`
	// ID is the watch's or library manga's id, 0 for the others.
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name"`
}

func (s Subject) String() string {
	switch {
	case s.Kind == "account" || s.Kind == "library":
		return s.Name
	case s.ID != 0:
		return fmt.Sprintf("%s %d %s", s.Kind, s.ID, s.Name)
	}
	return s.Kind + " " + s.Name
}

// Fix is how to fix a finding.
type Fix struct {
	// Commands are the CLI commands that fix it: alternatives or steps, Note says which.
	Commands []string `json:"commands,omitempty"`
	Note     string   `json:"note,omitempty"`
	// TUI is where the TUI fixes it.
	TUI string `json:"tui,omitempty"`
	// Action is the fix the TUI applies itself (after a confirm), nil when there's none.
	Action *Action `json:"action,omitempty"`
}

// Kinds of Action.
const (
	// ActionLink links the library Manga to Source's id, one of IDs (the first suggested).
	ActionLink = "link"
	// ActionPick picks one of the ids of the library Manga's link pick of Source.
	ActionPick = "pick"
	// ActionResume resumes the Watch.
	ActionResume = "resume"
	// ActionWatch watches the Provider's MangaID (the library Manga's source).
	ActionWatch = "watch"
	// ActionMerge merges the library Mangas into one (the suggested keep first).
	ActionMerge = "merge"
	// ActionMark writes the .libmangal.json of the library Manga, or of each of Mangas
	// (library.Files.MarkManga).
	ActionMark = "mark"
	// ActionForget forgets the library Manga (its folder is gone, or it has no chapters),
	// a library scan is the other choice.
	ActionForget = "forget"
	// ActionScan scans the library.
	ActionScan = "scan"
	// ActionRename does library rename's renames of the Paths (manga folders, provider
	// directories).
	ActionRename = "rename"
	// ActionEmptyTrash empties the trash.
	ActionEmptyTrash = "empty-trash"
)

// Action is a fix the TUI can apply: the same change as the fix's command.
type Action struct {
	Kind     string          `json:"kind"`
	Manga    int64           `json:"manga,omitempty"`
	Watch    int64           `json:"watch,omitempty"`
	Source   metadata.IDCode `json:"source,omitempty"`
	IDs      []string        `json:"ids,omitempty"`
	Provider string          `json:"provider,omitempty"`
	MangaID  string          `json:"manga_id,omitempty"`
	Mangas   []int64         `json:"mangas,omitempty"`
	Paths    []string        `json:"paths,omitempty"`
}

// Finding is a problem a check found.
type Finding struct {
	// Check is the check's id (W1, G1, ...).
	Check    string   `json:"check"`
	Category Category `json:"category"`
	Severity Severity `json:"severity"`
	Subject  Subject  `json:"subject"`
	Message  string   `json:"message"`
	Details  []string `json:"details,omitempty"`
	Fix      Fix      `json:"fix"`
	// Items are the subjects of a summary finding (W10's mangas), each with its fix.
	Items []Finding `json:"items,omitempty"`
	// Part tells apart the findings of a check on one subject (M3's link sources), in Key.
	Part string `json:"part,omitempty"`
	// Key is what dismisses it: check/subject (Subject.Key) and the part.
	Key string `json:"key"`
	// Dismissed is true when it was dismissed (only in a report with Options.All).
	Dismissed bool `json:"dismissed,omitempty"`
	// Network is true when it comes from what the network checks found.
	Network bool `json:"network,omitempty"`
}

// Key is what tells the subject apart in a finding's key: kind/id, kind/name, or just the
// kind when the name is the kind (the whole library).
func (s Subject) Key() string {
	switch {
	case s.ID != 0:
		return fmt.Sprintf("%s/%d", s.Kind, s.ID)
	case s.Name == s.Kind || s.Name == "":
		return s.Kind
	}
	return s.Kind + "/" + s.Name
}

// subjectKey is the finding's subject key with its part.
func (f Finding) subjectKey() string {
	if f.Part == "" {
		return f.Subject.Key()
	}
	return f.Subject.Key() + "/" + f.Part
}

// SplitKey is a finding key's check and subject key.
func SplitKey(key string) (string, string, error) {
	id, subject, ok := strings.Cut(strings.TrimSpace(key), "/")
	if !ok || id == "" || subject == "" {
		return "", "", fmt.Errorf("invalid finding key %q: it's <check>/<subject>, e.g. M3/manga/5/al (mangal health shows them)", key)
	}
	if !slices.ContainsFunc(checks, func(c check) bool { return strings.EqualFold(c.id, id) }) {
		return "", "", fmt.Errorf("invalid finding key %q: no check %s", key, id)
	}
	return strings.ToUpper(id), subject, nil
}

// Counts are how many findings there are by severity and by category.
type Counts struct {
	Info       int              `json:"info"`
	Warning    int              `json:"warning"`
	Error      int              `json:"error"`
	ByCategory map[Category]int `json:"by_category"`
}

// Total is how many findings there are.
func (c Counts) Total() int { return c.Info + c.Warning + c.Error }

// Report is what a run found.
type Report struct {
	// Findings are by category (Categories), severity, check and subject.
	Findings []Finding `json:"findings"`
	Counts   Counts    `json:"counts"`
	// Network is true when the network checks ran.
	Network bool `json:"network"`
	// Errors are the checks that couldn't run (their data didn't load, they failed).
	Errors []string `json:"errors,omitempty"`
	// Dismissed is how many findings (and summary items) were dismissed: hidden and not
	// counted, shown (Finding.Dismissed) with Options.All.
	Dismissed int `json:"dismissed,omitempty"`
}

// Options of a run.
type Options struct {
	// Network also runs the checks that make requests (W12, G2, A2, M1's AniList search)
	// and fetches the publication statuses.
	Network bool
	// Only runs these categories or checks (ids), all when empty.
	Only []string
	// All also has the dismissed findings.
	All bool
}

// Login is a saved login that needs attention.
type Login struct {
	// Service is AniList, Kitsu or MyAnimeList.
	Service string
	Message string
	// Command logs in again.
	Command string
	// Needed is true when something uses it (AniList, a read history sync).
	Needed bool
}

// Accounts is what the global check looks at.
type Accounts struct {
	// Notices are the storage's notices (e.g. converted files).
	Notices []string
	Logins  []Login
	// Skipped are the Lua providers that couldn't be loaded.
	Skipped []error
	// Webhook is true when a Discord webhook is set, Notify is watch.notify.
	Webhook bool
	Notify  string
}

// DefaultStaleAfter is how long a watch can go without a new chapter before it's stale.
const DefaultStaleAfter = 90 * 24 * time.Hour

// Checker runs the checks.
type Checker struct {
	Library storage.Library
	// Watches lists the watches (the local service or a running mangal serve), nil skips
	// the checks that need them.
	Watches watch.Manager
	// Loaders are the installed providers.
	Loaders []libmangal.ProviderLoader
	// OldIDs are the data under old provider ids (storage.FindOldIDs), nil skips L3.
	OldIDs func() ([]storage.OldID, error)
	// Accounts are the logins and notices, nil skips A1.
	Accounts func() (Accounts, error)
	// Root is download.path, ProviderDirs download.provider.create_dir.
	Root         string
	ProviderDirs bool
	// StaleAfter is DefaultStaleAfter when 0.
	StaleAfter time.Duration
	// Backoff is watch.backoff.
	Backoff int
	// Statuses fills in the publication status of the mangas (in place) from the cached
	// metadata, fetching from AniList with fetch (the network checks); nil leaves them as
	// stored.
	Statuses func(ctx context.Context, mangas []storage.Manga, bindings []storage.Binding, fetch bool) error
	// Chapters lists the chapters of each of the provider's mangas (ChaptersOf), holding
	// the provider's lock (wait is told when it waits for it); done is told each one. Nil
	// skips W12 and G2.
	Chapters func(ctx context.Context, provider string, mangas []ProviderManga, wait func(string), done func(i int, chapters ProviderChapters, err error)) error
	// Probe asks the provider for the manga (A2), within ProbeTimeout; nil skips A2.
	Probe func(ctx context.Context, provider string, manga ProviderManga) Probe
	// SearchAnilist is AniList's closest match to each query (FindClosestManyContext),
	// M1's suggestions with the network checks; nil skips them.
	SearchAnilist func(ctx context.Context, queries []metadata.Query) ([]metadata.Match, []bool, error)
	// Parallel is how many providers the network checks ask at once, 1 when 0.
	Parallel int
	// FS is the download directory's filesystem (Root), nil skips the fs checks.
	FS afero.Fs
	// Mark is how scan --mark tells a folder's provider (L2), its Loaders are Loaders.
	Mark library.MarkOptions
	// ProviderName and MangaName are the name templates, Metadata the AniList metadata of
	// mangas by their ids, cached only unless fetch (batched): L4 and L5 plan library
	// rename with them, nil skips them.
	ProviderName func(libmangal.ProviderInfo) string
	MangaName    func(libmangal.ProviderInfo, mangadata.Manga) string
	Metadata     func(ctx context.Context, ids [][]metadata.ID, fetch bool) ([]metadata.Metadata, error)
	// Now is time.Now when nil.
	Now func() time.Time
	// OnProgress is told what's loading, may be nil.
	OnProgress func(string)
	// Ignores are the dismissed findings, nil when none.
	Ignores func() ([]storage.HealthIgnore, error)
}

// part is a piece of the snapshot some checks need.
type part int

const (
	partLibrary part = iota
	partWatches
	partOldIDs
	partAccounts
	// partFS is the folders and markers, partRenames what library rename would do
	partFS
	partRenames
	// partProbes is what the providers answered, partChapters their chapters, partSearch
	// AniList's matches (network checks only: partSearch isn't loaded without them)
	partProbes
	partChapters
	partSearch
)

var partNames = map[part]string{partLibrary: "the library", partWatches: "the watches", partOldIDs: "the old provider ids",
	partAccounts: "the accounts", partFS: "the folders", partRenames: "the rename plan", partProbes: "the providers' answers",
	partChapters: "the providers' chapters", partSearch: "the AniList search"}

// check is one check over the snapshot.
type check struct {
	id       string
	category Category
	needs    []part
	// wants are loaded too, the check runs without them
	wants []part
	run   func(s *snapshot) []Finding
	// network checks make requests, they run only with Options.Network
	network bool
}

// selected are the checks opts.Only picks (the network ones only with network), an error
// for a name that's neither a category nor a check, or a network check named without
// network.
func selected(only []string, network bool) ([]check, error) {
	if len(only) == 0 {
		return slices.DeleteFunc(slices.Clone(checks), func(c check) bool { return c.network && !network }), nil
	}
	pick := map[string]bool{}
	for _, name := range only {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		known := slices.Contains(Categories, Category(name)) || slices.ContainsFunc(checks, func(c check) bool { return strings.EqualFold(c.id, name) })
		if !known {
			return nil, fmt.Errorf("unknown check or category %q (categories: %s)", name, categoryList())
		}
		pick[name] = true
	}
	var out []check
	for _, c := range checks {
		named := pick[strings.ToLower(c.id)]
		if c.network && !network {
			if named {
				return nil, fmt.Errorf("%s makes requests: add --network", c.id)
			}
			continue
		}
		if named || pick[string(c.category)] {
			out = append(out, c)
		}
	}
	return out, nil
}

// Validate checks the options' names (Run's error for them).
func Validate(opts Options) error {
	_, err := selected(opts.Only, opts.Network)
	return err
}

// IsNetwork is true for a check that makes requests (only runs with Options.Network).
func IsNetwork(id string) bool {
	return slices.ContainsFunc(checks, func(c check) bool { return c.network && strings.EqualFold(c.id, id) })
}

func categoryList() string {
	names := make([]string, len(Categories))
	for i, c := range Categories {
		names[i] = string(c)
	}
	return strings.Join(names, ", ")
}

// Run loads the snapshot the checks need and runs them. A check that fails, or whose
// data didn't load, is in Report.Errors; the others still run. Only a cancelled ctx or
// an unknown name in opts.Only is an error.
func (c Checker) Run(ctx context.Context, opts Options) (Report, error) {
	report := Report{Network: opts.Network, Counts: Counts{ByCategory: map[Category]int{}}}
	run, err := selected(opts.Only, opts.Network)
	if err != nil {
		return report, err
	}
	needed := map[part]bool{}
	running := map[string]bool{}
	for _, ch := range run {
		running[ch.id] = true
		for _, p := range slices.Concat(ch.needs, ch.wants) {
			needed[p] = true
		}
	}
	s, failed := c.load(ctx, needed, opts.Network, running)
	if err := ctx.Err(); err != nil {
		return report, err
	}
	report.Errors = append(report.Errors, s.errors...)
	loadErrors := len(s.errors)
	for p := partLibrary; p <= partSearch; p++ {
		if err := failed[p]; err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("load %s: %s", partNames[p], err))
		}
	}
	for _, ch := range run {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		var missing []string
		for _, p := range ch.needs {
			if failed[p] != nil {
				missing = append(missing, partNames[p])
			}
		}
		if len(missing) != 0 {
			report.Errors = append(report.Errors, fmt.Sprintf("%s skipped: %s didn't load", ch.id, strings.Join(missing, ", ")))
			continue
		}
		c.progress("checking " + ch.id)
		findings, err := runCheck(ch, s)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s: %s", ch.id, err))
		}
		for _, f := range findings {
			f.Check, f.Category = ch.id, ch.category
			report.Findings = append(report.Findings, f)
		}
	}
	report.Errors = append(report.Errors, s.errors[loadErrors:]...)
	if err := c.dismiss(&report, opts.All); err != nil {
		report.Errors = append(report.Errors, "dismissed findings: "+err.Error())
	}
	sortFindings(report.Findings)
	report.recount()
	return report, nil
}

// recount works the counts out from the findings.
func (r *Report) recount() {
	r.Counts = Counts{ByCategory: map[Category]int{}}
	for _, f := range r.Findings {
		if f.Dismissed {
			continue
		}
		switch f.Severity {
		case Error:
			r.Counts.Error++
		case Warning:
			r.Counts.Warning++
		default:
			r.Counts.Info++
		}
		r.Counts.ByCategory[f.Category]++
	}
}

// KeepNetwork is the offline report next with the findings of the network run last (the
// caller knows it was one) that still hold: W12 while its manga is still unwatched (a W10 item), G2 while its manga
// still has the gaps G1 found, A2, and M1's AniList matches while the manga still has
// no AniList link.
func KeepNetwork(last, next Report) Report {
	if next.Network {
		return next
	}
	network := map[string]bool{}
	for _, c := range checks {
		network[c.id] = c.network
	}
	has := func(check string, subject Subject, items bool) (int, bool) {
		for i, f := range next.Findings {
			if f.Check == check && f.Subject == subject {
				return i, true
			}
			if items && slices.ContainsFunc(f.Items, func(item Finding) bool { return item.Subject == subject }) {
				return i, true
			}
		}
		return 0, false
	}
	for _, f := range last.Findings {
		if !f.Network && !network[f.Check] {
			continue
		}
		switch f.Check {
		case "W12":
			if _, ok := has("W10", f.Subject, true); !ok {
				continue
			}
		case "G2":
			if _, ok := has("G1", f.Subject, false); !ok {
				continue
			}
		case "M1":
			i, ok := has("M1", f.Subject, false)
			if ok && next.Findings[i].Fix.Action == nil {
				next.Findings[i] = f
			}
			continue
		case "A2":
		default:
			continue
		}
		if _, ok := has(f.Check, f.Subject, false); !ok {
			next.Findings = append(next.Findings, f)
		}
	}
	next.Network = true
	sortFindings(next.Findings)
	next.recount()
	return next
}

// dismiss sets the findings' keys and leaves out the dismissed ones (marks them with all);
// a summary whose items are all dismissed is too.
func (c Checker) dismiss(report *Report, all bool) error {
	ignored := map[string]bool{}
	var err error
	if c.Ignores != nil {
		var list []storage.HealthIgnore
		if list, err = c.Ignores(); err == nil {
			for _, i := range list {
				ignored[i.Check+"/"+i.Subject] = true
			}
		}
	}
	keep := func(f *Finding) bool {
		f.Key = f.Check + "/" + f.subjectKey()
		if !ignored[f.Key] {
			return true
		}
		report.Dismissed++
		f.Dismissed = true
		return all
	}
	var out []Finding
	for _, f := range report.Findings {
		if len(f.Items) != 0 {
			var items []Finding
			hidden := 0
			for _, item := range f.Items {
				if item.Check == "" {
					item.Check = f.Check
				}
				if keep(&item) {
					items = append(items, item)
				}
				if item.Dismissed {
					hidden++
				}
			}
			if hidden != 0 && hidden == len(f.Items) && !all {
				continue
			}
			if hidden != 0 {
				f.Message += fmt.Sprintf(" (%d dismissed)", hidden)
			}
			f.Items = items
		}
		if keep(&f) {
			out = append(out, f)
		}
	}
	report.Findings = out
	return err
}

// runCheck runs the check, a panic is its error: one broken check doesn't stop the rest.
func runCheck(ch check, s *snapshot) (findings []Finding, err error) {
	defer func() {
		if r := recover(); r != nil {
			findings, err = nil, fmt.Errorf("panic: %v", r)
		}
	}()
	return ch.run(s), nil
}

func sortFindings(findings []Finding) {
	slices.SortStableFunc(findings, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(slices.Index(Categories, a.Category), slices.Index(Categories, b.Category)),
			cmp.Compare(a.Severity.rank(), b.Severity.rank()),
			compareCheck(a.Check, b.Check),
			cmp.Compare(a.Subject.Kind, b.Subject.Kind),
			cmp.Compare(a.Subject.ID, b.Subject.ID),
			strings.Compare(a.Subject.Name, b.Subject.Name),
		)
	})
}

// compareCheck orders check ids by letter, then number (W2 before W10).
func compareCheck(a, b string) int {
	if len(a) == 0 || len(b) == 0 || a[0] != b[0] {
		return strings.Compare(a, b)
	}
	return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b))
}

func (c Checker) progress(msg string) {
	if c.OnProgress != nil {
		c.OnProgress(msg)
	}
}

func (c Checker) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// load reads the parts of the snapshot, the ones that failed with their errors.
func (c Checker) load(ctx context.Context, needed map[part]bool, network bool, running map[string]bool) (*snapshot, map[part]error) {
	staleAfter := c.StaleAfter
	if staleAfter <= 0 {
		staleAfter = DefaultStaleAfter
	}
	mark := c.Mark
	mark.Loaders = c.Loaders
	s := &snapshot{now: c.now(), root: c.Root, providerDirs: c.ProviderDirs, staleAfter: staleAfter, backoff: c.Backoff,
		loaders: c.Loaders, fsys: c.FS, mark: mark, providerName: c.ProviderName, bindings: map[[2]string][]storage.Binding{}, network: network, running: running,
		probes: map[string]Probe{}, probed: map[string]ProviderManga{}, chapters: map[int64]chapterResult{}, anilist: map[int64]metadata.Match{}}
	failed := map[part]error{}
	if needed[partLibrary] {
		c.progress("loading the library")
		if err := s.loadLibrary(c.Library); err != nil {
			failed[partLibrary] = err
		} else if c.Statuses != nil {
			if err := c.Statuses(ctx, s.mangas, s.allBindings, network); err != nil {
				s.errors = append(s.errors, "publication statuses: "+err.Error())
			}
		}
	}
	if needed[partWatches] {
		c.progress("loading the watches")
		var err error
		if c.Watches == nil {
			err = errors.New("no watch manager")
		} else {
			s.watches, err = c.Watches.List(ctx)
		}
		if err != nil {
			failed[partWatches] = err
		} else {
			s.watchesLoaded = true
		}
	}
	if needed[partOldIDs] {
		var err error
		if c.OldIDs == nil {
			err = errors.New("not available")
		} else {
			s.oldIDs, err = c.OldIDs()
		}
		if err != nil {
			failed[partOldIDs] = err
		}
	}
	if needed[partAccounts] {
		var err error
		if c.Accounts == nil {
			err = errors.New("not available")
		} else {
			s.accounts, err = c.Accounts()
		}
		if err != nil {
			failed[partAccounts] = err
		}
	}
	if needed[partFS] {
		if failed[partLibrary] != nil {
			failed[partFS] = errors.New("needs the library")
		} else if err := c.loadFS(ctx, s); err != nil {
			failed[partFS] = err
		}
	}
	if needed[partRenames] {
		c.progress("planning the renames")
		if failed[partLibrary] != nil {
			failed[partRenames] = errors.New("needs the library")
		} else if err := c.loadRenames(ctx, s, network); err != nil {
			failed[partRenames] = err
		}
	}
	if needed[partProbes] {
		if failed[partLibrary] != nil {
			failed[partProbes] = errors.New("needs the library")
		} else if err := c.loadProbes(ctx, s); err != nil {
			failed[partProbes] = err
		}
	}
	if needed[partChapters] {
		switch {
		case failed[partLibrary] != nil || failed[partWatches] != nil:
			failed[partChapters] = errors.New("needs the library and the watches")
		default:
			if err := c.loadChapters(ctx, s); err != nil {
				failed[partChapters] = err
			}
		}
	}
	if needed[partSearch] && network && failed[partLibrary] == nil {
		if err := c.loadSearch(ctx, s); err != nil {
			s.errors = append(s.errors, "M1: AniList search: "+err.Error())
		}
	}
	return s, failed
}
