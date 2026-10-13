package watches

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/library"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/style"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

type item struct {
	s *listState
	w watch.Watch
	// last is its latest check, nil if none in the recent runs
	last    *storage.WatchRun
	backoff int
	now     time.Time
}

func (i *item) FilterValue() string { return i.w.Title + " " + i.w.Provider }
func (i *item) ItemKey() string     { return strconv.FormatInt(i.w.ID, 10) }

func (i *item) Title() string {
	// with a filter on, the list highlights the matches by rune position in the title: it
	// has to be plain and start with the watch's title
	plain := i.s != nil && i.s.List != nil && !i.s.List.Unfiltered()
	render := func(st lipgloss.Style, s string) string {
		if plain {
			return s
		}
		return st.Render(s)
	}
	title := i.w.Title
	if !i.w.Enabled {
		title += " " + render(pausedStyle, "(paused)")
	}
	if i.w.NeedsRelink {
		title += " " + render(relinkStyle, "(needs re-link)")
	}
	if i.w.LibraryProblem() != "" {
		title += " " + render(libraryStyle, "(not in library)")
	}
	if _, ok := i.checking(); ok {
		frame := spinnerFrames[i.s.frame%len(spinnerFrames)]
		if plain {
			title += " " + frame
		} else {
			title = style.Normal.Accent.Render(frame) + " " + title
		}
	}
	return title
}

// checking is what the watch's check does now, false when it isn't being checked.
func (i *item) checking() (liveWatch, bool) {
	if i.s == nil {
		return liveWatch{}, false
	}
	return i.s.liveOf(i.w)
}

func (i *item) Description() string {
	w := i.w
	if lw, ok := i.checking(); ok {
		state := "checking"
		if lw.state != "" && lw.state != "checking" {
			state += ": " + lw.state
		}
		if !lw.since.IsZero() {
			state += " (" + short(max(0, i.now.Sub(lw.since))) + ")"
		}
		return w.Provider + " · last " + formatNumber(w.LastDownloaded) + " · " + state
	}
	if w.NeedsRelink {
		return w.Provider + " · " + w.MangaID + " · last " + formatNumber(w.LastDownloaded) + " · needs re-link: " + w.RelinkReason
	}
	parts := []string{w.Provider}
	if status := library.StatusLabel(w.Status); status != "" {
		parts = append(parts, status)
	}
	parts = append(parts,
		"last "+formatNumber(w.LastDownloaded),
		"checked "+ago(w.LastChecked, i.now),
		"next "+nextCheck(w, i.backoff, i.now),
	)
	switch {
	case i.last != nil:
		parts = append(parts, result(*i.last))
	case w.LastError != "":
		parts = append(parts, "error")
	}
	if w.Failures > 0 {
		parts = append(parts, stringutil.Quantify(w.Failures, "failure", "failures"))
	}
	return strings.Join(parts, " · ")
}
