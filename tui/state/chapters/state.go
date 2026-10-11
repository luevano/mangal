package chapters

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/confirm"
	"github.com/luevano/mangal/v5/tui/model/format"
	"github.com/luevano/mangal/v5/tui/model/list"
	"github.com/luevano/mangal/v5/tui/model/metadata"
	"github.com/luevano/mangal/v5/tui/state/anilist"
	"github.com/luevano/mangal/v5/tui/util"
	"github.com/zyedidia/generic/set"
)

var _ base.State = (*state)(nil)

type confirmState uint8

const (
	cSDownloadNone confirmState = iota
	cSDownloadHovered
	cSDownloadSelected
	cSDownloadForRead
)

// state implements base.state.
type state struct {
	list    *list.Model
	meta    *metadata.Model
	confirm *confirm.Model
	formats *format.Model

	chapters []mangadata.Chapter
	volume   mangadata.Volume // can be nil
	manga    mangadata.Manga
	client   *libmangal.Client

	selected set.Set[*item]

	previousFrame,
	renderedSep,
	renderedSubtitleFormats string

	// to avoid extra read/download cmds from
	// firing up when an action is already happening,
	// only blocks keymaps handled by Update for read/download
	actionRunning string

	// if the actions that require an item are available
	actionItemAvailable bool

	inConfirm,
	inFormats bool

	confirmState confirmState

	showVolumeNumber  *bool
	showChapterNumber *bool
	showGroup         *bool
	showDate          *bool

	size   base.Size
	styles styles
	keyMap keyMap
}

// Intermediate implements base.State.
func (s *state) Intermediate() bool {
	return false
}

// Backable implements base.State.
func (s *state) Backable() bool {
	return s.list.Unfiltered() && !s.inFormats && !s.inConfirm
}

// KeyMap implements base.State.
func (s *state) KeyMap() help.KeyMap {
	return base.CombinedKeyMap(s.keyMap, s.list.KeyMap)
}

// Title implements base.State.
func (s *state) Title() base.Title {
	if s.volume != nil {
		return base.Title{Text: "Volume " + s.volume.String()}
	}
	return base.Title{Text: s.manga.String()}
}

// Subtitle implements base.State.
func (s *state) Subtitle() string {
	var subtitle strings.Builder
	subtitle.Grow(100)

	subtitle.WriteString(s.list.Subtitle())
	if s.selected.Size() > 0 {
		selected := s.renderedSep +
			s.styles.subtitle.Render(fmt.Sprintf("%d selected", s.selected.Size()))
		subtitle.WriteString(selected)
	}
	subtitle.WriteString(s.renderedSubtitleFormats)

	return subtitle.String()
}

// Status implements base.State.
func (s *state) Status() string {
	return s.meta.View() + " " + s.list.Status()
}

// Resize implements base.State.
func (s *state) Resize(size base.Size) tea.Cmd {
	s.size = size
	return s.list.Resize(size)
}

// Init implements base.State.
func (s *state) Init(ctx context.Context) tea.Cmd {
	s.updateRenderedSubtitleFormats()
	return tea.Sequence(
		s.list.Init(),
		s.confirm.Init(),
		s.formats.Init(),
	)
}

// Update implements base.State.
func (s *state) Update(ctx context.Context, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// skip keybind handling, let the models handle these events
		if s.list.Filtering() || s.inFormats || s.inConfirm {
			goto end
		}

		// don't return on nil item, keybinds will be disabled for relevant actions
		i, _ := s.list.SelectedItem().(*item)
		switch {
		case key.Matches(msg, s.keyMap.toggle):
			if reason := i.unavailable(); reason != "" {
				return base.Notify("Chapter unavailable: " + reason)
			}
			i.toggle()
			if i.selected {
				s.selected.Put(i)
			} else {
				s.selected.Remove(i)
			}
			return nil
		case key.Matches(msg, s.keyMap.read):
			return s.readCmd(ctx, i)
		case key.Matches(msg, s.keyMap.download):
			return s.downloadCmd(i)
		case key.Matches(msg, s.keyMap.info):
			s.meta.ShowFull = !s.meta.ShowFull
		case key.Matches(msg, s.keyMap.anilist):
			ani, err := s.client.GetMetadataProvider(lmmeta.IDSourceAnilist)
			if err != nil {
				return func() tea.Msg {
					return err
				}
			}
			return func() tea.Msg {
				return anilist.New(ani, s.manga)
			}
		case key.Matches(msg, s.keyMap.metadata):
			return s.meta.ShowMetadataCmd()
		case key.Matches(msg, s.keyMap.changeFormat):
			s.previousFrame = s.View()
			s.inFormats = true
		case key.Matches(msg, s.keyMap.openURL):
			return s.openURLCmd(i.chapter)
		case key.Matches(msg, s.keyMap.selectAll):
			for _, listItem := range s.list.Items() {
				it, ok := listItem.(*item)
				if !ok {
					continue
				}

				if !it.selected && it.unavailable() == "" {
					it.toggle()
					s.selected.Put(it)
				}
			}
			return nil
		case key.Matches(msg, s.keyMap.unselectAll):
			for _, item := range s.selected.Keys() {
				item.toggle()
				s.selected.Remove(item)
			}
			return nil
		case key.Matches(msg, s.keyMap.toggleVolumeNumber):
			*s.showVolumeNumber = !(*s.showVolumeNumber)
		case key.Matches(msg, s.keyMap.toggleChapterNumber):
			*s.showChapterNumber = !(*s.showChapterNumber)
		case key.Matches(msg, s.keyMap.toggleGroup):
			*s.showGroup = !(*s.showGroup)
			s.updateListDelegate()
		case key.Matches(msg, s.keyMap.toggleDate):
			*s.showDate = !(*s.showDate)
			s.updateListDelegate()
		}
	case showConfirmMsg:
		s.previousFrame = s.View()
		s.inConfirm = true
		s.confirmState = msg.state
		s.confirm.SetData(msg.title, msg.message)
	case confirm.YesMsg:
		return s.onConfirmCmd(ctx)
	case confirm.NoMsg:
		s.inConfirm = false
		s.confirmState = cSDownloadNone
	case format.BackMsg:
		s.inFormats = false
		s.updateAllItems()
		s.updateRenderedSubtitleFormats()
	case base.RestoredMsg:
		// in case the metadata was updated in the anilist state
		s.meta.SetMetadata(s.manga.Metadata())
		// usually the downloaded chapters change or the metadata when restoring the chapter list
		s.updateAllItems()
		s.updateRenderedSubtitleFormats()
	}
end:
	switch {
	case s.inConfirm:
		return s.confirm.Update(msg)
	case s.inFormats:
		return s.formats.Update(msg)
	default:
		return s.list.Update(msg)
	}
}

// View implements base.State.
func (s *state) View() string {
	switch {
	case s.inConfirm:
		cV := s.styles.confirmView.Render(s.confirm.View())
		w, h := lipgloss.Size(cV)
		return util.PlaceOverlay((s.size.Width-w)/2, (s.size.Height-h)/2, cV, s.previousFrame)
	case s.inFormats:
		fV := s.styles.formatView.Render(s.formats.View())
		w, h := lipgloss.Size(fV)
		return util.PlaceOverlay((s.size.Width-w)/2, (s.size.Height-h)/2, fV, s.previousFrame)
	default:
		return s.list.View()
	}
}
