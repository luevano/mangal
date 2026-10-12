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
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
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
	// Kind is watch, manga (a library manga), provider or account.
	Kind string `json:"kind"`
	// ID is the watch's or library manga's id, 0 for the others.
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name"`
}

func (s Subject) String() string {
	switch {
	case s.Kind == "account":
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
	// Network is true when the network checks ran (none exist yet).
	Network bool `json:"network"`
	// Errors are the checks that couldn't run (their data didn't load, they failed).
	Errors []string `json:"errors,omitempty"`
}

// Options of a run.
type Options struct {
	// Network also runs the checks that make requests (none exist yet).
	Network bool
	// Only runs these categories or checks (ids), all when empty.
	Only []string
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
	// Now is time.Now when nil.
	Now func() time.Time
	// OnProgress is told what's loading, may be nil.
	OnProgress func(string)
}

// part is a piece of the snapshot some checks need.
type part int

const (
	partLibrary part = iota
	partWatches
	partOldIDs
	partAccounts
)

var partNames = map[part]string{partLibrary: "the library", partWatches: "the watches", partOldIDs: "the old provider ids", partAccounts: "the accounts"}

// check is one check over the snapshot.
type check struct {
	id       string
	category Category
	needs    []part
	// wants are loaded too, the check runs without them
	wants []part
	run   func(s *snapshot) []Finding
}

// selected are the checks opts.Only picks, an error for a name that's neither a category
// nor a check.
func selected(only []string) ([]check, error) {
	if len(only) == 0 {
		return checks, nil
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
		if pick[string(c.category)] || pick[strings.ToLower(c.id)] {
			out = append(out, c)
		}
	}
	return out, nil
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
	run, err := selected(opts.Only)
	if err != nil {
		return report, err
	}
	needed := map[part]bool{}
	for _, ch := range run {
		for _, p := range slices.Concat(ch.needs, ch.wants) {
			needed[p] = true
		}
	}
	s, failed := c.load(ctx, needed)
	if err := ctx.Err(); err != nil {
		return report, err
	}
	for p := partLibrary; p <= partAccounts; p++ {
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
	sortFindings(report.Findings)
	for _, f := range report.Findings {
		switch f.Severity {
		case Error:
			report.Counts.Error++
		case Warning:
			report.Counts.Warning++
		default:
			report.Counts.Info++
		}
		report.Counts.ByCategory[f.Category]++
	}
	return report, nil
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
func (c Checker) load(ctx context.Context, needed map[part]bool) (*snapshot, map[part]error) {
	staleAfter := c.StaleAfter
	if staleAfter <= 0 {
		staleAfter = DefaultStaleAfter
	}
	s := &snapshot{now: c.now(), root: c.Root, providerDirs: c.ProviderDirs, staleAfter: staleAfter, backoff: c.Backoff,
		loaders: c.Loaders, bindings: map[[2]string][]storage.Binding{}}
	failed := map[part]error{}
	if needed[partLibrary] {
		c.progress("loading the library")
		if err := s.loadLibrary(c.Library); err != nil {
			failed[partLibrary] = err
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
	return s, failed
}
