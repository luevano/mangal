// Package downloads is the Downloads screen: the background jobs (downloads and watch
// checks) by provider, with their queues, and the finished ones.
package downloads

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/mangal/v5/client/jobs"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	tuijobs "github.com/luevano/mangal/v5/tui/jobs"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/state/download"
	"github.com/luevano/mangal/v5/tui/state/watches"
	"github.com/luevano/mangal/v5/tui/util"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

var _ base.State = (*state)(nil)

type item struct {
	job jobs.Job
	// group is the job's providers, the queue it's in
	group string
}

func (i *item) ItemKey() string     { return strconv.FormatInt(i.job.ID, 10) }
func (i *item) FilterValue() string { return i.group + " " + i.job.Title }

func (i *item) Title() string {
	j := i.job
	var mark string
	switch j.Status {
	case jobs.Queued:
		mark = icon.Queued.Colored()
	case jobs.Waiting:
		mark = icon.Timer.Colored()
	case jobs.Running:
		mark = icon.Progress.Colored()
	case jobs.Done:
		mark = icon.Check.Colored()
	case jobs.Failed:
		mark = icon.Cross.Colored()
	default:
		mark = style.Normal.Secondary.Render("-")
	}
	return mark + " " + style.Bold.Accent.Render(i.group) + " " + j.Title
}

func (i *item) Description() string {
	j := i.job
	switch j.Status {
	case jobs.Queued:
		return fmt.Sprintf("%s · queued, %s ahead (behind %s)", j.Kind, strconv.Itoa(j.Ahead), j.Behind)
	case jobs.Waiting:
		return j.Kind.String() + " · " + j.Message
	case jobs.Running:
		line := j.Kind.String() + " · " + j.Message
		if j.Total > 0 {
			line += fmt.Sprintf(" · %d/%d", j.Done, j.Total)
		}
		return line
	case jobs.Failed:
		return j.Kind.String() + " · " + style.Normal.Error.Render(j.Err.Error())
	case jobs.Cancelled:
		return j.Kind.String() + " · cancelled " + ago(j.Finished)
	}
	return j.Kind.String() + " · " + j.Message + " · " + ago(j.Finished)
}

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return time.Since(t).Round(time.Second).String() + " ago"
}

type state struct {
	list.Screen
	keys keyMap
	// seen is the newest active job listed while shown: a newer one is selected when it's
	// shown (again), it's the one the user just started
	seen int64
}

// KeyOpen opens the selected job's view.
var KeyOpen = util.Bind("open", "enter")

type keyMap struct {
	// cancel is the remove key on a finished job (the same key)
	open, origin, cancel, retry, clearQueue, clearQueues, cancelAll key.Binding
}

// New is the Downloads screen.
func New() base.State {
	s := &state{keys: keyMap{
		open:        KeyOpen,
		origin:      base.KeyOrigin,
		cancel:      base.KeyCancel,
		retry:       base.KeyRetry,
		clearQueue:  base.KeyClearQueue,
		clearQueues: base.KeyClearQueues,
		cancelAll:   base.KeyCancelAll,
	}}
	s.Screen = list.NewScreen(list.New(2, 1, "job", "jobs", []*item{}, func(i *item) _list.DefaultItem { return i }), s.keyMap)
	s.Confirm = confirm.NewDialog(40, color.Warning)
	s.setItems(true)
	return s
}

func (s *state) keyMap() help.KeyMap {
	k := s.keys
	short := []key.Binding{k.open, k.cancel, k.origin, k.retry}
	return util.KeyMap{Short: short, Full: [][]key.Binding{short, {k.clearQueue, k.clearQueues, k.cancelAll}}}
}

func (s *state) Key() string       { return "downloads" }
func (s *state) Title() base.Title { return base.Title{Text: "Downloads"} }

func (s *state) Subtitle() string {
	running, queued := 0, map[string]int{}
	for _, i := range s.List.Items() {
		j := i.(*item)
		switch j.job.Status {
		case jobs.Running, jobs.Waiting:
			running++
		case jobs.Queued:
			queued[j.group]++
		}
	}
	parts := []string{fmt.Sprintf("%d running", running)}
	groups := make([]string, 0, len(queued))
	for g := range queued {
		groups = append(groups, g)
	}
	slices.Sort(groups)
	for _, g := range groups {
		parts = append(parts, fmt.Sprintf("%s %d queued", g, queued[g]))
	}
	return strings.Join(parts, " · ")
}

func (s *state) Init(context.Context) tea.Cmd {
	return s.setItems(true)
}

