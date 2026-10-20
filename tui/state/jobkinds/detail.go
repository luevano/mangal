package jobkinds

import (
	"cmp"
	"context"
	"fmt"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	_list "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/util"
)

type providerItem struct {
	p    core.JobProvider
	kind core.JobKind
}

func (i *providerItem) ItemKey() string     { return i.p.Provider }
func (i *providerItem) FilterValue() string { return i.p.Name + " " + i.p.Provider }

func (i *providerItem) Title() string {
	on := style.Normal.Success.Render("on")
	if !i.p.On {
		on = style.Normal.Secondary.Render("off")
	}
	return on + " " + style.Bold.Accent.Render(i.p.Name) + " " + style.Normal.Secondary.Render(i.p.Provider)
}

func (i *providerItem) Description() string {
	line := "every " + cmp.Or(i.p.Interval, i.kind.Schedule, "run")
	if i.p.Interval == "" && i.kind.Schedule != "" {
		line += " (the kind's)"
	}
	if i.p.Default != "" {
		line += " · default " + i.p.Default
	}
	if !i.kind.Manual {
		line += fmt.Sprintf(" · %d items, %d missing · next %s", i.p.Items, i.p.Missing, next(i.p.Next))
	}
	return line
}

// detail is a kind's per-provider settings and counts.
type detail struct {
	list.Screen
	k    core.JobKind
	keys detailKeys
}

type detailKeys struct {
	toggle, reset, edit key.Binding
}

type detailMsg struct{ k core.JobKind }

func newDetail(k core.JobKind) base.State {
	d := &detail{k: k, keys: detailKeys{toggle: base.KeyOnOff, reset: base.KeyReset, edit: base.KeyEdit}}
	d.Screen = list.NewScreen(list.New(2, 1, "provider", "providers", []*providerItem{}, func(i *providerItem) _list.DefaultItem { return i }), d.keyMap)
	return d
}

func (d *detail) keyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{d.keys.toggle, d.keys.reset, d.keys.edit}}
}

func (d *detail) Key() string       { return "jobs/" + d.k.Kind.String() }
func (d *detail) Title() base.Title { return base.Title{Text: d.k.Title} }

func (d *detail) Subtitle() string {
	s := d.k.About + " · " + Line(d.k)
	if d.k.MissingWhat != "" {
		s += " (" + d.k.MissingWhat + ")"
	}
	return s
}

func (d *detail) Init(ctx context.Context) tea.Cmd { return d.reload(ctx) }

func (d *detail) reload(ctx context.Context) tea.Cmd {
	kind := d.k.Kind
	return base.Load(ctx, "", func(ctx context.Context) (detailMsg, error) {
		k, err := jobKind(ctx, kind)
		return detailMsg{k: k}, err
	}, func(msg detailMsg) tea.Msg { return msg })
}

func (d *detail) set(ctx context.Context, provider, value, notify string) tea.Cmd {
	kind := d.k.Kind
	return base.Load(ctx, "Saving", func(ctx context.Context) (savedMsg, error) {
		kinds, err := Kinds()
		if err != nil {
			return savedMsg{}, err
		}
		_, err = kinds.Set(ctx, kind, core.JobSettings{Providers: map[string]string{provider: value}})
		return savedMsg{notify: notify}, err
	}, func(msg savedMsg) tea.Msg { return msg })
}

func (d *detail) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case detailMsg:
		d.k = msg.k
		items := make([]_list.Item, len(msg.k.Providers))
		for i, p := range msg.k.Providers {
			items[i] = &providerItem{p: p, kind: msg.k}
		}
		return d.List.SetItemsKeep(items)
	case savedMsg:
		return tea.Batch(d.reload(ctx), base.Notify(msg.notify))
	case base.RestoredMsg:
		return d.reload(ctx)
	case tea.KeyPressMsg:
		if d.List.Filtering() {
			break
		}
		if key.Matches(msg, d.keys.edit) {
			k := d.k
			return func() tea.Msg { return newSettings(k) }
		}
		i, ok := d.List.SelectedItem().(*providerItem)
		if !ok || d.k.ProvidersKey == "" || base.Busy(ctx) {
			break
		}
		switch {
		case key.Matches(msg, d.keys.toggle):
			value, word := config.ProviderOff, "off"
			if !i.p.On {
				value, word = config.ProviderOn, "on"
			}
			return d.set(ctx, i.p.Provider, value, fmt.Sprintf("%s: %s %s", d.k.Title, i.p.Name, word))
		case key.Matches(msg, d.keys.reset):
			return d.set(ctx, i.p.Provider, "", fmt.Sprintf("%s: %s back to its default", d.k.Title, i.p.Name))
		}
	}
	return d.List.Update(msg)
}
