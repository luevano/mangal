package chapters

import (
	"fmt"

	_list "github.com/charmbracelet/bubbles/list"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/theme/color"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/format"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/metadata"
	"github.com/zyedidia/generic/set"
)

// volume can be nil, which represents a list of chapters for a manga with only one chapter
func New(client *libmangal.Client, manga mangadata.Manga, volume mangadata.Volume, chapters []mangadata.Chapter) *state {
	showVolumeNumber := config.TUI.Chapter.ShowVolumeNumber.Get()
	showChapterNumber := config.TUI.Chapter.ShowNumber.Get()
	showGroup := config.TUI.Chapter.ShowGroup.Get()
	showDate := config.TUI.Chapter.ShowDate.Get()

	_styles := defaultStyles()
	renderedSep := _styles.sep.Render(icon.Separator.Raw())
	listWrapper := list.New(
		3, 1,
		"chapter", "chapters",
		chapters,
		func(chapter mangadata.Chapter) _list.DefaultItem {
			volNum := fmt.Sprintf(config.TUI.Chapter.VolumeNumberFormat.Get(), chapter.Volume())
			chapNum := fmt.Sprintf(config.TUI.Chapter.NumberFormat.Get(), chapter.Info().Number)

			item := &item{
				chapter:               chapter,
				client:                client,
				renderedSep:           renderedSep,
				renderedVolumeNumber:  volNum,
				renderedChapterNumber: chapNum,
				showVolumeNumber:      &showVolumeNumber,
				showChapterNumber:     &showChapterNumber,
				showGroup:             &showGroup,
				showDate:              &showDate,
				styles:                _styles,
			}
			item.updatePaths()
			item.updateDownloadedFormats()
			item.updateReadAvailablePath()

			return item
		},
	)

	s := &state{
		list:              listWrapper,
		meta:              metadata.New(manga.Metadata()),
		confirm:           confirm.New(30, color.Success),
		formats:           format.New(color.Viewport),
		chapters:          chapters,
		volume:            volume,
		manga:             manga,
		client:            client,
		selected:          set.NewMapset[*item](),
		renderedSep:       renderedSep,
		confirmState:      cSDownloadNone,
		showVolumeNumber:  &showVolumeNumber,
		showChapterNumber: &showChapterNumber,
		showGroup:         &showGroup,
		showDate:          &showDate,
		styles:            _styles,
		keyMap:            newKeyMap(),
	}
	s.updateKeybinds()
	return s
}
