package chapters

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/tui/base"
	"github.com/luevano/mangal/v5/tui/state/download"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/skratchdot/open-golang/open"
)

// errNoMetadata is a download with download.metadata.strict of a manga without valid
// metadata, libmangal would refuse it.
var errNoMetadata = base.ConfigError(errors.New("no valid metadata (no AniList match?): bind one with the AniList screen (A), or press s to turn off download.metadata.strict in the config (, from any screen)"), config.Download.Metadata.Strict.Key)

// downloadMsg is a confirmed download: of one chapter (to read it after if read), or of
// the toggled ones on the download screen.
type downloadMsg struct {
	item     *item
	read     bool
	chapters []mangadata.Chapter
}

type downloadedMsg struct {
	item *item
	down *lmmeta.DownloadedChapter
	read bool
}

// waitMsg is a rate limit the download waits out.
type waitMsg struct {
	attempt int
	wait    time.Duration
	next    tea.Cmd
}

const busyNotice = "Wait for the running download or read"

// target is the toggled chapter, else the hovered one.
func (s *state) target(hovered *item) *item {
	if s.selected.Size() == 1 {
		return s.selected.Keys()[0]
	}
	return hovered
}

func (s *state) askDownload(ctx context.Context, hovered *item) tea.Cmd {
	if base.Busy(ctx) {
		return base.Notify(busyNotice)
	}
	if size := s.selected.Size(); size > 1 {
		items := s.selected.Keys()
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].chapter.Info().Number < items[j].chapter.Info().Number
		})
		chapters := make([]mangadata.Chapter, len(items))
		for i, it := range items {
			chapters[i] = it.chapter
		}
		msg := "Download " + stringutil.Quantify(size, "chapter", "chapters") + "?"
		s.confirm.Ask("Download", msg, s.View(), func() tea.Msg { return downloadMsg{chapters: chapters} })
		return nil
	}

	i := s.target(hovered)
	if reason := i.unavailable(); reason != "" {
		return base.Notify("Chapter unavailable: " + reason)
	}
	msg := "Download chapter " + stringutil.FormatFloa32(i.chapter.Info().Number) + ` ("` + i.chapter.Info().Title + `")?`
	s.confirm.Ask("Download", msg, s.View(), func() tea.Msg { return downloadMsg{item: i} })
	return nil
}

func (s *state) askRead(ctx context.Context, hovered *item) tea.Cmd {
	if base.Busy(ctx) {
		return base.Notify(busyNotice)
	}
	if s.selected.Size() > 1 {
		return base.Notify("Can't open for reading more than 1 chapter")
	}
	i := s.target(hovered)
	if reason := i.unavailable(); reason != "" {
		return base.Notify("Chapter unavailable: " + reason)
	}
	if i.readPath != "" {
		log.Debug("read format of %q already downloaded", i.chapter)
		return s.readCmd(ctx, i.readPath, i.chapter)
	}
	msg := "Download chapter " + stringutil.FormatFloa32(i.chapter.Info().Number) + ` ("` + i.chapter.Info().Title + `") for reading?`
	s.confirm.Ask("Download", msg, s.View(), func() tea.Msg { return downloadMsg{item: i, read: true} })
	return nil
}

// downloadOptions are the config's, for reading if read.
func downloadOptions(read bool) libmangal.DownloadOptions {
	options := config.DownloadOptions()
	// searched when the manga was opened, a search here would replace a bound one
	options.SearchMetadata = false
	if !read {
		return options
	}
	options.Format = config.Read.Format.Get()
	// without download_on_read, to the temp dir with all the dirs
	if !config.Read.DownloadOnRead.Get() {
		options.Directory = path.Temp()
		options.CreateProviderDir = true
		options.CreateMangaDir = true
		options.CreateVolumeDir = true
	}
	return options
}

// checkMetadata fails like libmangal would without valid metadata in strict mode, saying
// what to do.
func checkMetadata(manga mangadata.Manga, options libmangal.DownloadOptions) error {
	if !options.Strict {
		return nil
	}
	if err := lmmeta.Validate(manga.Metadata()); err != nil {
		log.Info("manga %q: %s", manga, err)
		return errNoMetadata
	}
	return nil
}

func (s *state) download(ctx context.Context, msg downloadMsg) tea.Cmd {
	options := downloadOptions(msg.read)
	if err := checkMetadata(s.manga, options); err != nil {
		return func() tea.Msg { return err }
	}
	if msg.item == nil {
		return func() tea.Msg { return download.New(s.client, msg.chapters, options) }
	}
	if base.Busy(ctx) {
		return base.Notify(busyNotice)
	}

	i, chapter := msg.item, msg.item.chapter
	if path := i.downloaded[options.Format]; path != "" && !msg.read {
		return base.Notify(fmt.Sprintf("Chapter %q already downloaded in %s format", chapter, options.Format))
	}
	waits := make(chan waitMsg)
	c := s.client
	load := base.Load(ctx, fmt.Sprintf("Downloading %q", chapter), func(ctx context.Context) (downloadedMsg, error) {
		defer close(waits)
		if options.Directory == path.Temp() {
			if _, err := path.EnsureTemp(); err != nil {
				return downloadedMsg{}, err
			}
		}
		down, err := client.DownloadChapter(ctx, c, chapter, options, func(attempt int, wait time.Duration) {
			select {
			case waits <- waitMsg{attempt: attempt, wait: wait}:
			case <-ctx.Done():
			}
		})
		if err != nil {
			return downloadedMsg{}, fmt.Errorf("download %q: %w", chapter, err)
		}
		return downloadedMsg{item: i, down: down, read: msg.read}, nil
	}, func(msg downloadedMsg) tea.Msg { return msg })
	return tea.Batch(load, listenWaits(waits))
}

func listenWaits(waits <-chan waitMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-waits
		if !ok {
			return nil
		}
		msg.next = listenWaits(waits)
		return msg
	}
}

func (s *state) onDownloaded(ctx context.Context, msg downloadedMsg) tea.Cmd {
	chapter := msg.item.chapter
	log.Info("downloaded chapter %q at %q", chapter, msg.down.Path())
	refresh := s.refresh(ctx, false)
	switch {
	case msg.read:
		return tea.Batch(refresh, s.readCmd(ctx, msg.down.Path(), chapter))
	case msg.down.Skipped != "":
		return tea.Batch(refresh, base.Notify(fmt.Sprintf("Skipped %q: %s", chapter, msg.down.Skipped)))
	}
	return tea.Batch(refresh, base.Notify(fmt.Sprintf("Downloaded %q", chapter)))
}

// readMsg is a chapter opened for reading, err is from opening or syncing it.
type readMsg struct{ err error }

func (s *state) readCmd(ctx context.Context, path string, chapter mangadata.Chapter) tea.Cmd {
	c := s.client
	return base.Load(ctx, fmt.Sprintf("Opening %q for reading", chapter), func(ctx context.Context) (readMsg, error) {
		// a failed sync still opened (and recorded) it, the marks refresh either way
		return readMsg{err: client.ReadChapter(ctx, c, path, chapter)}, nil
	}, func(msg readMsg) tea.Msg { return msg })
}

func openURLCmd(chapter mangadata.Chapter) tea.Cmd {
	url := chapter.Info().URL
	return func() tea.Msg {
		if err := open.Start(url); err != nil {
			return fmt.Errorf("open %s: %w", url, err)
		}
		return nil
	}
}
