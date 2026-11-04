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
	"github.com/luevano/mangal/v5/config"
	"github.com/luevano/mangal/v5/core"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/storage"
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
	if base.CurrentRemote != nil {
		read := s.readRemoteCmd(ctx, i)
		// read.download_on_read: the server downloads it too, in its download.format
		if config.Read.DownloadOnRead.Get() && len(i.downloaded) == 0 {
			return tea.Batch(read, s.download(ctx, downloadMsg{item: i}))
		}
		return read
	}
	if i.readPath != "" {
		log.Debug("read format of %q already downloaded", i.chapter)
		// its sync finds the ids in the metadata
		if cmd, waiting := s.afterLookup(ctx, readFileMsg{i.readPath, i.chapter}); waiting {
			return cmd
		}
		return s.readCmd(ctx, i.readPath, i.chapter)
	}
	msg := "Download chapter " + stringutil.FormatFloat32(i.chapter.Info().Number) + ` ("` + i.chapter.Info().Title + `") for reading?`
	s.Confirm.Ask("Download", msg, s.View(), func() tea.Msg { return downloadMsg{item: i, read: true} })
	return nil
}

// afterLookup is msg again once the manga's metadata search is over, when one is running:
// what uses the metadata needs its result. Dropped if the screen is left meanwhile.
func (s *state) afterLookup(ctx context.Context, msg tea.Msg) (tea.Cmd, bool) {
	pending := base.PendingLookup(s.manga)
	if pending == nil {
		return nil, false
	}
	return base.Load(ctx, fmt.Sprintf("Waiting for the metadata of %q", s.manga), func(ctx context.Context) (struct{}, error) {
		select {
		case <-pending:
			return struct{}{}, nil
		case <-ctx.Done():
			return struct{}{}, ctx.Err()
		}
	}, func(struct{}) tea.Msg { return msg }), true
}

// downloadOptions are the core's, for reading if read.
func downloadOptions(read bool) libmangal.DownloadOptions {
	options := core.DownloadOptions(read)
	// searched when the manga was opened, a search here would replace a bound one
	options.SearchMetadata = false
	return options
}

