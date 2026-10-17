package watches

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/client/watch"
	"github.com/luevano/mangal/v5/tui/base"
	tuijobs "github.com/luevano/mangal/v5/tui/jobs"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

func (s *listState) updateKey(ctx context.Context, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	m := s.manager
	switch {
	case key.Matches(msg, s.keys.refresh):
		return s.reload(ctx, "Refreshing"), true
	case key.Matches(msg, s.keys.runs):
		return func() tea.Msg { return newRunsState(m) }, true
	case key.Matches(msg, s.keys.nextProvider):
		s.provider = nextProvider(s.providers(), s.provider)
		return s.setItems(), true
	case key.Matches(msg, s.keys.group):
		s.byProvider = !s.byProvider
		return s.setItems(), true
	case key.Matches(msg, s.keys.missing):
		s.missing = !s.missing
		cmd := s.setItems()
		if s.missing && len(s.List.Items()) == 0 {
			cmd = tea.Batch(cmd, base.Notify("No watch needs a re-link"))
		}
		return cmd, true
	case key.Matches(msg, s.keys.unlinked):
		s.unlinked = !s.unlinked
		cmd := s.setItems()
		if s.unlinked && len(s.List.Items()) == 0 {
			cmd = tea.Batch(cmd, base.Notify("Every watch is in the library (or has downloaded nothing yet)"))
		}
		return cmd, true
	case key.Matches(msg, s.keys.running):
		return func() tea.Msg { return newLiveState(m, nil) }, true
	case key.Matches(msg, s.keys.checkDue):
		var due []int64
		var providers []string
		now := s.now()
		for _, i := range s.List.Items() {
			w := i.(*item).w
			if !w.Scheduled() || w.NextCheck.After(now) {
				continue
			}
			due = append(due, w.ID)
			providers = append(providers, w.Provider)
		}
		if len(due) == 0 {
			return base.Notify("No watch is due"), true
		}
		return s.check(due, providers, watch.CheckOptions{}, stringutil.Quantify(len(due), "due watch", "due watches")), true
	}
	i, ok := s.List.SelectedItem().(*item)
	if !ok {
		return nil, false
	}
	w := i.w
	switch {
	case key.Matches(msg, s.keys.details):
		backoff := s.backoff
		return func() tea.Msg { return newDetailsState(m, w, backoff) }, true
	case key.Matches(msg, s.keys.edit):
		return func() tea.Msg { return newEditState(m, w) }, true
	case key.Matches(msg, s.keys.relink):
		return func() tea.Msg { return newRelinkState(m, w) }, true
	case key.Matches(msg, s.keys.library):
		return libraryCmd(ctx, w), true
	case key.Matches(msg, s.keys.history):
		return func() tea.Msg { return newHistoryState(m, w.Provider) }, true
	case key.Matches(msg, s.keys.live):
		return func() tea.Msg { return newLiveState(m, &w) }, true
	case key.Matches(msg, s.keys.check):
		return s.check([]int64{w.ID}, []string{w.Provider}, watch.CheckOptions{}, w.Title), true
	case key.Matches(msg, s.keys.gaps):
		return s.check([]int64{w.ID}, []string{w.Provider}, watch.CheckOptions{Gaps: true, Kind: watch.KindGaps}, w.Title+" (gaps)"), true
	case key.Matches(msg, s.keys.checkProvider):
		s.started = s.now()
		// a new poll loop: the spinner's goes with the old one
		s.gen++
		s.spinning = false
		return tea.Batch(checkProviderCmd(ctx, m, w.Provider, s.Key()), s.tick(pollChecking), s.spin()), true
	case key.Matches(msg, s.keys.pause):
		if w.Hold != "" {
			return base.Notify(fmt.Sprintf("%s is %s: %s clears it", w.Title, markLabel(w.Hold), s.keys.hold.Help().Key)), true
		}
		return pauseCmd(m, w), true
	case key.Matches(msg, s.keys.hold):
		AskHold(s.Confirm, s.View(), WatchHoldTarget(w), func(opts watch.HoldOptions) tea.Cmd {
			return holdCmd(m, opts, w.Title)
		})
		return nil, true
	case key.Matches(msg, s.keys.pauseProvider):
		return pauseProviderCmd(m, w), true
	case key.Matches(msg, s.keys.delete):
		message := fmt.Sprintf("Stop watching %q? The history of its checks stays, the downloads too.", w.Title)
		s.Confirm.AskDelete("Stop watching", message, s.View(), func() tea.Msg {
			return actionMsg{label: "Removing " + w.Title, do: func(ctx context.Context) (string, error) {
				undo := fmt.Sprintf("stopped watching %s (%s in the library undoes it)", w.Title, base.KeyUndo.Help().Key)
				return undo, m.Remove(ctx, w.ID)
			}}
		})
		return nil, true
	}
	return nil, false
}

