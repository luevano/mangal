package chapters

import (
	"strings"

	"charm.land/bubbles/v2/list"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
)

var _ list.DefaultItem = (*item)(nil)

// show is what the items show, toggled by the State.
type show struct {
	volumeNumber,
	chapterNumber,
	group,
	date bool
}

type item struct {
	chapter mangadata.Chapter
	// selected means that the item is toggled on
	selected bool
	// read is in the read history
	read bool

	// downloaded are the library's files of the chapter by format, temp the ones read
	// without download_on_read
	downloaded map[libmangal.Format]string
	temp       map[libmangal.Format]string
	// readPath is the chapter in the read format (downloaded or temp), empty if none
	readPath string

	renderedSep               string
	renderedVolumeNumber      string
	renderedChapterNumber     string
	renderedDownloadedFormats string

	show   *show
	styles styles
	// jobMark is the mark of the chapter's download job (queued, downloading), may be nil
	jobMark func(mangadata.Chapter) string
}

func (i *item) FilterValue() string {
	var title strings.Builder
	title.Grow(50)
	if i.show.volumeNumber {
		title.WriteString(i.renderedVolumeNumber)
		title.WriteString(" ")
	}
	if i.show.chapterNumber {
		title.WriteString(i.renderedChapterNumber)
		title.WriteString(" ")
	}
	title.WriteString(i.chapter.Info().Title)
	return title.String()
}

func (i *item) Title() string {
	var title strings.Builder
	title.Grow(200)

	title.WriteString(i.FilterValue())

	if i.selected {
		title.WriteString(i.renderedSep)
		title.WriteString(icon.Mark.Colored())
	}

	if i.jobMark != nil {
		if mark := i.jobMark(i.chapter); mark != "" {
			title.WriteString(i.renderedSep)
			title.WriteString(mark)
		}
	}

	if i.read {
		title.WriteString(i.renderedSep)
		title.WriteString(icon.Read.Colored())
	}

	if i.readPath != "" {
		title.WriteString(i.renderedSep)
		title.WriteString(icon.Available.Colored())
	}

	if i.renderedDownloadedFormats != "" {
		title.WriteString(i.renderedSep)
		title.WriteString(i.renderedDownloadedFormats)
	}

	// grayed out with the reason, it can't be selected nor downloaded
	if reason := i.unavailable(); reason != "" {
		title.WriteString(" [")
		title.WriteString(reason)
		title.WriteString("]")
		return style.Normal.Secondary.Render(title.String())
	}

	return title.String()
}

// unavailable is the reason the chapter can't be downloaded, empty if it can.
func (i *item) unavailable() string {
	return i.chapter.Info().Unavailable
}

func (i *item) Description() string {
	chapterInfo := i.chapter.Info()
	if !i.show.date && !i.show.group {
		return chapterInfo.URL
	}

	var description strings.Builder
	description.Grow(200)

	if i.show.date {
		description.WriteString(style.Bold.Secondary.Render(chapterInfo.Date.String()))
		description.WriteString(" ")
	}
	if i.show.group {
		description.WriteString(style.Italic.Secondary.Render(chapterInfo.ScanlationGroup))
	}

	description.WriteString("\n")
	description.WriteString(chapterInfo.URL)
	return description.String()
}

func (i *item) toggle() {
	i.selected = !i.selected
}

// setFiles sets where the chapter is downloaded, and renders it.
func (i *item) setFiles(downloaded, temp map[libmangal.Format]string, readFormat libmangal.Format) {
	i.downloaded, i.temp = downloaded, temp
	i.setReadFormat(readFormat)

	i.renderedDownloadedFormats = ""
	if len(i.downloaded) == 0 {
		return
	}
	var formats strings.Builder
	formats.Grow(50)
	formats.WriteString(icon.Download.Colored())
	// always in the same order
	for _, format := range libmangal.FormatValues() {
		if i.downloaded[format] == "" {
			continue
		}
		formats.WriteString(" ")
		formats.WriteString(i.styles.format.Render(format.String()))
	}
	i.renderedDownloadedFormats = formats.String()
}

// setReadFormat picks the file to read, the download over the temp one.
func (i *item) setReadFormat(format libmangal.Format) {
	i.readPath = i.downloaded[format]
	if i.readPath == "" {
		i.readPath = i.temp[format]
	}
}
