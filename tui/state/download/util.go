package download

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

func (s *state) viewDownloading() string {
	progress := 0.0
	if len(s.batch) != 0 {
		progress = float64(s.done) / float64(len(s.batch))
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		s.viewSummary(),
		" ",
		s.progress.ViewAs(progress),
		" ",
		s.spinner.View()+" "+style.Normal.Secondary.Render(s.message),
	)
}

func (s *state) viewSummary() string {
	if len(s.batch) == 0 {
		return ""
	}
	ch := s.batch[s.current].Chapter
	if s.attempt != 0 {
		return fmt.Sprintf(
			`%s Retrying chapter %s "%s" in %s%s%d/%d`,
			icon.Timer.Colored(),
			s.styles.accent.Render(stringutil.FormatFloa32(ch.Info().Number)),
			s.styles.accent.Render(ch.String()),
			s.timer.View(),
			s.sep, s.current+1, len(s.batch),
		)
	}
	return fmt.Sprintf(
		`%s Downloading chapter %s "%s"%s%d/%d`,
		icon.Progress.Colored(),
		s.styles.accent.Render(stringutil.FormatFloa32(ch.Info().Number)),
		s.styles.accent.Render(ch.String()),
		s.sep, s.current+1, len(s.batch),
	)
}

func (s *state) viewDownloaded() string {
	down, succ, fail := s.results.GetEach()
	var lines []string
	if len(down) != 0 {
		lines = append(lines, s.viewChapterList("To download:", s.styles.toDownload, down))
	}
	if len(succ) != 0 {
		lines = append(lines, s.viewChapterList("Succeeded:", s.styles.succeed, succ))
	}
	if len(fail) != 0 {
		lines = append(lines, s.viewChapterList("Failed:", s.styles.failed, fail))
	}
	if unavailable := s.results.Unavailable(); len(unavailable) != 0 {
		lines = append(lines, s.viewChapterList("Unavailable:", s.styles.warning, unavailable))
	}
	expl := "\nThe lists follow the template:\n" +
		"`<number> - <title> - s: <download status> - f: <filename>`"
	lines = append(lines, s.styles.toDownload.Render(expl))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (s *state) viewChapterList(header string, headerStyle lipgloss.Style, chaps client.Results) string {
	var sb strings.Builder
	sb.Grow(200)

	sb.WriteString(headerStyle.Render(header))
	sb.WriteByte('\n')

	item := s.styles.itemEnum.Foreground(headerStyle.GetForeground()).Render(icon.Item.Raw())
	subItm := s.styles.subItemEnum.Foreground(headerStyle.GetForeground()).Render(icon.SubItem.Raw())
	var lastDir string
	for i, ch := range chaps {
		if ch.Down != nil && ch.Down.Directory != lastDir {
			lastDir = ch.Down.Directory
			sb.WriteString(item)
			sb.WriteString(s.styles.toDownload.Render(lastDir))
			sb.WriteByte('\n')
		}
		sb.WriteString(subItm)
		sb.WriteString(renderChapter(ch, style.Normal.Base, s.styles.toDownload, s.styles.succeed, s.styles.failed, s.styles.warning))
		if i < len(chaps)-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String()
}

// renderChapter is the chapter with its download status and file.
func renderChapter(c *client.Result, normal, toDownload, succeed, failed, warning lipgloss.Style) string {
	str := stringutil.FormatFloa32(c.Chapter.Info().Number) + " - " + c.Chapter.Info().Title
	if c.Unavailable != "" {
		return toDownload.Render(str + " - " + c.Unavailable)
	}
	if c.Err != nil {
		return toDownload.Render(str+" - ") + failed.Render(c.Err.Error())
	}
	if c.Down == nil {
		return toDownload.Render(str)
	}

	s := toDownload
	status := "s: " + string(c.Down.ChapterStatus)
	switch c.Down.ChapterStatus {
	case metadata.DownloadStatusNew:
		s = normal
		status = succeed.Render(status)
	case metadata.DownloadStatusMissingMetadata, metadata.DownloadStatusOverwritten, metadata.DownloadStatusSkip:
		status = warning.Render(status)
	case metadata.DownloadStatusFailed:
		status = failed.Render(status)
	default: // exists or other
		status = toDownload.Render(status)
	}
	if c.Down.Skipped != "" {
		status = warning.Render("s: skipped, " + c.Down.Skipped)
	}
	return s.Render(str+" - ") + status + s.Render(" - f: "+c.Down.Filename)
}
