package watches

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/huhform"
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

func validateSkip(s string) error {
	_, err := watch.ParseNumbers(s)
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
	form                           *huhform.Form
	manga, schedule, gaps, anilist *huhform.Field
	skip                           *huhform.Field
}

// mangaDisplay is the watch's manga for the edit screen.
func mangaDisplay(w watch.Watch) string {
	display := fmt.Sprintf("%s · %s · %s", w.Title, w.Provider, w.MangaID)
	if w.NeedsRelink {
		display += " · " + relinkStyle.Render("needs re-link: "+w.RelinkReason)
	} else if w.ProviderMissing {
		display += " · " + relinkStyle.Render("provider missing")
	}
	if p := w.LibraryProblem(); p != "" {
		display += " · " + libraryStyle.Render("not in library: "+p)
	}
	return display
}

type savedMsg struct{ notify string }

func newEditState(m watch.Manager, w watch.Watch) *editState {
	s := &editState{
		manager:  m,
		w:        w,
		schedule: huhform.Text("Schedule", w.Schedule, fmt.Sprintf(`cron, e.g. "0 */6 * * *" or @daily; empty for the default (%s)`, config.Watch.Schedule.Get()), validateSchedule),
		gaps:     huhform.Toggle("Gaps", w.Gaps, "also download the missing chapters below the last one"),
		anilist:  huhform.Text("AniList id", formatAnilistID(w.AnilistID), "the metadata of its downloads, empty for none", validateAnilistID),
		skip:     huhform.Text("Skip", watch.FormatNumbers(w.Skip), "chapter numbers never to download (fake or broken ones), e.g. 189.5, 190; remove one to get it again", validateSkip),
	}
	s.manga = huhform.Action("Manga", mangaDisplay(w), "enter: change it (search a provider), the watch keeps its history and settings",
		util.Bind("change manga", "enter"), s.relink)
	s.form = huhform.New(s.manga, s.schedule, s.gaps, s.anilist, s.skip)
	s.form.Focus(1)
	return s
}

// relink opens the re-link search of the watch's manga.
func (s *editState) relink() tea.Cmd {
	m, w := s.manager, s.w
	return func() tea.Msg { return newRelinkState(m, w) }
}

func (s *editState) Backable() bool                { return true }
func (s *editState) Inputting() bool               { return true }
func (s *editState) KeyMap() help.KeyMap           { return util.KeyMap{Short: s.form.Keys()} }
func (s *editState) Key() string                   { return fmt.Sprintf("watch-edit/%d", s.w.ID) }
func (s *editState) Title() base.Title             { return base.Title{Text: "Edit " + s.w.Title} }
func (s *editState) Subtitle() string              { return s.w.Provider + " · manga id " + s.w.MangaID }
func (s *editState) Status() string                { return "" }
func (s *editState) Resize(size base.Size) tea.Cmd { return nil }
func (s *editState) Init(ctx context.Context) tea.Cmd {
	return s.form.Focus(s.form.Current())
}

// options are the changed settings, valid once the form is.
func (s *editState) options() watch.UpdateOptions {
	var opts watch.UpdateOptions
	if sched := strings.TrimSpace(s.schedule.Value()); sched != s.w.Schedule {
		opts.Schedule = &sched
	}
	if s.gaps.On() != s.w.Gaps {
		gaps := s.gaps.On()
		opts.Gaps = &gaps
	}
	if id, _ := parseAnilistID(s.anilist.Value()); id != s.w.AnilistID {
		opts.AnilistID = &id
	}
	if skip, _ := watch.ParseNumbers(s.skip.Value()); !slices.Equal(watch.AddNumbers(nil, skip), s.w.Skip) {
		skip = watch.AddNumbers(nil, skip)
		if skip == nil {
			skip = []float32{}
		}
		opts.Skip = &skip
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
		s.manga.SetDisplay(mangaDisplay(s.w))
		return nil
	case tea.KeyPressMsg:
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
