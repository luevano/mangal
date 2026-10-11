package watches

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/util"
)

func validateSchedule(s string) error {
	_, err := watch.ParseSchedule(s)
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

func formatAnilistID(id int) string {
	if id == 0 {
		return ""
	}
	return strconv.Itoa(id)
}

var _ base.State = (*editState)(nil)

// editState changes a watch's settings, and its manga (the re-link search).
type editState struct {
	manager                        watch.Manager
	w                              watch.Watch
	form                           *form
	manga, schedule, gaps, anilist *field
	relink                         key.Binding
}

// mangaDisplay is the watch's manga for the edit screen.
func mangaDisplay(w watch.Watch) string {
	display := fmt.Sprintf("%s · %s · %s", w.Title, w.Provider, w.MangaID)
	if w.NeedsRelink {
		display += " · " + relinkStyle.Render("needs re-link: "+w.RelinkReason)
	} else if w.ProviderMissing {
		display += " · " + relinkStyle.Render("provider missing")
	}
	return display
}

type savedMsg struct{ notify string }

func newEditState(m watch.Manager, w watch.Watch) *editState {
	s := &editState{
		manager:  m,
		w:        w,
		manga:    actionField("Manga", mangaDisplay(w), "enter: change it (search a provider), the watch keeps its history and settings"),
		relink:   util.Bind("change manga", "enter"),
		schedule: textField("Schedule", w.Schedule, fmt.Sprintf(`cron, e.g. "0 */6 * * *" or @daily; empty for the default (%s)`, config.Watch.Schedule.Get()), validateSchedule),
		gaps:     toggleField("Gaps", w.Gaps, "also download the missing chapters below the last one"),
		anilist:  textField("AniList id", formatAnilistID(w.AnilistID), "the metadata of its downloads, empty for none", validateAnilistID),
	}
	s.form = newForm(s.manga, s.schedule, s.gaps, s.anilist)
	s.form.focus(1)
	return s
}

func (s *editState) Backable() bool  { return true }
func (s *editState) Inputting() bool { return true }
func (s *editState) KeyMap() help.KeyMap {
	s.relink.SetEnabled(s.form.focused() == s.manga)
	return util.KeyMap{Short: append([]key.Binding{s.relink}, s.form.keys()...)}
}
func (s *editState) Title() base.Title             { return base.Title{Text: "Edit " + s.w.Title} }
func (s *editState) Subtitle() string              { return s.w.Provider + " · manga id " + s.w.MangaID }
func (s *editState) Status() string                { return "" }
func (s *editState) Resize(size base.Size) tea.Cmd { return nil }
func (s *editState) Init(ctx context.Context) tea.Cmd {
	return s.form.focus(s.form.current)
}

// options are the changed settings, valid once the form is.
func (s *editState) options() watch.UpdateOptions {
	var opts watch.UpdateOptions
	if sched := strings.TrimSpace(s.schedule.value()); sched != s.w.Schedule {
		opts.Schedule = &sched
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
	case relinkedMsg:
		// the bindings moved with it, the AniList field stays right
		s.w.Provider, s.w.MangaID, s.w.Title = msg.w.Provider, msg.w.MangaID, msg.w.Title
		s.w.NeedsRelink, s.w.RelinkReason, s.w.ProviderMissing = msg.w.NeedsRelink, msg.w.RelinkReason, msg.w.ProviderMissing
		s.manga.display = mangaDisplay(s.w)
		return nil
	case tea.KeyMsg:
		if base.Busy(ctx) {
			return nil
		}
		if s.form.focused() == s.manga && key.Matches(msg, s.relink) {
			m, w := s.manager, s.w
			return func() tea.Msg { return newRelinkState(m, w) }
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
