package chapter

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// Chapter represents a manga chapter with possible
// downloaded chapter information or an error if failed.
type Chapter struct {
	Chapter mangadata.Chapter
	Down    *metadata.DownloadedChapter
	Err     error
}

// ToDownload returns true if it is yet to be downloaded
// (its error and downloaded chapter info are nil).
func (c Chapter) ToDownload() bool {
	return !c.Failed() && c.Down == nil
}

// Succeed returns true if it downloaded successfully
// (error is nil and downloaded chapter info is non-nil).
func (c Chapter) Succeed() bool {
	return !c.Failed() && c.Down != nil
}

// Failed returns true if it failed to download
// (error is non-nil).
func (c Chapter) Failed() bool {
	return c.Err != nil
}

// Render will render a chapter information with styling applied. Useful for TUI rendering.
func (c Chapter) Render(normal, toDownload, succeed, failed, warning lipgloss.Style) string {
	str := stringutil.FormatFloa32(c.Chapter.Info().Number) + " - " + c.Chapter.Info().Title
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
	str = s.Render(str+" - ") + status + s.Render(" - f: "+c.Down.Filename)
	return str
}

type Chapters []*Chapter

// GetEach will return its ToDownload, Succeed and Failed chapters in separate groups.
func (c Chapters) GetEach() (d, s, f Chapters) {
	for _, ch := range c {
		switch {
		case ch.ToDownload():
			d = append(d, ch)
		case ch.Succeed():
			s = append(s, ch)
		case ch.Failed():
			f = append(f, ch)
		default:
			panic("unexpected chapter download state")
		}
	}
	return d, s, f
}

// ToDownload returns the Chapters that are yet to be downloaded.
func (c Chapters) ToDownload() (d Chapters) {
	for _, ch := range c {
		if ch.ToDownload() {
			d = append(d, ch)
		}
	}
	return d
}

// Succeed returns the Chapters that were downloaded successfully.
func (c Chapters) Succeed() (s Chapters) {
	for _, ch := range c {
		if ch.Succeed() {
			s = append(s, ch)
		}
	}
	return s
}

// Existent returns the successfully 'downloaded' chapters that already existed.
func (c Chapters) Existent() (n Chapters) {
	for _, ch := range c {
		if ch.Succeed() && ch.Down.ChapterStatus == metadata.DownloadStatusExists {
			n = append(n, ch)
		}
	}
	return n
}

// Failed returns the Chapters that failed to download.
func (c Chapters) Failed() (f Chapters) {
	for _, ch := range c {
		if ch.Failed() {
			f = append(f, ch)
		}
	}
	return f
}
