package watches

import (
	"context"
	"errors"
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/tui/base"
)

// NewDetails is the screen of the watch, e.g. from the manga lists' watched marks.
func NewDetails(w watch.Watch) base.State {
	return newDetailsState(nil, w, config.Watch.Backoff.Get())
}

// LibraryScreen is the library screen of a library manga, set by the library package
// (it imports this one).
var LibraryScreen func(m storage.Manga) (base.State, error)

// findLibrary is the library manga the watch downloads into: the one it's linked to,
// else the one of its provider's manga.
var findLibrary = func(ctx context.Context, w watch.Watch) (storage.Manga, bool, error) {
	lib, err := base.Library()
	if err != nil {
		return storage.Manga{}, false, err
	}
	if w.LibraryManga != 0 {
		m, err := lib.Manga(ctx, w.LibraryManga)
		if err == nil || !errors.Is(err, core.ErrNotFound) {
			return m, err == nil, err
		}
	}
	return lib.MangaBySource(ctx, w.Provider, w.MangaID)
}

// libraryCmd opens the library manga of the watch, or says it has none.
func libraryCmd(ctx context.Context, w watch.Watch) tea.Cmd {
	screen := LibraryScreen
	if screen == nil {
		return base.Notify("No library screen")
	}
	return base.Load(ctx, "Looking up "+w.Title+" in the library", func(ctx context.Context) (tea.Msg, error) {
		return libraryScreen(ctx, screen, w)
	}, func(msg tea.Msg) tea.Msg { return msg })
}

// libraryScreen is the library screen of the watch's library manga, or a notification
// that it has none.
func libraryScreen(ctx context.Context, screen func(storage.Manga) (base.State, error), w watch.Watch) (tea.Msg, error) {
	m, found, err := findLibrary(ctx, w)
	if err != nil {
		return nil, fmt.Errorf("library manga of %q: %w", w.Title, err)
	}
	if !found {
		if p := w.LibraryProblem(); p != "" {
			return base.NotificationMsg{Message: w.Title + ": " + p}, nil
		}
		return base.NotificationMsg{Message: w.Title + " isn't in the library, nothing of it downloaded yet"}, nil
	}
	return screen(m)
}

// JumpCmd opens the watch of a manga, the first enabled one of several (a library manga
// can be watched on several providers); not watched says so, with what adds one.
func JumpCmd(list []watch.Watch, add string) tea.Cmd {
	if len(list) == 0 {
		msg := "Not watched"
		if add != "" {
			msg += ", " + add
		}
		return base.Notify(msg)
	}
	w := list[0]
	if i := slices.IndexFunc(list, func(w watch.Watch) bool { return w.Enabled }); i >= 0 {
		w = list[i]
	}
	return func() tea.Msg { return NewDetails(w) }
}
