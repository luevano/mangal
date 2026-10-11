package download

import (
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/timer"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/theme/icon"
	"github.com/luevano/mangal/v5/theme/style"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/model/viewport"
	"github.com/luevano/mangal/v5/util/chapter"
)

func New(client *libmangal.Client, chaptersToDownload []mangadata.Chapter, options libmangal.DownloadOptions) *state {
	c := make(chapter.Chapters, len(chaptersToDownload))
	for i, ch := range chaptersToDownload {
		c[i] = &chapter.Chapter{
			Chapter: ch,
		}
	}

	_styles := defaultStyles()
	sep := _styles.sep.Render(icon.Separator.Raw())

	_viewport := viewport.New()
	_keyMap := newKeyMap(&_viewport.KeyMap)
	return &state{
		progress: progress.New(),
		spinner: spinner.New(
			spinner.WithSpinner(base.DotSpinner),
			spinner.WithStyle(style.Normal.Accent),
		),
		timer:       timer.New(time.Second),
		viewport:    _viewport,
		client:      client,
		chapters:    c,
		options:     options,
		downloading: dSUninitialized,
		maxRetries:  10, // TODO: make it configurable
		sep:         sep,
		message:     "Preparing...",
		styles:      _styles,
		keyMap:      _keyMap,
	}
}
