package chapters

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/config"
	"github.com/luevano/mangal/path"
	"github.com/luevano/mangal/theme/icon"
	"github.com/luevano/mangal/theme/style"
	"github.com/luevano/mangal/util/afs"
	"github.com/zyedidia/generic/set"
)

var (
	_ list.Item        = (*item)(nil)
	_ list.DefaultItem = (*item)(nil)
)

// item implements list.item.
type item struct {
	chapter mangadata.Chapter
	client  *libmangal.Client

	tempDownloadedFormats set.Set[libmangal.Format]
	downloadedFormats     set.Set[libmangal.Format]

	renderedSep               string
	renderedVolumeNumber      string
	renderedChapterNumber     string
	renderedDownloadedFormats string

	// selected means that the item is toggled on
	selected bool
	// path to the downloaded chapter in preferred read format,
	// prefers download directory path over temp path if existent,
	// if the read format is not available anywhere it is empty
	readAvailablePath string

	// full computed paths minus the filename
	fullTempPath     string
	fullDownloadPath string

	showVolumeNumber  *bool
	showChapterNumber *bool
	showGroup         *bool
	showDate          *bool

	styles styles
}

// FilterValue implements list.Item.
func (i *item) FilterValue() string {
	var title strings.Builder
	title.Grow(50)
	if *i.showVolumeNumber {
		title.WriteString(i.renderedVolumeNumber)
		title.WriteString(" ")
	}

	if *i.showChapterNumber {
		title.WriteString(i.renderedChapterNumber)
		title.WriteString(" ")
	}

	title.WriteString(i.chapter.Info().Title)
	return title.String()
}

// Title implements list.DefaultItem.
func (i *item) Title() string {
	var title strings.Builder
	title.Grow(200)

	title.WriteString(i.FilterValue())

	if i.selected {
		title.WriteString(i.renderedSep)
		title.WriteString(icon.Mark.Colored())
	}

	if i.readAvailablePath != "" {
		title.WriteString(i.renderedSep)
		title.WriteString(icon.Available.Colored())
	}

	if i.renderedDownloadedFormats != "" {
		title.WriteString(i.renderedSep)
		title.WriteString(i.renderedDownloadedFormats)
	}

	// grayed out with the reason, it can't be selected nor downloaded
	if reason := i.unavailable(); reason != "" {
		title.WriteString(" [" + reason + "]")
		return style.Normal.Secondary.Render(title.String())
	}

	return title.String()
}

// unavailable is the reason the chapter can't be downloaded, empty if it can.
func (i *item) unavailable() string {
	return i.chapter.Info().Unavailable
}

// Description implements list.DefaultItem.
func (i *item) Description() string {
	chapterInfo := i.chapter.Info()
	if !*i.showDate && !*i.showGroup {
		return chapterInfo.URL
	}

	var description strings.Builder
	description.Grow(200)

	if *i.showDate {
		description.WriteString(style.Bold.Secondary.Render(chapterInfo.Date.String()))
		description.WriteString(" ")
	}

	if *i.showGroup {
		description.WriteString(style.Italic.Secondary.Render(chapterInfo.ScanlationGroup))
	}

	description.WriteString("\n")
	description.WriteString(chapterInfo.URL)
	return description.String()
}

func (i *item) toggle() {
	i.selected = !i.selected
}

// path computes the full filepath to the (possibly) downloaded chapter
func (i *item) path(directory string, format libmangal.Format) string {
	return filepath.Join(directory, i.client.ChapterName(i.chapter, format))
}

// updatePaths should only be needed if the config changes
func (i *item) updatePaths() {
	client := i.client
	providerFilename := client.ProviderName(client.Info())
	mangaFilename := client.MangaName(i.chapter.Volume().Manga())
	volumeFilename := client.VolumeName(i.chapter.Volume())

	i.fullTempPath = filepath.Join(path.TempDir(), providerFilename, mangaFilename, volumeFilename)

	downloadPath := path.DownloadsDir()
	if config.Download.Provider.CreateDir.Get() {
		downloadPath = filepath.Join(downloadPath, providerFilename)
	}
	if config.Download.Manga.CreateDir.Get() {
		downloadPath = filepath.Join(downloadPath, mangaFilename)
	}
	if config.Download.Volume.CreateDir.Get() {
		downloadPath = filepath.Join(downloadPath, volumeFilename)
	}
	i.fullDownloadPath = downloadPath
}

// updateDownloadedFormats should only be computed as needed (when the chapter
// is downloaded for example), it also renders the downloaded formats for display
func (i *item) updateDownloadedFormats() {
	i.tempDownloadedFormats = set.NewMapset[libmangal.Format]()
	i.downloadedFormats = set.NewMapset[libmangal.Format]()

	for _, format := range libmangal.FormatValues() {
		for k, path := range map[string]string{
			"temp": i.path(i.fullTempPath, format),
			"down": i.path(i.fullDownloadPath, format),
		} {
			exists, err := afs.Afero.Exists(path)
			if err != nil {
				continue
			}

			if exists {
				switch k {
				case "temp":
					i.tempDownloadedFormats.Put(format)
				case "down":
					i.downloadedFormats.Put(format)
				}
			}
		}
	}

	i.renderDownloadedFormats()
}

// renderDownloadedFormats will create the string displayed
// next to the chapter name that shows the downloaded formats
func (i *item) renderDownloadedFormats() {
	i.renderedDownloadedFormats = ""

	if i.downloadedFormats.Size() > 0 {
		var formats strings.Builder
		formats.Grow(50)
		formats.WriteString(icon.Download.Colored())

		// So that formats will be displayed in the same order
		for _, format := range libmangal.FormatValues() {
			if !i.downloadedFormats.Has(format) {
				continue
			}

			formats.WriteString(" ")
			formats.WriteString(i.styles.format.Render(format.String()))
		}
		i.renderedDownloadedFormats = formats.String()
	}
}

// updateReadAvailablePath checks if the chapter is downloaded in read format in either temp or download dir
func (i *item) updateReadAvailablePath() {
	readFormat := config.Read.Format.Get()

	switch {
	case i.downloadedFormats.Has(readFormat):
		i.readAvailablePath = i.path(i.fullDownloadPath, readFormat)
	case i.tempDownloadedFormats.Has(readFormat):
		i.readAvailablePath = i.path(i.fullTempPath, readFormat)
	default:
		i.readAvailablePath = ""
	}
}
