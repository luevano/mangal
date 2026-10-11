// Package download is the download of several chapters, with its progress.
package download

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/timer"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/notify"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/viewport"
	"github.com/luevano/mangal/v5/tui/util"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/skratchdot/open-golang/open"
)

var _ base.State = (*state)(nil)

type state struct {
	progress progress.Model
	spinner  spinner.Model
	timer    timer.Model
	viewport *viewport.Model
	client   *libmangal.Client
	options  libmangal.DownloadOptions

	// results are all the chapters, batch the ones of the running download (all, or the
	// failed ones on retry), both only changed in Update
	results client.Results
	batch   client.Results
	// run counts the downloads, the events of an older one are dropped
	run         int
	downloading bool
	// current is the chapter (in batch) being downloaded, done how many finished
	current, done int
	// attempt is the rate limit retry of the current chapter, 0 if not waiting
	attempt int
	message string

	sep    string
	size   base.Size
	styles styles

	open,
	retry key.Binding
}

// eventMsg is an event of the run's download, with a copy of the chapter's result.
type eventMsg struct {
	run     int
	kind    client.EventKind
	index   int
	result  client.Result
	attempt int
	wait    time.Duration
	// next waits for the following event
	next tea.Cmd
}

type finishedMsg struct {
	run int
}

func New(c *libmangal.Client, chapters []mangadata.Chapter, options libmangal.DownloadOptions) base.State {
	_styles := defaultStyles()
	return &state{
		progress: progress.New(),
		spinner: spinner.New(
			spinner.WithSpinner(base.DotSpinner),
			spinner.WithStyle(style.Normal.Accent),
		),
		viewport: viewport.New(),
		client:   c,
		results:  client.NewResults(chapters),
		options:  options,
		sep:      _styles.sep.Render(icon.Separator.Raw()),
		message:  "Preparing...",
		styles:   _styles,
		open:     util.Bind("open directory", "o"),
		retry:    util.Bind("retry", "r"),
	}
}

func (s *state) Backable() bool    { return true }
func (s *state) Inputting() bool   { return false }
func (s *state) Title() base.Title { return base.Title{Text: "Download"} }
func (s *state) Status() string    { return "" }

func (s *state) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(util.KeyMap{Short: []key.Binding{s.open, s.retry}}, s.viewport.KeyMap)
}

func (s *state) Subtitle() string {
	down, succ, fail := s.results.GetEach()
	var parts []string
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

func (s *state) Resize(size base.Size) tea.Cmd {
	s.size = size
	s.progress.Width = s.size.Width
	h := 0
	if s.downloading {
		h = lipgloss.Height(s.viewDownloading())
	}
	return s.viewport.Resize(base.Size{Width: s.size.Width, Height: s.size.Height - h})
}

func (s *state) Init(ctx context.Context) tea.Cmd {
	return tea.Batch(s.spinner.Tick, s.start(ctx, s.results.ToDownload()))
}

// start downloads the batch in the background, its events come back as msgs.
func (s *state) start(ctx context.Context, batch client.Results) tea.Cmd {
	s.run++
	s.batch = batch
	s.downloading = true
	s.current, s.done, s.attempt = 0, 0, 0
	chapters := make([]mangadata.Chapter, len(batch))
	for i, r := range batch {
		r.Down, r.Err = nil, nil
		chapters[i] = r.Chapter
	}
	s.updateView()

	run, c, options := s.run, s.client, s.options
	events := make(chan eventMsg, 8)
	download := func() tea.Msg {
		release, ok := base.Track(ctx)
		if !ok {
			close(events)
			return nil
		}
		go func() {
			defer release()
			defer close(events)
			client.Download(ctx, c, chapters, options, func(e client.Event) {
				msg := eventMsg{run: run, kind: e.Kind, index: e.Index, result: *e.Result, attempt: e.Attempt, wait: e.Wait}
				select {
				case events <- msg:
				case <-ctx.Done():
				}
			})
		}()
		return nil
	}
	return tea.Batch(download, listen(run, events), s.Resize(s.size))
}

// listen waits for the next event of the run.
func listen(run int, events <-chan eventMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return finishedMsg{run: run}
		}
		msg.next = listen(run, events)
		return msg
	}
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, s.open):
			return s.openCmd()
		case key.Matches(msg, s.retry):
			return s.start(ctx, s.results.Failed())
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(msg)
		return cmd
	case timer.TickMsg, timer.StartStopMsg:
		var cmd tea.Cmd
		s.timer, cmd = s.timer.Update(msg)
		return cmd
	case eventMsg:
		if msg.run != s.run {
			return nil
		}
		return tea.Batch(s.onEvent(msg), msg.next)
	case finishedMsg:
		if msg.run != s.run {
			return nil
		}
		s.downloading = false
		s.updateView()
		return tea.Batch(s.Resize(s.size), notifyCmd(ctx, s.batch))
	}
	return s.viewport.Update(msg)
}

func (s *state) onEvent(msg eventMsg) tea.Cmd {
	if msg.index < 0 || msg.index >= len(s.batch) {
		return nil
	}
	switch msg.kind {
	case client.EventStart:
		s.current, s.attempt = msg.index, 0
		s.message = "Downloading..."
	case client.EventWait:
		s.current, s.attempt = msg.index, msg.attempt
		s.message = fmt.Sprintf("429 Too Many Requests (retry #%d)", msg.attempt)
		s.timer = timer.NewWithInterval(msg.wait, time.Second)
		return s.timer.Init()
	case client.EventDone:
		*s.batch[msg.index] = msg.result
		s.done = msg.index + 1
		s.attempt = 0
		s.updateView()
	}
	return nil
}

func (s *state) updateView() {
	_, succ, fail := s.results.GetEach()
	s.open.SetEnabled(len(succ) != 0 && !s.downloading)
	s.retry.SetEnabled(len(fail) != 0 && !s.downloading)
	s.viewport.SetContent(s.viewDownloaded())
}

// openCmd opens the directory of the first downloaded chapter.
func (s *state) openCmd() tea.Cmd {
	for _, r := range s.results.Succeed() {
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
	if s.downloading {
		return s.viewDownloading() + "\n" + s.viewport.View()
	}
	return s.viewport.View()
}

// notifyCmd sends the download's notification when notification.tui is on.
func notifyCmd(ctx context.Context, batch client.Results) tea.Cmd {
	if !config.Notification.TUI.Get() || len(batch) == 0 {
		return nil
	}
	// copies: a retry resets the failed ones while this runs
	results := make(client.Results, len(batch))
	for i, r := range batch {
		c := *r
		results[i] = &c
	}
	return func() tea.Msg {
		if err := notify.SendContext(ctx, results); err != nil {
			return fmt.Errorf("send the notification: %w", err)
		}
		return nil
	}
}
