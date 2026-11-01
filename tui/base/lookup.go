package base

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal/mangadata"
)

const lookupNoticeDuration = 3 * time.Second

// lookups are the running metadata searches by manga, closed once applied. Only touched in
// Update: the search itself is what's in the background.
var lookups = map[mangadata.Manga]chan struct{}{}

// LookupDoneMsg is a manga's metadata search over and applied, it also reaches the shown
// State (e.g. its marks depend on the metadata).
type LookupDoneMsg struct {
	Manga  mangadata.Manga
	apply  func()
	notice string
	done   chan struct{}
}

// LookupMetadata searches the manga's metadata past the State that starts it (a slow source
// doesn't hold up the screens) with label shown meanwhile, nil if one is running. Its work
// returns what sets the result on the manga, run in Update, and a notice.
func LookupMetadata(ctx context.Context, manga mangadata.Manga, label string, work func(context.Context) (apply func(), notice string)) tea.Cmd {
	if lookups[manga] != nil {
		return nil
	}
	done := make(chan struct{})
	lookups[manga] = done
	return LoadWithStatus(Background(ctx), label, func(ctx context.Context, _ func(string)) (LookupDoneMsg, error) {
		apply, notice := work(ctx)
		return LookupDoneMsg{Manga: manga, apply: apply, notice: notice, done: done}, nil
	}, func(msg LookupDoneMsg) tea.Msg { return msg })
}

// PendingLookup is the manga's running metadata search, closed once its result is set, nil
// if none: what uses the metadata (a download's strict check) waits for it.
func PendingLookup(manga mangadata.Manga) <-chan struct{} {
	if done := lookups[manga]; done != nil {
		return done
	}
	return nil
}

// Apply sets the result, what waits for it goes on. Only in Update.
func (msg LookupDoneMsg) Apply() {
	delete(lookups, msg.Manga)
	msg.apply()
	close(msg.done)
}

func (m *model) onLookupDone(msg LookupDoneMsg) tea.Cmd {
	msg.Apply()
	var notice tea.Cmd
	if msg.notice != "" {
		notice = NotifyWithDuration(msg.notice, lookupNoticeDuration)
	}
	return tea.Batch(notice, m.state.Update(m.ctx, msg))
}
