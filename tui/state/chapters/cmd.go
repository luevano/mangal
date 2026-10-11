package chapters

import (
	"context"
	"errors"
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/state/download"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/skratchdot/open-golang/open"
	"github.com/zyedidia/generic/set"
)

func showConfirmCmd(title, message string, state confirmState) tea.Cmd {
	return func() tea.Msg {
		return showConfirmMsg{
			title:   title,
			message: message,
			state:   state,
		}
	}
}

func (s *state) onConfirmCmd(ctx context.Context) tea.Cmd {
	// save current state and update to none
	state := s.confirmState
	s.inConfirm = false
	s.confirmState = cSDownloadNone

	options := config.DownloadOptions()
	// Guaranteed to had been searched during mangas state,
	// this avoids re-searching during download and unsetting the
	// set metadata
	options.SearchMetadata = false

	// hovered
	i, _ := s.list.SelectedItem().(*item)
	switch state {
	case cSDownloadHovered:
		return s.downloadChapterCmd(ctx, i, options, false)
	case cSDownloadSelected:
		return s.downloadChaptersCmd(s.selected, options)
	case cSDownloadForRead:
		options.Format = config.Read.Format.Get()
		// if shouldn't download on read, save to tmp dir with all dirs created
		if !config.Read.DownloadOnRead.Get() {
			options.Directory = path.TempDir()
			options.CreateProviderDir = true
			options.CreateMangaDir = true
			options.CreateVolumeDir = true
		}
		return s.downloadChapterCmd(ctx, i, options, true)
	default:
		return func() tea.Msg {
			return errors.New("unexpected confirm yes msg on chapters state")
		}
	}
}

func (s *state) blockedActionByCmd(wanted string) tea.Cmd {
	return base.Notify(fmt.Sprintf("Can't perform %q right now, %q is running", wanted, s.actionRunning))
}

func (s *state) openURLCmd(chapter mangadata.Chapter) tea.Cmd {
	return tea.Sequence(
		base.Loading(fmt.Sprintf("Opening URL %s for chapter %q", chapter.Info().URL, chapter)),
		func() tea.Msg {
			err := open.Run(chapter.Info().URL)
			if err != nil {
				return err
			}

			return nil
		},
		base.Loaded,
	)
}

func (s *state) downloadCmd(item *item) tea.Cmd {
	if s.actionRunning != "" {
		return s.blockedActionByCmd("download")
	}

	size := s.selected.Size()
	// when no toggled chapters then just download the one hovered
	if size <= 1 {
		i := item
		if size == 1 {
			i = s.selected.Keys()[0]
		}
		if reason := i.unavailable(); reason != "" {
			return base.Notify("Chapter unavailable: " + reason)
		}
		msg := "Download chapter " +
			stringutil.FormatFloa32(i.chapter.Info().Number) +
			` ("` + i.chapter.Info().Title + `")?`
		return showConfirmCmd("Download", msg, cSDownloadHovered)
	}

	msg := "Download " + stringutil.Quantify(size, "chapter", "chapters") + "?"
	return showConfirmCmd("Download", msg, cSDownloadSelected)
}

func (s *state) downloadChapterCmd(ctx context.Context, item *item, options libmangal.DownloadOptions, readAfter bool) tea.Cmd {
	chapter := item.chapter

	if item.downloadedFormats.Has(options.Format) {
		return base.Notify(fmt.Sprintf("Chapter %q already downloaded in %s format", chapter, options.Format))
	}

	return tea.Sequence(
		base.Loading(fmt.Sprintf("Downloading %q", chapter)),
		func() tea.Msg {
			s.actionRunningNow("download")
			defer s.actionRunningNow("")

			downChap, err := client.DownloadChapter(ctx, s.client, chapter, options)
			if err != nil {
				return err
			}
			s.updateItem(item)

			log.Log("Downloaded chapter %q at %q", chapter, downChap.Path())
			if readAfter {
				return s.readChapterCmd(ctx, downChap.Path(), item, config.ReadOptions())()
			}
			return base.Notify(fmt.Sprintf("Downloaded %q", chapter))()
		},
		base.Loaded,
	)
}

func (s *state) downloadChaptersCmd(items set.Set[*item], options libmangal.DownloadOptions) tea.Cmd {
	return func() tea.Msg {
		s.actionRunningNow("download")
		defer s.actionRunningNow("")

		var chapters []mangadata.Chapter
		for _, item := range items.Keys() {
			chapters = append(chapters, item.chapter)
		}
		sort.SliceStable(chapters, func(i, j int) bool {
			return chapters[i].Info().Number < chapters[j].Info().Number
		})

		return download.New(s.client, chapters, options)
	}
}

func (s *state) readCmd(ctx context.Context, item *item) tea.Cmd {
	if s.actionRunning != "" {
		return s.blockedActionByCmd("read")
	}

	// when no toggled chapters then just download the one selected
	if s.selected.Size() > 1 {
		return base.Notify("Can't open for reading more than 1 chapter")
	}

	// use the toggled item, else the hovered one
	i := item
	if s.selected.Size() == 1 {
		i = s.selected.Keys()[0]
	}
	if reason := i.unavailable(); reason != "" {
		return base.Notify("Chapter unavailable: " + reason)
	}

	if i.readAvailablePath != "" {
		log.Log("Read format already downloaded")
		return s.readChapterCmd(ctx, i.readAvailablePath, i, config.ReadOptions())
	}

	msg := "Download chapter " +
		stringutil.FormatFloa32(i.chapter.Info().Number) +
		` ("` + i.chapter.Info().Title + `") for reading?`
	return showConfirmCmd("Download", msg, cSDownloadForRead)
}

func (s *state) readChapterCmd(ctx context.Context, path string, item *item, options libmangal.ReadOptions) tea.Cmd {
	chapter := item.chapter

	return tea.Sequence(
		base.Loading(fmt.Sprintf("Opening %q for reading", chapter)),
		func() tea.Msg {
			s.actionRunningNow("read")
			defer s.actionRunningNow("")

			err := s.client.ReadChapterContext(ctx, path, chapter, options)
			if err != nil {
				return err
			}

			return nil
		},
		base.Loaded,
	)
}
