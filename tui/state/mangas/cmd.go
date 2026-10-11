package mangas

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/state/chapters"
	"github.com/luevano/mangal/v5/tui/state/volumes"
	"github.com/luevano/mangal/v5/util/cache"
)

func (s *state) getHistoryCmd() tea.Msg {
	found, err := cache.GetMangasSearchHistory(&s.history)
	if err != nil {
		return err
	}
	if found {
		s.search.SetSuggestions(s.history.Get())
	}
	return nil
}

func (s *state) updateHistoryCmd(query string) tea.Cmd {
	return func() tea.Msg {
		s.history.Add(query)
		s.history.Sort()
		s.search.SetSuggestions(s.history.Get())
		return cache.SetMangasSearchHistory(s.history)
	}
}

func (s *state) searchMangasCmd(ctx context.Context, query string) tea.Cmd {
	return tea.Sequence(
		base.Loading(fmt.Sprintf("Searching for %q", query)),
		func() tea.Msg {
			mangas, err := s.client.SearchMangasContext(ctx, query)
			if err != nil {
				return nil
			}

			items := make([]list.Item, len(mangas))
			for i, m := range mangas {
				items[i] = newItem(m, s.extraInfo, s.fullExtraInfo)
			}
			s.list.SetItems(items)

			s.searched = true
			s.updateKeybinds()
			return nil
		},
		base.Loaded,
	)
}

func (s *state) searchMetadataCmd(ctx context.Context, item *item) tea.Cmd {
	return tea.Sequence(
		base.Loading(fmt.Sprintf("Searching metadata for %q", item.manga)),
		func() tea.Msg {
			meta, err := s.client.SearchMetadataContext(ctx, item.manga)
			if err != nil {
				return err
			}
			if err := metadata.Validate(meta); err != nil {
				return err
			}

			item.manga.SetMetadata(meta)
			log.Log("Found and set metadata for %q: %q", item.manga, meta.String())
			return s.searchVolumesCmd(ctx, item)()
		},
		base.Loaded,
	)
}

func (s *state) searchVolumesCmd(ctx context.Context, item *item) tea.Cmd {
	return tea.Sequence(
		base.Loading(fmt.Sprintf("Searching volumes for %q", item.manga)),
		func() tea.Msg {
			volumeList, err := s.client.MangaVolumesContext(ctx, item.manga)
			if err != nil {
				return err
			}
			vols := len(volumeList)

			if config.TUI.ExpandAllVolumes.Get() {
				return s.searchAllChaptersCmd(ctx, item.manga, volumeList)()
			}

			if vols == 1 && config.TUI.ExpandSingleVolume.Get() {
				return s.searchChaptersCmd(ctx, item.manga, volumeList[0])()
			}

			return volumes.New(s.client, item.manga, volumeList)
		},
		base.Loaded,
	)
}

func (s *state) searchChaptersCmd(ctx context.Context, manga mangadata.Manga, volume mangadata.Volume) tea.Cmd {
	return tea.Sequence(
		base.NotifyWithDuration(fmt.Sprintf("Skipped single volume (cfg: %s)", config.TUI.ExpandSingleVolume.Key), 3*time.Second),
		base.Loading("Searching chapters"),
		func() tea.Msg {
			chapterList, err := s.client.VolumeChaptersContext(ctx, volume)
			if err != nil {
				return err
			}

			return chapters.New(s.client, manga, nil, chapterList)
		},
		base.Loaded,
	)
}

func (s *state) searchAllChaptersCmd(ctx context.Context, manga mangadata.Manga, volumes []mangadata.Volume) tea.Cmd {
	// TODO: make different loading messages for each volume?
	return tea.Sequence(
		base.NotifyWithDuration(fmt.Sprintf("Skipped selecting volumes (cfg: %s)", config.TUI.ExpandAllVolumes.Key), 3*time.Second),
		base.Loading("Searching chapters for all volumes"),
		func() tea.Msg {
			var chapterList []mangadata.Chapter
			for _, v := range volumes {
				c, err := s.client.VolumeChaptersContext(ctx, v)
				if err != nil {
					return err
				}
				chapterList = append(chapterList, c...)
			}

			return chapters.New(s.client, manga, nil, chapterList)
		},
		base.Loaded,
	)
}