// checkMetadata fails like libmangal would without valid metadata in strict mode, saying
// what to do.
func checkMetadata(manga mangadata.Manga, options libmangal.DownloadOptions) error {
	// a server's download looks it up and checks it there
	if !options.Strict || base.CurrentRemote != nil {
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
// this screen is shown. While the manga's metadata search runs it waits for it past this
// screen: the user confirmed the download, leaving doesn't drop it.
func (s *state) download(ctx context.Context, msg downloadMsg) tea.Cmd {
	options := downloadOptions(msg.read)
	chapters := msg.chapters
	if msg.item != nil {
		chapter := msg.item.chapter
		if path := msg.item.downloaded[options.Format]; path != "" && !msg.read {
			return base.Notify(fmt.Sprintf("Chapter %q already downloaded in %s format", chapter, options.Format))
		}
		chapters = []mangadata.Chapter{chapter}
	}
	batch := msg.item == nil && !msg.read
	// a remote server's jobs answer over the network: not in Update
	queue := func(ctx context.Context, silent bool) (tea.Msg, error) {
		if err := checkMetadata(s.manga, options); err != nil {
			return nil, err
		}
		provider, mangaID := s.client.Info().ID, s.manga.Info().ID
		var fresh []mangadata.Chapter
		for _, c := range chapters {
			if _, ok := tuijobs.Service().ChapterStatus(provider, mangaID, core.ChapterRefOf(c)); !ok {
				fresh = append(fresh, c)
			}
		}
		switch {
		case len(fresh) == 0 && len(chapters) == 1:
			return base.NotificationMsg{Message: fmt.Sprintf("Chapter %q is already being downloaded (%s: downloads)", chapters[0], base.KeyDownloads.Help().Key)}, nil
		case len(fresh) == 0:
			return base.NotificationMsg{Message: "Those chapters are already being downloaded"}, nil
		}
		refs := make([]core.ChapterRef, len(fresh))
		for i, c := range fresh {
			refs[i] = core.ChapterRefOf(c)
		}
		req := core.DownloadReq{Provider: provider, MangaID: mangaID, ChapterRefs: refs, Read: msg.read, Origin: s.Key(),
			Have: &core.DownloadHave{Client: s.client, Manga: s.manga, Chapters: fresh}}
		job, err := tuijobs.Download(ctx, req, silent)
		return queuedMsg{job: job, read: msg.read, batch: batch, first: fresh[0]}, err
	}
	same := func(m tea.Msg) tea.Msg { return m }
	pending := base.PendingLookup(s.manga)
	if pending == nil {
		return base.Load(ctx, "", func(ctx context.Context) (tea.Msg, error) { return queue(ctx, batch) }, same)
	}
	return base.Load(base.Background(ctx), fmt.Sprintf("Waiting for the metadata of %q", s.manga), func(ctx context.Context) (tea.Msg, error) {
		select {
		case <-pending:
			// the screen may be gone: the job's own notice says it's queued
			return queue(ctx, false)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, same)
}

// queuedMsg is a download queued.
type queuedMsg struct {
	job         core.Job
	read, batch bool
	first       mangadata.Chapter
}

func (s *state) onQueued(msg queuedMsg) tea.Cmd {
	if msg.read {
		s.reading, s.readChapter = msg.job.ID, msg.first
		return nil
	}
	if msg.batch {
		// they're queued, the marks show them
		s.unselectAll()
		return base.NotifyWithDuration(batchNotice(msg.job), batchNoticeDuration)
	}
	return nil
}

// batchNoticeDuration is long enough to read the hint.
const batchNoticeDuration = 4 * time.Second

// batchNotice says the batch is queued and how to open its view: the Downloads screen
// selects the job just added.
func batchNotice(job core.Job) string {
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
		for _, r := range j.Results {
			if r.Succeed() {
				log.Info("downloaded chapter %q at %q", s.readChapter, r.Path)
				cmds = append(cmds, s.readCmd(ctx, r.Path, s.readChapter))
			}
		}
	}
	return tea.Batch(cmds...)
}

// readMsg is a chapter opened for reading, err is from opening or syncing it.
type readMsg struct{ err error }

// readFileMsg is a downloaded chapter to read, its metadata search over.
type readFileMsg struct {
	path    string
	chapter mangadata.Chapter
}

func (s *state) readCmd(ctx context.Context, path string, chapter mangadata.Chapter) tea.Cmd {
	c := s.client
	return base.Load(ctx, fmt.Sprintf("Opening %q for reading", chapter), func(ctx context.Context) (readMsg, error) {
		// a failed sync still opened (and recorded) it, the marks refresh either way
		return readMsg{err: client.ReadChapter(ctx, c, path, chapter)}, nil
	}, func(msg readMsg) tea.Msg { return msg })
}

// readRemoteCmd fetches the chapter from the server into the read cache (its file in the
// library, else its pages written in read.format), opens it and records the read there.
func (s *state) readRemoteCmd(ctx context.Context, i *item) tea.Cmd {
	rc, cache := base.CurrentRemote.Client, base.ReadCache()
	file := i.readPath
	if file == "" {
		for _, p := range i.downloaded {
			file = p
			break
		}
	}
	chapter, manga, provider, libraryManga := i.chapter, s.manga.Info(), s.client.Info().ID, s.libraryManga
	info, volume, format := chapter.Info(), chapter.Volume().Info().Number, config.Read.Format.Get()
	return base.Load(ctx, fmt.Sprintf("Fetching %q for reading", chapter), func(ctx context.Context) (readMsg, error) {
		var path string
		var err error
		if file != "" && libraryManga != 0 {
			path, err = rc.ChapterFile(ctx, cache, libraryManga, file)
		} else {
			path, err = rc.OnlineChapter(ctx, cache, provider, manga.ID, volume, info.Number, manga.Title+" "+stringutil.FormatFloat32(info.Number), format)
		}
		if err != nil {
			return readMsg{}, err
		}
		log.Info("opening chapter %q from %s", chapter, path)
		if err := openFile(path); err != nil {
			return readMsg{}, fmt.Errorf("open %s: %w", path, err)
		}
		err = rc.RecordRead(ctx, storage.Read{Provider: provider, MangaID: manga.ID, MangaTitle: manga.Title, Volume: volume,
			Chapter: info.Number, ChapterTitle: info.Title, ChapterURL: info.URL, LibraryManga: libraryManga})
		if err != nil {
			err = fmt.Errorf("opened, but the read isn't recorded on the server: %w", err)
		}
		return readMsg{err: err}, nil
	}, func(msg readMsg) tea.Msg { return msg })
}

// openFile opens a chapter with the default app. Swapped in tests.
var openFile = open.Run

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
