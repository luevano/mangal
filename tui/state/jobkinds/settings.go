package jobkinds

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/huhform"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cron"
)

var _ base.State = (*settings)(nil)

// settings changes a kind's settings: on, its schedule, the pause between requests and
// each provider's setting.
type settings struct {
	k                        core.JobKind
	form                     *huhform.Form
	enabled, schedule, delay *huhform.Field
	providers                []*huhform.Field
}

func validDuration(s string) error {
	_, err := time.ParseDuration(s)
	return err
}

// validInterval is the kind's own interval: a cron schedule for the watch checks.
func validInterval(kind jobs.Kind) func(string) error {
	if kind == jobs.KindCheck {
		return func(s string) error {
			_, err := cron.Parse(s)
			return err
		}
	}
	return validDuration
}

// validDelay is min-max, or one duration.
func validDelay(s string) error {
	lo, hi, _ := strings.Cut(s, "-")
	if err := validDuration(strings.TrimSpace(lo)); err != nil {
		return err
	}
	if hi != "" {
		return validDuration(strings.TrimSpace(hi))
	}
	return nil
}

func newSettings(k core.JobKind) base.State {
	s := &settings{k: k}
	var fields []*huhform.Field
	if !k.Manual {
		s.enabled = huhform.Toggle("On", k.Enabled, "mangal serve runs it on its schedule")
		fields = append(fields, s.enabled)
	}
	if k.ScheduleKey != "" {
		hint := "a Go duration, 0 off (" + k.ScheduleKey + ")"
		if k.Kind == jobs.KindCheck {
			hint = `cron, e.g. "0 */4 * * *" or @daily (` + k.ScheduleKey + ")"
		}
		s.schedule = huhform.Text("Schedule", k.Schedule, hint, validInterval(jobs.KindSourceCheck))
		if k.Kind == jobs.KindCheck {
			s.schedule.SetValidate(validInterval(jobs.KindCheck))
		}
		fields = append(fields, s.schedule)
	}
	if k.Kind == jobs.KindSourceCheck || k.Kind == jobs.KindSourceURLs {
		s.delay = huhform.Text("Pause", config.Jobs.DelayMin.Fresh()+"-"+config.Jobs.DelayMax.Fresh(), "between two requests to a provider, min-max (every kind)", validDelay)
		fields = append(fields, s.delay)
	}
	if k.ProvidersKey != "" {
		valid := validInterval(k.Kind)
		for _, p := range k.Providers {
			value := p.Interval
			if !p.On {
				value = config.ProviderOff
			} else if value == "" && p.Default != "" {
				value = config.ProviderOn
			}
			hint := "on, off or its own interval; empty: the default"
			if p.Default != "" {
				hint += " (" + p.Default + ")"
			}
			f := huhform.Text(p.Name, value, hint, func(v string) error {
				if v == "" || v == config.ProviderOn || v == config.ProviderOff {
					return nil
				}
				return valid(v)
			})
			s.providers = append(s.providers, f)
			fields = append(fields, f)
		}
	}
	s.form = huhform.New(fields...)
	return s
}

func (s *settings) Backable() bool               { return true }
func (s *settings) Inputting() bool              { return true }
func (s *settings) KeyMap() help.KeyMap          { return util.KeyMap{Short: s.form.Keys()} }
func (s *settings) Key() string                  { return "jobs-settings/" + s.k.Kind.String() }
func (s *settings) Title() base.Title            { return base.Title{Text: s.k.Title + " settings"} }
func (s *settings) Subtitle() string             { return "saved in the config file (" + s.k.ProvidersKey + ")" }
func (s *settings) Status() string               { return "" }
func (s *settings) Resize(base.Size) tea.Cmd     { return nil }
func (s *settings) Init(context.Context) tea.Cmd { return s.form.Focus(0) }
func (s *settings) View() string                 { return s.form.View() }

// changes are the settings changed, valid once the form is.
func (s *settings) changes() core.JobSettings {
	var c core.JobSettings
	if s.enabled != nil && s.enabled.On() != s.k.Enabled {
		on := s.enabled.On()
		c.Enabled = &on
	}
	if s.schedule != nil && s.schedule.Value() != s.k.Schedule {
		v := s.schedule.Value()
		c.Schedule = &v
	}
	if s.delay != nil && s.delay.Value() != config.Jobs.DelayMin.Fresh()+"-"+config.Jobs.DelayMax.Fresh() {
		lo, hi, ok := strings.Cut(s.delay.Value(), "-")
		if !ok {
			hi = lo
		}
		lo, hi = strings.TrimSpace(lo), strings.TrimSpace(hi)
		c.DelayMin, c.DelayMax = &lo, &hi
	}
	for i, f := range s.providers {
		p := s.k.Providers[i]
		was := p.Interval
		if !p.On {
			was = config.ProviderOff
		} else if was == "" && p.Default != "" {
			was = config.ProviderOn
		}
		if v := strings.ToLower(f.Value()); v != was {
			if c.Providers == nil {
				c.Providers = map[string]string{}
			}
			c.Providers[p.Provider] = v
		}
	}
	return c
}

func (s *settings) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case savedMsg:
		return tea.Sequence(base.Back, base.Notify(msg.notify))
	case tea.KeyPressMsg:
		if base.Busy(ctx) {
			return nil
		}
	}
	submitted, cmd := s.form.Update(msg)
	if !submitted {
		return cmd
	}
	c := s.changes()
	if c.Enabled == nil && c.Schedule == nil && c.DelayMin == nil && c.Providers == nil {
		return tea.Sequence(base.Back, base.Notify("Nothing changed"))
	}
	kind, title := s.k.Kind, s.k.Title
	return base.Load(ctx, "Saving", func(ctx context.Context) (savedMsg, error) {
		kinds, err := Kinds()
		if err != nil {
			return savedMsg{}, err
		}
		_, err = kinds.Set(ctx, kind, c)
		return savedMsg{notify: "Saved " + title + " settings"}, err
	}, func(msg savedMsg) tea.Msg { return msg })
}
