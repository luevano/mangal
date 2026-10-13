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
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
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
	confirm  *confirm.Dialog
	client   *libmangal.Client
	options  libmangal.DownloadOptions

	// results are all the chapters, batch the ones of the running download (all, or the
	// failed ones on retry), both only changed in Update
	results client.Results
	batch   client.Results
	// run counts the downloads, the events of an older one are dropped
	run         int
	downloading bool
	// leaving is the download stopped by leaving the screen, it can go
	leaving bool
	// current is the chapter (in batch) being downloaded, done how many finished
	current, done int
	// attempt is the rate limit retry of the current chapter, 0 if not waiting
	attempt int
	message string
	// status is the provider's latest (e.g. a mirror switch, a rate limit), shown a while
	status   string
	statusAt time.Time
	// pages is the share of the current chapter's pages that are downloaded
	pages float64

	sep    string
	size   base.Size
	styles styles

	open,
	retry,
	stop key.Binding
}

// leaveMsg is leaving the screen confirmed, the download stops.
type leaveMsg struct{}

// statusFor is how long a provider's status is shown.
const statusFor = 15 * time.Second

func New(c *libmangal.Client, chapters []mangadata.Chapter, options libmangal.DownloadOptions) base.State {
	_styles := defaultStyles()
	return &state{
		progress: progress.New(),
		spinner: spinner.New(
			spinner.WithSpinner(base.DotSpinner),
			spinner.WithStyle(style.Normal.Accent),
		),
		viewport: viewport.New(),
		confirm:  confirm.NewDialog(40, color.Warning),
		client:   c,
		results:  client.NewResults(chapters),
		options:  options,
		sep:      _styles.sep.Render(icon.Separator.Raw()),
		message:  "Preparing...",
		styles:   _styles,
		open:     base.KeyOpenDir,
		retry:    base.KeyRetry,
		stop:     util.Bind("stop", "esc"),
	}
}

// running is a download going on that leaving the screen would stop.
func (s *state) running() bool { return s.downloading && !s.leaving }

// Backable is false while downloading: esc asks to stop it.
func (s *state) Backable() bool    { return !s.running() && !s.confirm.Active() }
func (s *state) Inputting() bool   { return s.confirm.Active() }
func (s *state) Title() base.Title { return base.Title{Text: "Download"} }
func (s *state) Status() string    { return "" }

// Keeping implements base.Keeper: the download stops when the screen is left.
func (s *state) Keeping() string {
	if !s.running() {
		return ""
	}
	return "A download is running, " + s.stop.Help().Key + " stops it"
}

func (s *state) KeyMap() help.KeyMap {
	if s.confirm.Active() {
		return s.confirm.KeyMap()
	}
	s.stop.SetEnabled(s.running())
	return base.CombinedKeyMap(util.KeyMap{Short: []key.Binding{s.stop, s.open, s.retry}}, s.viewport.KeyMap)
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
	s.confirm.Resize(size)
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

	c, options := s.client, s.options
	feed := NewFeed(ctx, s.run)
	download := func() tea.Msg {
		release, ok := base.Track(ctx)
		if !ok {
			feed.Close()
			return nil
		}
		go func() {
			defer release()
			defer feed.Close()
			client.Download(feed.Reporting(ctx), c, chapters, options, feed.Send)
		}()
		return nil
	}
	return tea.Batch(download, feed.Listen(), s.Resize(s.size))
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok && s.confirm.Active() {
		return s.confirm.Update(msg)
	}
	switch msg := msg.(type) {
	case leaveMsg:
		// the pop cancels ctx, the download with it
		s.leaving = true
		return base.Back
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, s.stop) && s.running():
			s.confirm.AskWith([]string{"y"}, "Stop", "Stop the download and leave? The chapters downloaded stay.", s.View(), func() tea.Msg { return leaveMsg{} })
			return nil
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
	case EventMsg:
		if msg.Run != s.run {
			return nil
		}
		return tea.Batch(s.onEvent(msg), msg.Next)
	case FinishedMsg:
		if msg.Run != s.run {
			return nil
		}
		s.downloading = false
		s.updateView()
		return tea.Batch(s.Resize(s.size), notifyCmd(ctx, s.batch))
	}
	return s.viewport.Update(msg)
}

func (s *state) onEvent(msg EventMsg) tea.Cmd {
	if msg.Kind == EventStatus {
		s.status, s.statusAt = msg.Status, time.Now()
		return nil
	}
	if msg.Index < 0 || msg.Index >= len(s.batch) {
		return nil
	}
	switch msg.Kind {
	case client.EventStart:
		s.current, s.attempt, s.pages, s.status = msg.Index, 0, 0, ""
		s.message = "Downloading..."
	case client.EventProgress:
		// a rate limited chapter is being tried again
		s.current, s.attempt = msg.Index, 0
		p := msg.Progress
		if p.Total > 0 {
			s.pages = float64(p.Done) / float64(p.Total)
		}
		s.message = capitalize(client.Event{Progress: p}.String())
	case client.EventWait:
		s.current, s.attempt = msg.Index, msg.Attempt
		s.message = fmt.Sprintf("429 Too Many Requests (retry #%d)", msg.Attempt)
		s.timer = timer.NewWithInterval(msg.Wait, time.Second)
		return s.timer.Init()
	case client.EventBusy:
		s.current = msg.Index
		s.message = msg.Busy.Message()
		return base.NotifyWithDuration(s.message, 10*time.Second)
	case client.EventDone:
		*s.batch[msg.Index] = msg.Result
		s.done = msg.Index + 1
		s.attempt, s.pages = 0, 0
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
	if s.confirm.Active() {
		return s.confirm.View()
	}
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
	// it goes out even if the screen is left meanwhile, quitting waits for it
	ctx = base.Background(ctx)
	return func() tea.Msg {
		release, ok := base.Track(ctx)
		if !ok {
			return nil
		}
		defer release()
		if err := notify.SendContext(ctx, results); err != nil {
			return fmt.Errorf("send the notification: %w", err)
		}
		return nil
	}
}
