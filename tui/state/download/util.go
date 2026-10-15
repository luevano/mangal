package download

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/jobs"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// noteFor is how long a provider's note is shown.
const noteFor = 15 * time.Second

// viewActive is the job's place in the queue, or its progress.
func (s *state) viewActive() string {
	j := s.job
	var head string
	switch j.Status {
	case jobs.Queued:
		head = fmt.Sprintf("%s Queued behind %s%s%s",
			icon.Queued.Colored(), s.styles.accent.Render(j.Behind), s.sep,
			stringutil.Quantify(j.Ahead, "job", "jobs")+" ahead on "+strings.Join(j.Providers, ", "))
	case jobs.Waiting:
		head = icon.Timer.Colored() + " " + capitalize(j.Message)
	default:
		head = fmt.Sprintf("%s Downloading%s%d/%d", icon.Progress.Colored(), s.sep, min(j.Done+1, j.Total), j.Total)
	}
	progress := 0.0
	if j.Total != 0 {
		progress = min(1, (float64(j.Done)+j.Part)/float64(j.Total))
	}
	message := capitalize(j.Message)
	if j.Status == jobs.Queued {
		message = "Waiting for its turn"
	}
	if j.Note != "" && time.Since(j.NoteAt) < noteFor {
		message += s.sep + j.Note
	}
	line := s.spinner.View() + " " + style.Normal.Secondary.Render(message)
	if s.size.Width > 1 {
		// one line each, or the viewport below would be pushed off
		line = ansi.Truncate(line, s.size.Width-1, "…")
		head = ansi.Truncate(head, s.size.Width-1, "…")
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		head,
		" ",
		s.progress.ViewAs(progress),
		" ",
		line,
	)
}

// capitalize upper-cases the first letter, client's progress lines are lowercase.
func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

func (s *state) viewDownloaded() string {
	down, succ, fail := s.job.Results.GetEach()
	var lines []string
	switch {
	case s.gone:
		lines = append(lines, s.styles.warning.Render("This download isn't listed anymore."))
	case s.job.Status == jobs.Cancelled:
		lines = append(lines, s.styles.warning.Render("Cancelled."))
	case s.job.Err != nil:
		lines = append(lines, s.styles.failed.Render(capitalize(s.job.Err.Error())))
	case s.job.Status == jobs.Done:
		lines = append(lines, s.styles.succeed.Render(capitalize(s.job.Message)))
	}
	if len(down) != 0 {
		lines = append(lines, s.viewChapterList("To download:", s.styles.toDownload, down))
	}
	if len(succ) != 0 {
		lines = append(lines, s.viewChapterList("Succeeded:", s.styles.succeed, succ))
	}
	if len(fail) != 0 {
		lines = append(lines, s.viewChapterList("Failed:", s.styles.failed, fail))
	}
	if unavailable := s.job.Results.Unavailable(); len(unavailable) != 0 {
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
	str := stringutil.FormatFloat32(c.Chapter.Info().Number) + " - " + c.Chapter.Info().Title
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
