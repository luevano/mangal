package chapters

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmanilist "github.com/luevano/libmangal/metadata/anilist"
	"github.com/luevano/mangal/config"
	"github.com/luevano/mangal/log"
	"github.com/luevano/mangal/path"
	"github.com/luevano/mangal/tui/base"
	"github.com/luevano/mangal/tui/state/anilistmangas"
	"github.com/luevano/mangal/tui/state/confirm"
	"github.com/luevano/mangal/tui/state/formats"
	"github.com/luevano/mangal/tui/state/listwrapper"
	"github.com/luevano/mangal/tui/state/loading"
	stringutil "github.com/luevano/mangal/util/string"
	"github.com/skratchdot/open-golang/open"
	"github.com/zyedidia/generic/set"
)

var _ base.State = (*State)(nil)

// State implements base.State.
type State struct {
	list              *listwrapper.State
	chapters          []mangadata.Chapter
	volume            mangadata.Volume
	manga             mangadata.Manga
	client            *libmangal.Client
	selected          set.Set[*Item]
	keyMap            keyMap
	showChapterNumber *bool
	showGroup         *bool
	showDate          *bool
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
	return base.Title{Text: fmt.Sprintf("%s / Vol. %s", s.manga, s.volume)}
}

// Subtitle implements base.State.
func (s *State) Subtitle() string {
	var subtitle strings.Builder

	subtitle.WriteString(s.list.Subtitle())

	if s.selected.Size() > 0 {
		subtitle.WriteString(" ")
		subtitle.WriteString(fmt.Sprint(s.selected.Size()))
		subtitle.WriteString(" selected")
	}

	subtitle.WriteString(". Download ")
	subtitle.WriteString(config.Download.Format.Get().String())
	subtitle.WriteString(" & Read ")
	subtitle.WriteString(config.Read.Format.Get().String())

	return subtitle.String()
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

		item, ok := s.list.SelectedItem().(*Item)
		if !ok {
			return nil
		}

		switch {
		case key.Matches(msg, s.keyMap.toggle):
			item.Toggle()

			return nil
		case key.Matches(msg, s.keyMap.unselectAll):
			for _, item := range s.selected.Keys() {
				item.Toggle()
			}

			return nil
		case key.Matches(msg, s.keyMap.selectAll):
			for _, listItem := range s.list.Items() {
				// TODO: possibly issue here? item is re-declared, need to keep an eye
				item, ok := listItem.(*Item)
				if !ok {
					continue
				}

				if !item.IsSelected() {
					item.Toggle()
				}
			}

			return nil
		case key.Matches(msg, s.keyMap.changeFormat):
			return func() tea.Msg {
				return formats.New()
			}
		case key.Matches(msg, s.keyMap.openURL):
			return tea.Sequence(
				func() tea.Msg {
					return loading.New("Opening", item.chapter.String())
				},
				func() tea.Msg {
					err := open.Run(item.chapter.Info().URL)
					if err != nil {
						return err
					}

					return base.Back
				},
			)
		case key.Matches(msg, s.keyMap.download) || (s.selected.Size() > 0 && key.Matches(msg, s.keyMap.confirm)):
			var chapters []mangadata.Chapter

			if s.selected.Size() == 0 {
				chapters = append(chapters, item.chapter)
			} else {
				for _, item := range s.selected.Keys() {
					chapters = append(chapters, item.chapter)
				}
			}

			sort.SliceStable(chapters, func(i, j int) bool {
				return chapters[i].Info().Number < chapters[j].Info().Number
			})

			return func() tea.Msg {
				return confirm.New(
					fmt.Sprint("Download ", stringutil.Quantify(len(chapters), "chapter", "chapters")),
					func(response bool) tea.Cmd {
						if !response {
							return base.Back
						}

						return s.downloadChaptersCmd(chapters, config.DownloadOptions())
					},
				)
			}
		case key.Matches(msg, s.keyMap.read) || (s.selected.Size() == 0 && key.Matches(msg, s.keyMap.confirm)):
			// If download on read is wanted, then use the normal download path
			var directory string
			if config.Read.DownloadOnRead.Get() {
				directory = path.DownloadsDir()
			} else {
				directory = path.TempDir()
			}

			// Modify a bit the configured download options for this
			downloadOptions := config.DownloadOptions()
			downloadOptions.Format = config.Read.Format.Get()
			downloadOptions.Directory = directory
			downloadOptions.SkipIfExists = true
			downloadOptions.ReadAfter = true
			downloadOptions.CreateProviderDir = true
			downloadOptions.CreateMangaDir = true
			downloadOptions.CreateVolumeDir = true

			if item.DownloadedFormats().Has(downloadOptions.Format) {
				return tea.Sequence(
					func() tea.Msg {
						return loading.New("Opening for reading", item.chapter.String())
					},
					func() tea.Msg {
						err := s.client.ReadChapter(
							model.Context(),
							item.Path(downloadOptions.Format),
							item.chapter,
							downloadOptions.ReadOptions,
						)
						if err != nil {
							return err
						}

						return base.Back
					},
				)
			}

			return s.downloadChapterCmd(model.Context(), item.chapter, downloadOptions)
		// TODO: refactor/fix this so that the metadata is propagated (probably needs a change on libmangal itself)
		case key.Matches(msg, s.keyMap.anilist):
			return tea.Sequence(
				func() tea.Msg {
					return loading.New("Searching", "Getting Anilist Mangas")
				},
				func() tea.Msg {
					var mangas []lmanilist.Manga

					// TODO: solidify the metadata gathering, missing/partial
					// TODO: revert to just Title instead of AnilistSearch?
					var mangaTitle string
					mangaInfo := item.chapter.Volume().Manga().Info()
					if mangaInfo.AnilistSearch != "" {
						mangaTitle = mangaInfo.AnilistSearch
					} else {
						mangaTitle = mangaInfo.Title
					}

					closest, ok, err := s.client.Anilist().FindClosestManga(model.Context(), mangaTitle)
					if err != nil {
						return err
					}

					if ok {
						mangas = append(mangas, closest)
					}

					mangaSearchResults, err := s.client.Anilist().SearchMangas(model.Context(), mangaTitle)
					if err != nil {
						return nil
					}

					for _, manga := range mangaSearchResults {
						if manga.ID == closest.ID {
							continue
						}

						mangas = append(mangas, manga)
					}

					return anilistmangas.New(
						s.client.Anilist(),
						mangas,
						func(response *lmanilist.Manga) tea.Cmd {
							return tea.Sequence(
								func() tea.Msg {
									log.Log("Setting Anilist manga %q (%d)", response.String(), response.ID)
									s.manga.SetMetadata(response.Metadata())

									return base.Back
								},
								s.list.Notify(fmt.Sprintf("Set Anilist %s (%d)", response.String(), response.ID), 3*time.Second),
							)
						},
					)
				},
			)
		case key.Matches(msg, s.keyMap.toggleChapterNumber):
			*s.showChapterNumber = !(*s.showChapterNumber)

			return s.list.Update(model, msg)
		case key.Matches(msg, s.keyMap.toggleGroup):
			*s.showGroup = !(*s.showGroup)

			return s.list.Update(model, msg)
		case key.Matches(msg, s.keyMap.toggleDate):
			*s.showDate = !(*s.showDate)

			return s.list.Update(model, msg)
		}
	}

end:
	return s.list.Update(model, msg)
}

// View implements base.State.
func (s *State) View(model base.Model) string {
	return s.list.View(model)
}
