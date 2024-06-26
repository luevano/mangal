package mangas

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/config"
	"github.com/luevano/mangal/log"
	"github.com/luevano/mangal/tui/base"
	"github.com/luevano/mangal/tui/state/chapters"
	"github.com/luevano/mangal/tui/state/listwrapper"
	"github.com/luevano/mangal/tui/state/loading"
	"github.com/luevano/mangal/tui/state/volumes"
)

var _ base.State = (*State)(nil)

// State implements base.State.
type State struct {
	list   *listwrapper.State
	mangas []mangadata.Manga
	client *libmangal.Client
	query  string
	keyMap keyMap
}

// Intermediate implements base.State.
func (s *State) Intermediate() bool {
	return false
}

// Backable implements base.State.
func (s *State) Backable() bool {
	return s.list.Backable()
}

// KeyMap implements base.State.
func (s *State) KeyMap() help.KeyMap {
	return s.keyMap
}

// Title implements base.State.
func (s *State) Title() base.Title {
	return base.Title{Text: fmt.Sprintf("Search %q", s.query)}
}

// Subtitle implements base.State.
func (s *State) Subtitle() string {
	return s.list.Subtitle()
}

// Status implements base.State.
func (s *State) Status() string {
	return s.list.Status()
}

// Resize implements base.State.
func (s *State) Resize(size base.Size) {
	s.list.Resize(size)
}

// Init implements base.State.
func (s *State) Init(model base.Model) tea.Cmd {
	return s.list.Init(model)
}

// Update implements base.State.
func (s *State) Update(model base.Model, msg tea.Msg) (cmd tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.list.FilterState() == list.Filtering {
			goto end
		}

		item, ok := s.list.SelectedItem().(Item)
		if !ok {
			return nil
		}

		switch {
		case key.Matches(msg, s.keyMap.confirm):
			return tea.Sequence(
				func() tea.Msg {
					return loading.New("Searching", fmt.Sprintf("Finding Anilist for %q", item.manga))
				},
				func() tea.Msg {
					// TODO: handle more cases for missing/partial metadata
					// Find anilist manga closest to the selected manga and assign it
					anilistManga, found, err := s.client.Anilist().SearchByManga(context.Background(), item.manga)
					if err != nil {
						return err
					}
					if !found {
						log.Log("Couldn't find Anilist for %q", item.manga)
					} else {
						item.manga.SetMetadata(anilistManga.Metadata())
						log.Log("Found and set Anilist for %q: %q (%d)", item.manga, anilistManga.String(), anilistManga.ID)
						log.Log("Set metadata to %q: %p", item.manga, item.manga)
					}

					return nil
				},
				func() tea.Msg {
					return loading.New("Searching", fmt.Sprintf("Getting volumes for %q", item.manga))
				},
				func() tea.Msg {
					volumeList, err := s.client.MangaVolumes(model.Context(), item.manga)
					if err != nil {
						return err
					}

					if len(volumeList) != 1 || !config.TUI.ExpandSingleVolume.Get() {
						return volumes.New(s.client, item.manga, volumeList)
					}

					// It's guaranteed to at least contain 1 volume
					volume := volumeList[0]
					chapterList, err := s.client.VolumeChapters(model.Context(), volume)
					if err != nil {
						return err
					}

					return chapters.New(s.client, item.manga, volume, chapterList)
				},
			)
		}
	}
end:
	return s.list.Update(model, msg)
}

// View implements base.State.
func (s *State) View(model base.Model) string {
	return s.list.View(model)
}
