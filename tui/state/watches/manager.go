// Package watches is the TUI's watches: the watched mangas, their checks and the runs,
// through a watch.Manager (a running `mangal serve`, or the data db directly).
package watches

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/style"
)

// connector gets the Manager, swapped in tests.
type connector func(ctx context.Context) (watch.Manager, error)

// hub is the live events of the checks this process runs, shared by its local services:
// a check started from one screen shows on the others (the watches opened again too).
var hub = sync.OnceValue(watch.NewHub)

// connect is the running `mangal serve` of this config, else the local service.
func connect(ctx context.Context) (watch.Manager, error) {
	m, err := watch.Connect(ctx, notify.Watches())
	if err != nil {
		return nil, err
	}
	if svc, ok := m.(*watch.Service); ok {
		svc.OnError = func(err error) { log.Warn("watch: %s", err) }
		svc.Events = hub()
	}
	return m, nil
}

// Connect is the Manager the screens use.
var Connect connector = connect

// managerName is where the watches are managed, for the subtitles.
func managerName(st watch.Status) string {
	if !st.Server {
		return "local"
	}
	if st.Address == "" {
		return "server"
	}
	return "server at " + st.Address
}

// Set is the watched mangas, for the marks of the manga lists and the jumps to their
// watch.
type Set struct {
	byManga   map[string]watch.Watch
	byLibrary map[int64][]watch.Watch
}

func mangaKey(provider, mangaID string) string {
	return fmt.Sprintf("%s\x00%s", provider, mangaID)
}

// NewSet is the set of the watches.
func NewSet(watches []watch.Watch) Set {
	s := Set{byManga: map[string]watch.Watch{}, byLibrary: map[int64][]watch.Watch{}}
	for _, w := range watches {
		s.byManga[mangaKey(w.Provider, w.MangaID)] = w
		if w.LibraryManga != 0 {
			s.byLibrary[w.LibraryManga] = append(s.byLibrary[w.LibraryManga], w)
		}
	}
	return s
}

// Has is true when the provider's manga is watched.
func (s Set) Has(provider, mangaID string) bool {
	_, ok := s.Find(provider, mangaID)
	return ok
}

// Find is the watch of the provider's manga.
func (s Set) Find(provider, mangaID string) (watch.Watch, bool) {
	w, ok := s.byManga[mangaKey(provider, mangaID)]
	return w, ok
}

// HasLibrary is true when the library manga, or one of its sources, is watched.
func (s Set) HasLibrary(m storage.Manga) bool {
	return len(s.ForLibrary(m)) != 0
}

// ForLibrary are the watches of the library manga: downloading into it, or of one of its
// sources.
func (s Set) ForLibrary(m storage.Manga) []watch.Watch {
	list := slices.Clone(s.byLibrary[m.ID])
	for _, src := range m.Sources {
		w, ok := s.Find(src.Provider, src.MangaID)
		if ok && !slices.ContainsFunc(list, func(o watch.Watch) bool { return o.ID == w.ID }) {
			list = append(list, w)
		}
	}
	return list
}

// Watched loads the set of watched mangas, run it in a base.Load. The marks aren't
// worth an error screen: errors are logged, the set is empty then.
func Watched(ctx context.Context) (Set, error) {
	list, err := watched(ctx)
	if err != nil {
		log.Warn("watched mangas: %s", err)
	}
	return NewSet(list), nil
}

func watched(ctx context.Context) ([]watch.Watch, error) {
	m, err := Connect(ctx)
	if err != nil {
		return nil, err
	}
	return m.List(ctx)
}

// Mark is the mark of a watched manga in the manga lists.
func Mark() string {
	return style.Italic.Accent.Render("(watched)")
}

// Describe is the watch's state in a line: its provider and when it's checked next.
func Describe(w watch.Watch) string {
	next := nextCheck(w, config.Watch.Backoff.Get(), time.Now())
	// a time, not a state (on hold, paused, ...)
	if next == "due" || strings.HasPrefix(next, "in ") {
		next = "next check " + next
	}
	return "watched on " + w.Provider + ": " + next
}