// pauseCmd pauses or resumes the watch.
func pauseCmd(m watch.Manager, w watch.Watch) tea.Cmd {
	label, done := "Pausing "+w.Title, "paused "+w.Title
	if !w.Enabled {
		label, done = "Resuming "+w.Title, "resumed "+w.Title
	}
	return func() tea.Msg {
		return actionMsg{label: label, do: func(ctx context.Context) (string, error) {
			_, err := m.SetEnabled(ctx, w.ID, !w.Enabled)
			return done, err
		}}
	}
}

// holdCmd marks the watch (or its library manga).
func holdCmd(m watch.Manager, opts watch.HoldOptions, title string) tea.Cmd {
	return func() tea.Msg {
		return actionMsg{label: HoldLabel(opts, title), do: func(ctx context.Context) (string, error) {
			return hold(ctx, m, opts, title)
		}}
	}
}

// pauseProviderCmd pauses every watch of w's provider, or resumes them when w is paused
// (e.g. while the site is broken).
func pauseProviderCmd(m watch.Manager, w watch.Watch) tea.Cmd {
	resume := !w.Enabled
	label, done := "Pausing the watches of "+w.Provider, "paused the watches of "+w.Provider
	if resume {
		label, done = "Resuming the watches of "+w.Provider, "resumed the watches of "+w.Provider
	}
	return func() tea.Msg {
		return actionMsg{label: label, do: func(ctx context.Context) (string, error) {
			all, err := m.List(ctx)
			if err != nil {
				return "", err
			}
			n := 0
			for _, other := range all {
				if other.Provider != w.Provider || other.Enabled == resume || resume && other.Hold != "" {
					continue
				}
				if _, err := m.SetEnabled(ctx, other.ID, resume); err != nil {
					return "", err
				}
				n++
			}
			return fmt.Sprintf("%s (%d)", done, n), nil
		}}
	}
}

// check queues a check of the watches (of the providers) as a background job (tui/jobs):
// it waits for the jobs and other mangals that have its providers, keeps running on other
// screens, its summary is a notification wherever the user is.
func (s *listState) check(ids []int64, providers []string, opts watch.CheckOptions, what string) tea.Cmd {
	s.started = s.now()
	// polls faster from now, the loop in place could be waiting its slow tick
	s.gen++
	s.spinning = false
	queueCheck(s.manager, ids, providers, opts, what, s.Key())
	return tea.Batch(s.tick(pollChecking), s.spin())
}

func queueCheck(m watch.Manager, ids []int64, providers []string, opts watch.CheckOptions, what, origin string) {
	tuijobs.Check(m, ids, providers, opts, what, origin, summary)
}

// checkProviderCmd queues a check of every enabled watch of the provider.
func checkProviderCmd(ctx context.Context, m watch.Manager, provider, origin string) tea.Cmd {
	return base.Load(ctx, "Listing the watches of "+provider, func(ctx context.Context) (tea.Msg, error) {
		all, err := m.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", provider, err)
		}
		var ids []int64
		for _, w := range all {
			if w.Provider == provider && w.Scheduled() {
				ids = append(ids, w.ID)
			}
		}
		if len(ids) == 0 {
			return base.NotificationMsg{Message: provider + " has no enabled watches"}, nil
		}
		queueCheck(m, ids, []string{provider}, watch.CheckOptions{}, "every watch of "+provider, origin)
		return nil, nil
	}, func(msg tea.Msg) tea.Msg { return msg })
}

// summary is a check's report in one line.
func summary(r watch.Report) string {
	s := fmt.Sprintf("Checked %s: %s", stringutil.Quantify(r.Watches, "watch", "watches"), stringutil.Quantify(r.NewChapters, "new chapter", "new chapters"))
	if r.Errors > 0 {
		s += ", " + stringutil.Quantify(r.Errors, "error", "errors")
	}
	if len(r.Busy) > 0 {
		s += " (busy, skipped: " + strings.Join(r.Busy, ", ") + ")"
	}
	return s
}
