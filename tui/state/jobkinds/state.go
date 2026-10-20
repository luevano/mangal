// Package jobkinds is the Jobs screen: the maintenance jobs by kind (watch checks, source
// checks, source addresses, the library scan, the network health checks), their state,
// runs and settings ("mangal jobs"). The jobs run in the TUI's jobs (the Downloads screen
// lists them); mangal serve runs the schedules.
package jobkinds

import (
	"context"
	"errors"
	"fmt"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	tuijobs "github.com/luevano/mangal/v5/tui/jobs"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
)

// Kinds are the job kinds over the TUI's jobs (swapped in tests).
var Kinds = func() (core.JobKinds, error) {
	c, err := base.Core()
	if err != nil {
		return nil, err
	}
	return c.Kinds(tuijobs.Manager(), core.KindsOptions{Origin: "jobs"}), nil
}

var _ base.State = (*state)(nil)

type item struct{ k core.JobKind }

func (i *item) ItemKey() string     { return i.k.Kind.String() }
func (i *item) FilterValue() string { return i.k.Title }

func (i *item) Title() string {
	k := i.k
	var mark string
	switch {
	case k.Running != nil || k.Busy != "":
		mark = icon.Progress.Colored()
	case k.Last != nil && k.Last.Error != "":
		mark = icon.Cross.Colored()
	case k.Last != nil:
		mark = icon.Check.Colored()
	default:
		mark = style.Normal.Secondary.Render("-")
	}
	return mark + " " + style.Bold.Accent.Render(k.Title) + " " + style.Normal.Secondary.Render(State(k))
}

func (i *item) Description() string { return Line(i.k) }

// State is the kind's state in a word.
func State(k core.JobKind) string {
	switch {
	case k.Running != nil:
		return "running"
	case k.Busy != "":
		return "busy"
	case k.Manual:
		return "manual"
	case !k.Enabled:
		return "off"
	case k.Paused:
		return "paused"
	}
	return "on"
}

const when = "2006-01-02 15:04"

// Line is what the kind does now, its last and next run.
func Line(k core.JobKind) string {
	if j := k.Running; j != nil {
		line := "running: " + j.Message
		if j.Total > 0 {
			line += fmt.Sprintf(" (%d/%d)", j.Done, j.Total)
		}
		return line
	}
	line := "last: never"
	if k.Last != nil {
		at := k.Last.Finished
		if at.IsZero() {
			at = k.Last.Started
		}
		what := k.Last.Summary
		if k.Last.Error != "" {
			what = style.Normal.Error.Render(k.Last.Error)
		}
		line = "last " + at.Local().Format(when) + ": " + what
	}
	if k.Busy != "" {
		line = k.Busy + " · " + line
	}
	if !k.Next.IsZero() {
		line += " · next " + k.Next.Local().Format(when)
	} else if !k.Manual {
		line += " · " + k.NoNext
	}
	if k.Missing != nil {
		line += fmt.Sprintf(" · %d missing", *k.Missing)
	}
	return line
}

type state struct {
	list.Screen
	keys keyMap
}

type keyMap struct {
	open, runAll, runMissing, pause, edit key.Binding
}

type loadedMsg []_list.Item

// startedMsg is a job started.
type startedMsg struct{ notify string }

// New is the Jobs screen.
func New() base.State {
	s := &state{keys: keyMap{
		open:       util.Bind("details", "enter"),
		runAll:     base.KeyRunAll,
		runMissing: base.KeyRunMissing,
		pause:      base.KeyPause,
		edit:       base.KeyEdit,
	}}
	s.Screen = list.NewScreen(list.New(2, 1, "kind", "kinds", []*item{}, func(i *item) _list.DefaultItem { return i }), s.keyMap)
	return s
}

func (s *state) keyMap() help.KeyMap {
	k := s.keys
	return util.KeyMap{Short: []key.Binding{k.open, k.runMissing, k.runAll, k.pause, k.edit}}
}

func (s *state) Key() string       { return "jobs" }
func (s *state) Title() base.Title { return base.Title{Text: "Jobs"} }

func (s *state) Subtitle() string {
	return "the maintenance jobs; mangal serve runs their schedules (" + base.KeyDownloads.Help().Key + ": downloads)"
}

func (s *state) Init(ctx context.Context) tea.Cmd { return s.reload(ctx) }

