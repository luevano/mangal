package volumes

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	_list "github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/metadata"
	"github.com/luevano/mangal/v5/tui/state/anilist"
	"github.com/luevano/mangal/v5/tui/state/chapters"
	"github.com/luevano/mangal/v5/tui/util"
)

var _ base.State = (*state)(nil)

type state struct {
	list.Screen
	manga  mangadata.Manga
	client *libmangal.Client
	// fullMeta shows the metadata's source and id in the status
	fullMeta bool

	confirm,
	anilist,
	metadata,
	info key.Binding
}

func New(client *libmangal.Client, manga mangadata.Manga, volumes []mangadata.Volume) base.State {
	s := &state{
		manga:    manga,
		client:   client,
		confirm:  util.Bind("confirm", "enter"),
		anilist:  util.Bind("anilist", "A"),
		metadata: util.Bind("metadata", "m"),
		info:     util.Bind("info", "i"),
	}
	s.Screen = list.NewScreen(list.New(1, 1, "volume", "volumes", volumes, func(volume mangadata.Volume) _list.DefaultItem {
		return &item{volume}
	}), s.keyMap)
	return s
}

func (s *state) keyMap() help.KeyMap {
	return util.KeyMap{Short: []key.Binding{s.confirm, s.anilist, s.metadata, s.info}}
}

func (s *state) Title() base.Title {
	return base.Title{Text: s.manga.String()}
}

func (s *state) Status() string {
	return metadata.Status(s.manga.Metadata(), s.fullMeta) + s.List.Status()
}

func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	if msg, ok := msg.(tea.KeyMsg); ok && !s.List.Filtering() {
		i, ok := s.List.SelectedItem().(*item)
		if !ok {
			return s.List.Update(msg)
		}
		switch {
		case key.Matches(msg, s.confirm):
			if base.Busy(ctx) {
				return nil
			}
			return s.chaptersCmd(ctx, i.volume)
		case key.Matches(msg, s.anilist):
			return anilist.Open(s.client, s.manga)
		case key.Matches(msg, s.metadata):
			return metadata.ShowManga(s.manga)
		case key.Matches(msg, s.info):
			s.fullMeta = !s.fullMeta
			return nil
		}
	}
	return s.List.Update(msg)
}

func (s *state) chaptersCmd(ctx context.Context, volume mangadata.Volume) tea.Cmd {
	return base.Load(ctx, fmt.Sprintf("Searching chapters for volume %s", volume), func(ctx context.Context) ([]mangadata.Chapter, error) {
		return s.client.VolumeChaptersContext(ctx, volume)
	}, func(list []mangadata.Chapter) tea.Msg {
		return chapters.New(s.client, s.manga, volume, list)
	})
}
