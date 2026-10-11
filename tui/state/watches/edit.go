package watches

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

// parseInterval is a check interval: empty or 0 for the default, a duration (12h) or
// days (7d), at least a minute.
func parseInterval(s string) (time.Duration, error) {
	if s == "" || s == "0" {
		return 0, nil
	}
	var d time.Duration
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.ParseFloat(days, 64)
		if err != nil {
			return 0, fmt.Errorf("%q isn't a number of days", s)
		}
		d = time.Duration(n * float64(24*time.Hour))
	} else {
		var err error
		if d, err = time.ParseDuration(s); err != nil {
			return 0, fmt.Errorf("%q isn't a duration (e.g. 12h, 7d)", s)
		}
	}
	if d < time.Minute {
		return 0, errors.New("at least 1m, or empty for watch.interval")
	}
	return d, nil
}

func validateInterval(s string) error {
	_, err := parseInterval(s)
	return err
}

// parseAnilistID is an AniList id, empty or 0 for none.
func parseAnilistID(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	id, err := strconv.Atoi(s)
	if err != nil || id < 0 {
		return 0, fmt.Errorf("%q isn't an AniList id (the number in its URL)", s)
	}
	return id, nil
}

func validateAnilistID(s string) error {
	_, err := parseAnilistID(s)
	return err
}

func formatInterval(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	return d.String()
}

func formatAnilistID(id int) string {
	if id == 0 {
		return ""
	}
	return strconv.Itoa(id)
}

var _ base.State = (*editState)(nil)

// editState changes a watch's settings.
type editState struct {
	manager                 watch.Manager
	w                       watch.Watch
	form                    *form
	interval, gaps, anilist *field
}

type savedMsg struct{ notify string }

func newEditState(m watch.Manager, w watch.Watch) *editState {
	s := &editState{
		manager:  m,
		w:        w,
		interval: textField("Interval", formatInterval(time.Duration(w.Interval)), "e.g. 12h or 7d, empty for watch.interval", validateInterval),
		gaps:     toggleField("Gaps", w.Gaps, "also download the missing chapters below the last one"),
		anilist:  textField("AniList id", formatAnilistID(w.AnilistID), "the metadata of its downloads, empty for none", validateAnilistID),
	}
	s.form = newForm(s.interval, s.gaps, s.anilist)
	return s
}

func (s *editState) Backable() bool                { return true }
func (s *editState) Inputting() bool               { return true }
func (s *editState) KeyMap() help.KeyMap           { return util.KeyMap{Short: s.form.keys()} }
func (s *editState) Title() base.Title             { return base.Title{Text: "Edit " + s.w.Title} }
func (s *editState) Subtitle() string              { return s.w.Provider + " · manga id " + s.w.MangaID }
func (s *editState) Status() string                { return "" }
func (s *editState) Resize(size base.Size) tea.Cmd { return nil }
func (s *editState) Init(ctx context.Context) tea.Cmd {
	return s.form.focus(0)
}

// options are the changed settings, valid once the form is.
func (s *editState) options() watch.UpdateOptions {
	var opts watch.UpdateOptions
	if d, _ := parseInterval(s.interval.value()); d != time.Duration(s.w.Interval) {
		opts.Interval = &d
	}
	if s.gaps.on != s.w.Gaps {
		gaps := s.gaps.on
		opts.Gaps = &gaps
	}
	if id, _ := parseAnilistID(s.anilist.value()); id != s.w.AnilistID {
		opts.AnilistID = &id
	}
	return opts
}

func (s *editState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case savedMsg:
		return tea.Sequence(base.Back, base.Notify(msg.notify))
	case tea.KeyMsg:
		if base.Busy(ctx) {
			return nil
		}
	}
	submitted, cmd := s.form.Update(msg)
	if !submitted {
		return cmd
	}
	opts := s.options()
	if opts == (watch.UpdateOptions{}) {
		return tea.Sequence(base.Back, base.Notify("Nothing changed"))
	}
	m, w := s.manager, s.w
	return base.Load(ctx, "Saving", func(ctx context.Context) (savedMsg, error) {
		_, err := m.Update(ctx, w.ID, opts)
		return savedMsg{notify: "saved " + w.Title}, err
	}, func(msg savedMsg) tea.Msg { return msg })
}

func (s *editState) View() string { return s.form.View() }
