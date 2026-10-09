package download

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/util"
	"github.com/skratchdot/open-golang/open"
)

func (s *state) startDownloadCmd() tea.Msg {
	s.downloading = dSDownloading
	s.currentIdx = 0
	s.toDownload = s.chapters.ToDownload()
	return nextChapterMsg{}
}

func (s *state) downloadChapterCmd(ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		var (
			ch       = s.toDownload[s.currentIdx]
			downChap *metadata.DownloadedChapter
			err      error
		)

		downChap, err = s.client.DownloadChapter(ctx, ch.Chapter, s.options)
		if err != nil {
			// on too many requests error
			if wait, ok := util.RetryAfter(err); ok {
				s.retryCount++
				if s.retryCount > s.maxRetries {
					return fmt.Errorf("exceeded max retries (%d) while downloading chapters", s.maxRetries)
				}
				return retryChapterMsg{After: min(10*time.Second, wait)}
			}
		}
		ch.Down = downChap
		ch.Err = err

		if s.currentIdx+1 >= len(s.toDownload) {
			return downloadCompletedMsg{}
		}
		s.currentIdx++
		return nextChapterMsg{}
	}
}

// openCmd acts on the key press and thus relies on the keybinds being updated.
func (s *state) openCmd() tea.Msg {
	return open.Start(s.chapters[0].Down.Directory)
}

// retryCmd acts on the key press and thus relies on the keybinds being updated,
// will retry all failed chapters.
func (s *state) retryCmd() tea.Cmd {
	s.downloading = dSDownloading
	s.currentIdx = 0
	s.toDownload = s.chapters.Failed()
	return tea.Sequence(
		func() tea.Msg {
			return nextChapterMsg{}
		},
		s.Resize(s.size),
	)
}
