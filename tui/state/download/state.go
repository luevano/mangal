// Package download is the view of a download job: its place in the queue, its progress,
// then its results. The job runs in the background (tui/jobs), the view can be left.
package download

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	bviewport "charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/luevano/mangal/v5/client/jobs"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	tuijobs "github.com/luevano/mangal/v5/tui/jobs"
	"github.com/luevano/mangal/v5/tui/model/viewport"
	"github.com/luevano/mangal/v5/tui/util"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/skratchdot/open-golang/open"
)

var _ base.State = (*state)(nil)

type state struct {
	progress progress.Model
	spinner  spinner.Model
	viewport *viewport.Model

	id  int64
	job jobs.Job
	// gone is the job no longer listed (removed)
	gone bool

	sep    string
	size   base.Size
	styles styles

	cancel,
	open,
	retry key.Binding
}

// New is the view of the download job with the id.
func New(id int64) base.State {
	_styles := defaultStyles()
	// full blocks, as before bubbles v2 (it fills with half blocks)
	bar := progress.New()
	bar.Full = progress.DefaultFullCharFullBlock
	s := &state{
		progress: bar,
		spinner: spinner.New(
			spinner.WithSpinner(base.DotSpinner),
			spinner.WithStyle(style.Normal.Accent),
		),
		viewport: viewport.New(),
		id:       id,
		sep:      _styles.sep.Render(icon.Separator.Raw()),
		styles:   _styles,
		cancel:   base.KeyCancel,
		open:     base.KeyOpenDir,
		retry:    base.KeyRetry,
	}
	// d cancels here: the pager's half pages keep their ctrl keys only, esc is the
	// global back
	for _, km := range []*bviewport.KeyMap{&s.viewport.Model.KeyMap, s.viewport.KeyMap.Viewport} {
		km.HalfPageDown.SetKeys("ctrl+d")
		km.HalfPageDown.SetHelp("ctrl+d", "½ page down")
		km.HalfPageUp.SetKeys("ctrl+u")
		km.HalfPageUp.SetHelp("ctrl+u", "½ page up")
	}
	s.viewport.KeyMap.Back.SetEnabled(false)
	s.load()
	return s
}

// Key is the job's window.
func (s *state) Key() string { return fmt.Sprintf("download/%d", s.id) }

func (s *state) Backable() bool  { return true }
func (s *state) Inputting() bool { return false }
func (s *state) Title() base.Title {
	return base.Title{Text: "Download"}
}
func (s *state) Status() string { return s.job.Status.String() }

func (s *state) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(util.KeyMap{Short: []key.Binding{s.cancel, s.open, s.retry}}, s.viewport.KeyMap)
}

func (s *state) Subtitle() string {
	down, succ, fail := s.job.Results.GetEach()
	parts := []string{s.styles.accent.Render(s.job.Title)}
	if len(down) != 0 {
		parts = append(parts, s.styles.toDownload.Render(stringutil.Quantify(len(down), "chapter", "chapters")+" to download"))
	}
	if len(succ) != 0 {
		parts = append(parts, s.styles.succeed.Render(stringutil.Quantify(len(succ), "chapter", "chapters")+" downloaded"))
	}
	if len(fail) != 0 {
		parts = append(parts, s.styles.failed.Render(stringutil.Quantify(len(fail), "chapter", "chapters")+" failed"))
	}
	return strings.Join(parts, s.sep)
}

// load takes the job as it is now.
func (s *state) load() {
	job, ok := tuijobs.Manager().Get(s.id)
	if !ok {
		s.gone = true
		s.job.Status = jobs.Cancelled
	} else {
		s.job = job
	}
	active := s.job.Status.Active()
	s.cancel.SetEnabled(active)
	s.open.SetEnabled(len(s.job.Results.Succeed()) != 0)
	s.retry.SetEnabled(!active && !s.gone && (len(s.job.Results.Failed()) != 0 || len(s.job.Results.ToDownload()) != 0))
	s.viewport.SetContent(s.viewDownloaded())
}

func (s *state) Resize(size base.Size) tea.Cmd {
	s.size = size
	s.progress.SetWidth(s.size.Width)
	h := 0
	if s.job.Status.Active() {
		h = lipgloss.Height(s.viewActive())
	}
	return s.viewport.Resize(base.Size{Width: s.size.Width, Height: s.size.Height - h})
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	s.load()
	return tea.Batch(s.spinner.Tick, s.Resize(s.size))
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case base.RestoredMsg:
		s.load()
		return tea.Batch(s.spinner.Tick, s.Resize(s.size))
	case tuijobs.ChangedMsg:
		wasActive := s.job.Status.Active()
		s.load()
		if wasActive != s.job.Status.Active() {
			return s.Resize(s.size)
		}
		return nil
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, s.cancel):
			if tuijobs.Manager().Cancel(s.id) {
				return base.Notify("Cancelling " + s.job.Title)
			}
			return nil
		case key.Matches(msg, s.open):
			return s.openCmd()
		case key.Matches(msg, s.retry):
			if _, err := tuijobs.Manager().Retry(s.id); err != nil {
				return func() tea.Msg { return err }
			}
			s.load()
			return tea.Batch(s.spinner.Tick, s.Resize(s.size))
		}
	case spinner.TickMsg:
		if !s.job.Status.Active() {
			return nil
		}
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(msg)
		return cmd
	}
	return s.viewport.Update(msg)
}

// openCmd opens the directory of the first downloaded chapter.
func (s *state) openCmd() tea.Cmd {
	for _, r := range s.job.Results.Succeed() {
		dir := r.Down.Directory
		return func() tea.Msg {
			if err := open.Start(dir); err != nil {
				return fmt.Errorf("open %s: %w", dir, err)
			}
			return nil
		}
	}
	return nil
}

func (s *state) View() string {
	if s.job.Status.Active() {
		return s.viewActive() + "\n" + s.viewport.View()
	}
	return s.viewport.View()
}
