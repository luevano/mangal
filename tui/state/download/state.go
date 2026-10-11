package download

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/timer"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/viewport"
	"github.com/luevano/mangal/v5/util/chapter"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

var _ base.State = (*state)(nil)

type downloadState uint8

const (
	dSUninitialized downloadState = iota + 1
	dSDownloading
	dSDownloaded
)

// state implements base.state.
type state struct {
	progress progress.Model
	spinner  spinner.Model
	timer    timer.Model
	viewport *viewport.Model
	client   *libmangal.Client
	chapters chapter.Chapters
	options  libmangal.DownloadOptions

	downloading downloadState
	currentIdx  int
	toDownload  chapter.Chapters

	retryCount,
	maxRetries int
	retrying bool

	sep,
	message string

	size   base.Size
	styles styles
	keyMap keyMap
}

// Backable implements base.State.
func (s *state) Backable() bool {
	return true
}

// KeyMap implements base.State.
func (s *state) KeyMap() help.KeyMap {
	return s.keyMap
}

// Title implements base.State.
func (s *state) Title() base.Title {
	return base.Title{Text: "Download"}
}

// Subtitle implements base.State.
func (s *state) Subtitle() string {
	down, succ, fail := s.chapters.GetEach()
	var sb strings.Builder
	sb.Grow(50)

	renDown := len(down) != 0
	renSucc := len(succ) != 0
	renFail := len(fail) != 0

	if renDown {
		q := stringutil.Quantify(len(down), "chapter", "chapters")
		sb.WriteString(s.styles.toDownload.Render(q + " to download"))
		if renSucc || renFail {
			sb.WriteString(s.sep)
		}
	}
	if renSucc {
		q := stringutil.Quantify(len(succ), "chapter", "chapters")
		sb.WriteString(s.styles.succeed.Render(q + " downloaded"))
		if renFail {
			sb.WriteString(s.sep)
		}
	}
	if renFail {
		q := stringutil.Quantify(len(fail), "chapter", "chapters")
		sb.WriteString(s.styles.failed.Render(q + " failed"))
	}
	return sb.String()
}

// Status implements base.State.
func (s *state) Status() string {
	return ""
}

// Resize implements base.State.
func (s *state) Resize(size base.Size) tea.Cmd {
	s.size = size
	s.progress.Width = s.size.Width
	h := 0
	if s.downloading != dSDownloaded {
		h = lipgloss.Height(s.viewDownloading())
	}
	return s.viewport.Resize(s.size.Width, s.size.Height-h)
}

// Init implements base.State.
func (s *state) Init(ctx context.Context) tea.Cmd {
	s.client.Logger().SetOnLog(func(format string, a ...any) {
		s.message = fmt.Sprintf(format, a...)
		// TODO: add option for "verbose" so it logs pages progress?
		if !strings.HasPrefix(format, "page") {
			log.Log(format, a...)
		}
	})

	s.updateKeybinds()
	return tea.Sequence(
		s.viewport.Init(),
		s.spinner.Tick,
		s.startDownloadCmd,
	)
}

// Update implements base.State.
func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, s.keyMap.open):
			return s.openCmd
		case key.Matches(msg, s.keyMap.retry):
			return s.retryCmd()
		}
	case spinner.TickMsg:
		spinner, cmd := s.spinner.Update(msg)
		s.spinner = spinner
		return cmd
	case timer.TickMsg:
		timer, cmd := s.timer.Update(msg)
		s.timer = timer
		if !msg.Timeout {
			return cmd
		}

		s.retrying = false
		// since the currentIdx is not updated,
		// just resend the normal download chapter cmd
		return tea.Sequence(
			cmd,
			s.downloadChapterCmd(ctx),
		)
	case nextChapterMsg:
		s.updateKeybinds()
		s.viewport.SetContent(s.viewDownloaded())
		return s.downloadChapterCmd(ctx)
	case retryChapterMsg:
		s.retrying = true
		s.timer.Timeout = msg.After
		return s.timer.Init()
	case downloadCompletedMsg:
		s.downloading = dSDownloaded
		s.updateKeybinds()
		s.viewport.SetContent(s.viewDownloaded())
		return s.Resize(s.size)
	}
	return s.viewport.Update(msg)
}

// View implements base.State.
func (s *state) View() string {
	if s.downloading == dSDownloading {
		return s.viewDownloading() + "\n" + s.viewport.View()
	}
	return s.viewport.View()
}
