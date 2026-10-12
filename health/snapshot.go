package health

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/storage"
)

// snapshot is the data the checks look at, loaded once.
type snapshot struct {
	now          time.Time
	root         string
	providerDirs bool
	staleAfter   time.Duration
	backoff      int
	loaders      []libmangal.ProviderLoader

	mangas []storage.Manga
	// numbers are the downloaded chapter numbers by library manga, sorted
	numbers map[int64][]float32
	// bindings are by provider + manga id, allBindings every one
	bindings    map[[2]string][]storage.Binding
	allBindings []storage.Binding
	// picks are the links to pick (not dismissed)
	picks []storage.LinkPick

	watches       []watch.Watch
	watchesLoaded bool

	oldIDs   []storage.OldID
	accounts Accounts

	// network is true when the network checks run, newChapters are the unwatched mangas'
	// new chapters on their source
	network     bool
	newChapters map[int64]newChapters

	// errors are what didn't load without skipping a check, and what a check couldn't do
	errors []string
}

func (s *snapshot) loadLibrary(lib storage.Library) error {
	var err error
	if s.mangas, err = lib.Mangas(); err != nil {
		return err
	}
	if s.numbers, err = lib.ChapterNumbers(); err != nil {
		return err
	}
	if s.allBindings, err = (storage.Bindings{DB: lib.DB}).List("", ""); err != nil {
		return err
	}
	if s.picks, err = lib.LinkPicks(); err != nil {
		return err
	}
	for _, b := range s.allBindings {
		k := [2]string{b.Provider, b.MangaID}
		s.bindings[k] = append(s.bindings[k], b)
	}
	return nil
}

// installed is true when a loader has the provider id (an old id too).
func (s *snapshot) installed(provider string) bool {
	_, _, ok := libmangal.FindLoader(s.loaders, provider)
	return ok
}

// providerInfo is the provider's info, just its id when it isn't installed.
func (s *snapshot) providerInfo(provider string) libmangal.ProviderInfo {
	if l, _, ok := libmangal.FindLoader(s.loaders, provider); ok {
		return l.Info()
	}
	return libmangal.ProviderInfo{ID: provider, Name: provider}
}

// watchesOf are the watches downloading into the library manga: linked to it, or of one
// of its sources.
func (s *snapshot) watchesOf(m storage.Manga) []watch.Watch {
	var out []watch.Watch
	for _, w := range s.watches {
		if s.watchedManga(w) == m.ID {
			out = append(out, w)
		}
	}
	return out
}

// watchedManga is the library manga of the watch, 0 when none is known.
func (s *snapshot) watchedManga(w watch.Watch) int64 {
	if w.LibraryManga != 0 {
		return w.LibraryManga
	}
	for _, m := range s.mangas {
		for _, src := range m.Sources {
			if src.Provider == w.Provider && src.MangaID == w.MangaID {
				return m.ID
			}
		}
	}
	return 0
}

// sourceBindings are the bindings of the manga's sources, the current source's first.
func (s *snapshot) sourceBindings(m storage.Manga) []storage.Binding {
	var out []storage.Binding
	for _, src := range m.Sources {
		out = append(out, s.bindings[[2]string{src.Provider, src.MangaID}]...)
	}
	return out
}

// rel is the path relative to the download directory when it's in it.
func (s *snapshot) rel(path string) string {
	if s.root == "" {
		return path
	}
	if r, err := filepath.Rel(s.root, path); err == nil && r != "." && !strings.HasPrefix(r, "..") {
		return r
	}
	return path
}

func watchSubject(w watch.Watch) Subject {
	return Subject{Kind: "watch", ID: w.ID, Name: fmt.Sprintf("%s (%s)", w.Title, w.Provider)}
}

func mangaSubject(m storage.Manga) Subject {
	return Subject{Kind: "manga", ID: m.ID, Name: m.Title}
}

// quote quotes a command argument for a shell when it needs it.
func quote(arg string) string {
	if arg != "" && !strings.ContainsFunc(arg, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:/@+=,", r))
	}) {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}
