package anilist

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/metadata"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/luevano/mangal/v5/util/cache"
)

var _ base.State = (*bindingsState)(nil)

// bindingsState lists the metadata sources of the client with the manga's binding to each.
type bindingsState struct {
	list.Screen
	client *libmangal.Client
	manga  mangadata.Manga

	search,
	remove,
	metadata key.Binding
}

// loadedMsg are the sources with their bindings and bound metadata.
type loadedMsg []*sourceItem

// changedMsg is a binding changed (removed), the screen reloads.
type changedMsg struct{ notice string }

// removeMsg is the removal of the source's binding, confirmed.
type removeMsg struct{ src client.MetadataSource }

// Open shows the metadata bindings of the manga.
func Open(c *libmangal.Client, manga mangadata.Manga) tea.Cmd {
	return func() tea.Msg { return newBindings(c, manga) }
}

func newBindings(c *libmangal.Client, manga mangadata.Manga) base.State {
	s := &bindingsState{
		client:   c,
		manga:    manga,
		search:   util.Bind("search", "enter", "s"),
		remove:   base.KeyRemove,
		metadata: base.KeyMetadata,
	}
	s.Screen = list.NewScreen(list.New(2, 1, "source", "sources", []*sourceItem{}, func(i *sourceItem) _list.DefaultItem { return i }), s.keyMap)
	s.Confirm = confirm.NewDialog(40, color.Warning)
	return s
}

func (s *bindingsState) keyMap() help.KeyMap {
	return util.KeyMap{
		Short: []key.Binding{s.search, s.remove, s.metadata},
		Full:  [][]key.Binding{{s.search, s.remove, s.metadata}},
	}
}

func (s *bindingsState) Key() string {
	return "bindings/" + s.client.Info().ID + "/" + s.manga.Info().ID
}

func (s *bindingsState) Title() base.Title {
	return base.Title{Text: "Metadata", Background: color.Anilist, Foreground: color.Bright}
}

func (s *bindingsState) Subtitle() string {
	return fmt.Sprintf("%s · %s", s.manga.Info().Title, s.client.Info().ID)
}

func (s *bindingsState) Init(ctx context.Context) tea.Cmd {
	return s.load(ctx)
}

func (s *bindingsState) load(ctx context.Context) tea.Cmd {
	c, manga := s.client, s.manga
	return base.Load(ctx, "Loading the metadata bindings", func(ctx context.Context) (loadedMsg, error) {
		bindings, err := cache.Bindings()
		if err != nil {
			return nil, err
		}
		bound, err := bindings.Get(c.Info().ID, manga.Info().ID)
		if err != nil {
			return nil, err
		}
		var items []*sourceItem
		for _, src := range client.MetadataSources {
			// a source the client doesn't have isn't enabled or configured
			if _, err := c.GetMetadataProvider(src.Source); err != nil {
				continue
			}
			i := &sourceItem{src: src}
			for _, b := range bound {
				if b.Source == src.Code {
					i.binding = &b
				}
			}
			if i.binding != nil {
				i.meta, i.err = client.BoundMetadata(ctx, c, src.Code, i.binding.ID)
			} else if meta := manga.Metadata(); meta != nil && meta.ID().Code == src.Code {
				i.matched = meta
			}
			items = append(items, i)
		}
		return items, nil
	}, func(items loadedMsg) tea.Msg { return items })
}

func (s *bindingsState) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if cmd, ok := s.Answer(msg); ok {
		return cmd
	}
	switch msg := msg.(type) {
	case removeMsg:
		src := msg.src
		return s.change(ctx, func(b storage.Bindings) (string, error) {
			_, err := base.ApplyChange(ctx, core.UnbindReq{Provider: s.client.Info().ID, MangaID: s.manga.Info().ID, Code: src.Code}, func(string) {})
			if errors.Is(err, core.ErrNothingToDo) {
				err = nil
			}
			return fmt.Sprintf("Removed the %s binding", src.Name), err
		})
	case loadedMsg:
		items := make([]_list.Item, len(msg))
		for i, it := range msg {
			items[i] = it
		}
		cmd := s.List.SetItemsKeep(items)
		s.applyDefault(msg)
		return cmd
	case changedMsg:
		return tea.Batch(s.load(ctx), base.Notify(msg.notice))
	case base.RestoredMsg:
		return s.load(ctx)
	case tea.KeyMsg:
		if s.List.Filtering() {
			break
		}
		i, ok := s.List.SelectedItem().(*sourceItem)
		if !ok {
			break
		}
		switch {
		case key.Matches(msg, s.search):
			p, err := s.client.GetMetadataProvider(i.src.Source)
			if err != nil {
				return func() tea.Msg { return err }
			}
			return func() tea.Msg { return newSearch(s.client, i.src, p, s.manga) }
		case key.Matches(msg, s.remove) && i.binding != nil:
			src := i.src
			message := fmt.Sprintf("Remove the %s binding of %q (%s)?", src.Name, s.manga.Info().Title, i.binding.ID)
			s.Confirm.AskDelete("Remove", message, s.View(), func() tea.Msg { return removeMsg{src: src} })
			return nil
		case key.Matches(msg, s.metadata):
			switch {
			case i.meta != nil:
				return metadata.ShowCmd(i.meta)
			case i.matched != nil:
				return metadata.ShowCmd(i.matched)
			}
		}
	}
	return s.List.Update(msg)
}

func (s *bindingsState) change(ctx context.Context, change func(storage.Bindings) (string, error)) tea.Cmd {
	return base.Load(ctx, "", func(context.Context) (changedMsg, error) {
		bindings, err := cache.Bindings()
		if err != nil {
			return changedMsg{}, err
		}
		notice, err := change(bindings)
		return changedMsg{notice: notice}, err
	}, func(msg changedMsg) tea.Msg { return msg })
}

// applyDefault sets the default binding's metadata on the manga (the first bound source
// whose metadata loaded, as downloads fall back), so the screens and their downloads use it.
func (s *bindingsState) applyDefault(items []*sourceItem) {
	for _, i := range items {
		if i.binding == nil || i.meta == nil {
			continue
		}
		if cur := s.manga.Metadata(); cur != nil && cur.ID().Code == i.src.Code && cur.ID().Raw == i.binding.ID {
			return
		}
		if err := lmmeta.Validate(i.meta); err != nil {
			log.Info("bound metadata %q of %q: %s", i.meta, s.manga, err)
		}
		s.manga.SetMetadata(i.meta)
		log.Info("set the %s metadata %q to manga %q", i.src.Name, i.meta, s.manga)
		return
	}
}
