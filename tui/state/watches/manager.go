// Package watches is the TUI's watches: the watched mangas, their checks and the runs,
// through a watch.Manager (a running `mangal serve`, or the data db directly).
package watches

import (
	"context"
	"fmt"

	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/style"
)

// connector gets the Manager, swapped in tests.
type connector func(ctx context.Context) (watch.Manager, error)

// connect is the running `mangal serve` of this config, else the local service.
func connect(ctx context.Context) (watch.Manager, error) {
	m, err := watch.Connect(ctx, notify.Watches())
	if err != nil {
		return nil, err
	}
	if svc, ok := m.(*watch.Service); ok {
		svc.OnError = func(err error) { log.Warn("watch: %s", err) }
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

// Set is the watched mangas, for the marks of the manga lists.
type Set struct {
	byManga   map[string]bool
	byLibrary map[int64]bool
}

func mangaKey(provider, mangaID string) string {
	return fmt.Sprintf("%s\x00%s", provider, mangaID)
}

// NewSet is the set of the watches.
func NewSet(watches []watch.Watch) Set {
	s := Set{byManga: map[string]bool{}, byLibrary: map[int64]bool{}}
	for _, w := range watches {
		s.byManga[mangaKey(w.Provider, w.MangaID)] = true
		if w.LibraryManga != 0 {
			s.byLibrary[w.LibraryManga] = true
		}
	}
	return s
}

// Has is true when the provider's manga is watched.
func (s Set) Has(provider, mangaID string) bool {
	return s.byManga[mangaKey(provider, mangaID)]
}

// HasLibrary is true when the library manga, or one of its sources, is watched.
func (s Set) HasLibrary(m storage.Manga) bool {
	if s.byLibrary[m.ID] {
		return true
	}
	for _, src := range m.Sources {
		if s.Has(src.Provider, src.MangaID) {
			return true
		}
	}
	return false
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
