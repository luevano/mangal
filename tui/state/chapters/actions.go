package chapters

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	lmmeta "github.com/luevano/libmangal/metadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/jobs"
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	"github.com/luevano/mangal/v5/tui/base"
	tuijobs "github.com/luevano/mangal/v5/tui/jobs"
	"github.com/luevano/mangal/v5/tui/state/downloads"
	stringutil "github.com/luevano/mangal/v5/util/string"
	"github.com/skratchdot/open-golang/open"
)

// errNoMetadata is a download with download.metadata.strict of a manga without valid
// metadata, libmangal would refuse it.
var errNoMetadata = func() error {
	open, change := base.ConfigKeys()
	return base.ConfigError(fmt.Errorf("no valid metadata (no AniList match?): bind one with the AniList screen (%s), or press %s to turn off download.metadata.strict in the config (%s from any screen)",
		base.KeyBindings.Help().Key, change, open), config.Download.Metadata.Strict.Key)
}()

// downloadMsg is a confirmed download: of one chapter (to read it after if read), or of
// the toggled ones (their job's view is opened).
type downloadMsg struct {
	item     *item
	read     bool
	chapters []mangadata.Chapter
}

const busyNotice = "Wait for the running download or read"

// target is the toggled chapter, else the hovered one.
func (s *state) target(hovered *item) *item {
	if len(s.selected) == 1 {
		for i := range s.selected {
			return i
		}
	}
	return hovered
}

func (s *state) askDownload(hovered *item) tea.Cmd {
	if size := len(s.selected); size > 1 {
		items := slices.Collect(maps.Keys(s.selected))
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].chapter.Info().Number < items[j].chapter.Info().Number
		})
		chapters := make([]mangadata.Chapter, len(items))
		for i, it := range items {
			chapters[i] = it.chapter
		}
		msg := "Download " + stringutil.Quantify(size, "chapter", "chapters") + "?"
		s.Confirm.Ask("Download", msg, s.View(), func() tea.Msg { return downloadMsg{chapters: chapters} })
		return nil
	}

	i := s.target(hovered)
	if reason := i.unavailable(); reason != "" {
		return base.Notify("Chapter unavailable: " + reason)
	}
	msg := "Download chapter " + stringutil.FormatFloat32(i.chapter.Info().Number) + ` ("` + i.chapter.Info().Title + `")?`
	s.Confirm.Ask("Download", msg, s.View(), func() tea.Msg { return downloadMsg{item: i} })
	return nil
}

func (s *state) askRead(ctx context.Context, hovered *item) tea.Cmd {
	if base.Busy(ctx) {
		return base.Notify(busyNotice)
	}
	if len(s.selected) > 1 {
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
	msg := "Download chapter " + stringutil.FormatFloat32(i.chapter.Info().Number) + ` ("` + i.chapter.Info().Title + `") for reading?`
	s.Confirm.Ask("Download", msg, s.View(), func() tea.Msg { return downloadMsg{item: i, read: true} })
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

// download queues the download as a job (tui/jobs), it goes on in the background. A batch
// stays here with a notice of how to see it; reading opens the reader when it's done, if
// this screen is shown.
func (s *state) download(ctx context.Context, msg downloadMsg) tea.Cmd {
	options := downloadOptions(msg.read)
	if err := checkMetadata(s.manga, options); err != nil {
		return func() tea.Msg { return err }
	}
	chapters := msg.chapters
	if msg.item != nil {
		chapter := msg.item.chapter
		if path := msg.item.downloaded[options.Format]; path != "" && !msg.read {
			return base.Notify(fmt.Sprintf("Chapter %q already downloaded in %s format", chapter, options.Format))
		}
		chapters = []mangadata.Chapter{chapter}
	}
	provider, mangaID := s.client.Info().ID, s.manga.Info().ID
	var fresh []mangadata.Chapter
	for _, c := range chapters {
		if _, ok := tuijobs.Manager().ChapterStatus(provider, mangaID, c); !ok {
			fresh = append(fresh, c)
		}
	}
	switch {
	case len(fresh) == 0 && len(chapters) == 1:
		return base.Notify(fmt.Sprintf("Chapter %q is already being downloaded (%s: downloads)", chapters[0], base.KeyDownloads.Help().Key))
	case len(fresh) == 0:
		return base.Notify("Those chapters are already being downloaded")
	}
	batch := msg.item == nil && !msg.read
	job := tuijobs.Download(s.client, s.manga, fresh, options, msg.read, s.Key(), batch)
	if msg.read {
		s.reading = job.ID
		return nil
	}
	if batch {
		// they're queued, the marks show them
		s.unselectAll()
		return base.NotifyWithDuration(batchNotice(job), batchNoticeDuration)
	}
	return nil
}

// batchNoticeDuration is long enough to read the hint.
const batchNoticeDuration = 4 * time.Second

// batchNotice says the batch is queued and how to open its view: the Downloads screen
// selects the job just added.
func batchNotice(job jobs.Job) string {
	what := "Queued " + stringutil.Quantify(job.Total, "chapter", "chapters")
	if job.Status == jobs.Queued && job.Behind != "" {
		what += " behind " + job.Behind
	}
	return fmt.Sprintf("%s (%s downloads, %s opens it)", what, base.KeyDownloads.Help().Key, downloads.KeyOpen.Help().Key)
}

// onJobs refreshes the marks when a download of the manga is over, and opens the chapter
// downloaded for reading.
func (s *state) onJobs(ctx context.Context, msg tuijobs.ChangedMsg) tea.Cmd {
	var cmds []tea.Cmd
	if msg.FinishedFor(s.client.Info().ID, s.manga.Info().ID) {
		cmds = append(cmds, s.refresh(ctx, false))
	}
	for _, j := range msg.Finished {
		if j.ID != s.reading {
			continue
		}
		s.reading = 0
		if j.Status != jobs.Done {
			break
		}
		for _, r := range j.Results.Succeed() {
			log.Info("downloaded chapter %q at %q", r.Chapter, r.Down.Path())
			cmds = append(cmds, s.readCmd(ctx, r.Down.Path(), r.Chapter))
		}
	}
	return tea.Batch(cmds...)
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

// openURLCmd opens the chapter's page in the browser. Only a web url: the provider says
// what it is, a path or a UNC share would be opened as well.
func openURLCmd(chapter mangadata.Chapter) tea.Cmd {
	url := chapter.Info().URL
	if !webURL(url) {
		return base.Notify("Not a web page: " + url)
	}
	return func() tea.Msg {
		if err := open.Start(url); err != nil {
			return fmt.Errorf("open %s: %w", url, err)
		}
		return nil
	}
}

func webURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