// setItems lists the jobs: the active ones by queue (providers, in the order their first
// job came; the running one first, then the queue in order), then the finished ones, the
// latest first. selectNew selects the newest active job if it's newer than the ones seen.
func (s *state) setItems(selectNew bool) tea.Cmd {
	all := tuijobs.Manager().Jobs()
	order := map[string]int{}
	var active, finished []*item
	for _, j := range all {
		i := &item{job: j, group: strings.Join(j.Providers, ", ")}
		if !j.Status.Active() {
			finished = append(finished, i)
			continue
		}
		if _, ok := order[i.group]; !ok {
			order[i.group] = len(order)
		}
		active = append(active, i)
	}
	slices.SortStableFunc(active, func(a, b *item) int {
		return cmp.Or(
			cmp.Compare(order[a.group], order[b.group]),
			cmp.Compare(queuedRank(a.job), queuedRank(b.job)),
			cmp.Compare(a.job.Ahead, b.job.Ahead),
		)
	})
	slices.SortStableFunc(finished, func(a, b *item) int { return b.job.Finished.Compare(a.job.Finished) })
	items := make([]_list.Item, 0, len(all))
	for _, i := range append(active, finished...) {
		items = append(items, i)
	}
	cmd := s.List.SetItemsKeep(items)
	var newest int64
	for _, j := range all {
		if j.Status.Active() {
			newest = max(newest, j.ID)
		}
	}
	if selectNew && newest > s.seen {
		s.List.SelectFunc(func(i _list.Item) bool { return i.(*item).job.ID == newest })
	}
	s.seen = max(s.seen, newest)
	s.updateKeys()
	return cmd
}

// queuedRank puts the running (and waiting) job before the queued ones.
func queuedRank(j jobs.Job) int {
	if j.Status == jobs.Queued {
		return 1
	}
	return 0
}

func (s *state) selected() (jobs.Job, bool) {
	i, ok := s.List.SelectedItem().(*item)
	if !ok {
		return jobs.Job{}, false
	}
	return i.job, true
}

func (s *state) updateKeys() {
	j, ok := s.selected()
	active := ok && j.Status.Active()
	s.keys.open.SetEnabled(ok)
	s.keys.origin.SetEnabled(ok && j.Origin != "")
	s.keys.cancel = base.KeyCancel
	if ok && !active {
		s.keys.cancel = base.KeyRemove
	}
	s.keys.cancel.SetEnabled(ok)
	s.keys.retry.SetEnabled(ok && !active && j.Kind == jobs.KindDownload && (len(j.Results.Failed()) != 0 || len(j.Results.ToDownload()) != 0))
	s.keys.clearQueue.SetEnabled(active)
}

// cancelAllMsg is cancelling every job, confirmed.
type cancelAllMsg struct{}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	m := tuijobs.Manager()
	switch msg := msg.(type) {
	case tuijobs.ChangedMsg:
		return s.setItems(false)
	case base.RestoredMsg:
		return s.setItems(true)
	case cancelAllMsg:
		n := m.CancelAll()
		return base.Notify("Cancelled " + stringutil.Quantify(n, "job", "jobs"))
	case tea.KeyMsg:
		if s.List.Filtering() {
			break
		}
		k := s.keys
		switch {
		case key.Matches(msg, k.cancelAll):
			active := m.Active()
			if len(active) == 0 {
				return base.Notify("Nothing to cancel")
			}
			s.Confirm.Ask("Cancel all", "Cancel "+stringutil.Quantify(len(active), "job", "jobs")+"? The running ones stop, the queues are cleared; the chapters downloaded stay.", s.View(), func() tea.Msg { return cancelAllMsg{} })
			return nil
		case key.Matches(msg, k.clearQueues):
			return base.Notify("Cleared " + stringutil.Quantify(m.ClearQueue(""), "queued job", "queued jobs"))
		}
		j, ok := s.selected()
		if !ok {
			break
		}
		switch {
		case key.Matches(msg, k.open):
			if j.Kind == jobs.KindCheck {
				return func() tea.Msg { return watches.NewLiveLog() }
			}
			return func() tea.Msg { return download.New(j.ID) }
		case key.Matches(msg, k.origin):
			return base.JumpTo(j.Origin)
		case key.Matches(msg, k.cancel) && !j.Status.Active():
			m.Remove(j.ID)
			return s.setItems(false)
		case key.Matches(msg, k.cancel):
			if m.Cancel(j.ID) {
				return base.Notify("Cancelling " + j.Title)
			}
			return nil
		case key.Matches(msg, k.retry):
			if _, err := m.Retry(j.ID); err != nil {
				return func() tea.Msg { return err }
			}
			return s.setItems(false)
		case key.Matches(msg, k.clearQueue):
			var cleared int
			for _, p := range j.Providers {
				cleared += m.ClearQueue(p)
			}
			return base.Notify("Cleared " + stringutil.Quantify(cleared, "queued job", "queued jobs") + " of " + strings.Join(j.Providers, ", "))
		}
	}
	cmd := s.List.Update(msg)
	s.updateKeys()
	return cmd
}