func (s *state) reload(ctx context.Context) tea.Cmd {
	return base.Load(ctx, "", func(ctx context.Context) (loadedMsg, error) {
		kinds, err := Kinds()
		if err != nil {
			return nil, err
		}
		list, err := kinds.List(ctx)
		if err != nil {
			return nil, err
		}
		items := make(loadedMsg, len(list))
		for i, k := range list {
			items[i] = &item{k: k}
		}
		return items, nil
	}, func(items loadedMsg) tea.Msg { return items })
}

func (s *state) selected() (core.JobKind, bool) {
	i, ok := s.List.SelectedItem().(*item)
	if !ok {
		return core.JobKind{}, false
	}
	return i.k, true
}

func (s *state) updateKeys() {
	k, ok := s.selected()
	s.keys.open.SetEnabled(ok)
	s.keys.runAll.SetEnabled(ok && k.Running == nil)
	s.keys.runMissing.SetEnabled(ok && k.Running == nil && k.Missing != nil)
	s.keys.pause.SetEnabled(ok && !k.Manual)
	s.keys.edit.SetEnabled(ok)
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case loadedMsg:
		cmd := s.List.SetItemsKeep(msg)
		s.updateKeys()
		return cmd
	case tuijobs.ChangedMsg, base.RestoredMsg:
		return s.reload(ctx)
	case savedMsg:
		return tea.Batch(s.reload(ctx), base.Notify(msg.notify))
	case startedMsg:
		return tea.Batch(s.reload(ctx), base.Notify(msg.notify))
	case tea.KeyPressMsg:
		if s.List.Filtering() {
			break
		}
		k, ok := s.selected()
		if !ok {
			break
		}
		switch {
		case key.Matches(msg, s.keys.open):
			return func() tea.Msg { return newDetail(k) }
		case key.Matches(msg, s.keys.edit):
			return func() tea.Msg { return newSettings(k) }
		case key.Matches(msg, s.keys.runAll):
			return run(ctx, k, true)
		case key.Matches(msg, s.keys.runMissing):
			return run(ctx, k, false)
		case key.Matches(msg, s.keys.pause):
			return pause(ctx, k)
		}
	}
	cmd := s.List.Update(msg)
	s.updateKeys()
	return cmd
}

// run starts a job of the kind: all, or the missing ones.
func run(ctx context.Context, k core.JobKind, all bool) tea.Cmd {
	if base.Busy(ctx) {
		return nil
	}
	return base.Load(ctx, "Starting", func(ctx context.Context) (startedMsg, error) {
		kinds, err := Kinds()
		if err != nil {
			return startedMsg{}, err
		}
		job, err := kinds.Run(ctx, k.Kind, all)
		switch {
		case errors.Is(err, core.ErrNothingToDo):
			return startedMsg{notify: k.Title + ": nothing to do"}, nil
		case errors.Is(err, core.ErrRefused):
			return startedMsg{notify: k.Title + " runs already"}, nil
		case err != nil:
			return startedMsg{}, err
		}
		return startedMsg{notify: fmt.Sprintf("Started %s (%s: downloads)", job.Title, base.KeyDownloads.Help().Key)}, nil
	}, func(msg startedMsg) tea.Msg { return msg })
}

// pause pauses or resumes the kind's schedule.
func pause(ctx context.Context, k core.JobKind) tea.Cmd {
	return base.Load(ctx, "", func(ctx context.Context) (savedMsg, error) {
		kinds, err := Kinds()
		if err != nil {
			return savedMsg{}, err
		}
		k, err := kinds.Pause(ctx, k.Kind, !k.Paused)
		if err != nil {
			return savedMsg{}, err
		}
		if k.Paused {
			return savedMsg{notify: k.Title + ": paused (a running job goes on)"}, nil
		}
		return savedMsg{notify: k.Title + ": resumed"}, nil
	}, func(msg savedMsg) tea.Msg { return msg })
}

// savedMsg is a kind's settings or pause changed.
type savedMsg struct{ notify string }

// next is a provider's next run, "-" without one.
func next(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format(when)
}

// jobKind is the kind as it is now.
func jobKind(ctx context.Context, kind jobs.Kind) (core.JobKind, error) {
	kinds, err := Kinds()
	if err != nil {
		return core.JobKind{}, err
	}
	return kinds.Get(ctx, kind)
}
