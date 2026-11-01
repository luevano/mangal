package core

import (
	"context"
	"fmt"

	"github.com/luevano/libmangal"
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/mangal/v5/client"
	"github.com/luevano/mangal/v5/client/selector"
	"github.com/luevano/mangal/v5/core/jobs"
	"github.com/luevano/mangal/v5/log"
	"github.com/luevano/mangal/v5/path"
	stringutil "github.com/luevano/mangal/v5/util/string"
)

// DownloadReq is a download to queue: the provider's manga (MangaID, else the search of
// Query, Selector picking among its results), its chapters (the Chapters selector, else
// ChapterRefs), for reading if Read. It's plain data
// a remote jobs service sends as is; Have is the local shortcut.
type DownloadReq struct {
	Provider string `json:"provider"`
	MangaID  string `json:"manga_id,omitempty"`
	Query    string `json:"query,omitempty"`
	Selector string `json:"selector,omitempty"`
	// Chapters is a chapter selector: all, last, 12, 10-20, 30-.
	Chapters    string       `json:"chapters,omitempty"`
	ChapterRefs []ChapterRef `json:"chapter_refs,omitempty"`
	// Read is a download for reading: in the read format and, without
	// read.download_on_read, in the temp directory.
	Read bool `json:"read,omitempty"`
	// Origin is who starts it: the TUI screen's window key, "api: user kid".
	Origin string `json:"origin,omitempty"`
	// Have are the client, manga and chapters a local frontend already has: the local jobs
	// download them as they are (the manga's metadata searched or bound already, not
	// searched again), a remote service goes by the fields above.
	Have *DownloadHave `json:"-"`
}

// DownloadHave are DownloadReq.Have.
type DownloadHave struct {
	Client   *libmangal.Client
	Manga    mangadata.Manga
	Chapters []mangadata.Chapter
}

// ChapterRef is a chapter of a manga as the jobs know it (jobs.ChapterKey).
type ChapterRef struct {
	Number float32 `json:"number"`
	Title  string  `json:"title,omitempty"`
	URL    string  `json:"url,omitempty"`
}

// ChapterRefOf is the chapter's ref.
func ChapterRefOf(c mangadata.Chapter) ChapterRef {
	info := c.Info()
	return ChapterRef{Number: info.Number, Title: info.Title, URL: info.URL}
}

func (r ChapterRef) key() string { return jobs.Key(r.URL, r.Number, r.Title) }

// JobEventKind is what happened to a job.
type JobEventKind string

const (
	JobAdded    JobEventKind = "added"
	JobFinished JobEventKind = "finished"
	// JobChanged is jobs progressing, without a job.
	JobChanged JobEventKind = "changed"
)

// JobEvent is a job added or finished, or jobs changing.
type JobEvent struct {
	Seq  uint64       `json:"seq"`
	Kind JobEventKind `json:"kind"`
	Job  Job          `json:"job,omitzero"`
}

// JobsOptions are the local jobs' options.
type JobsOptions struct {
	// Find finds a download's manga and chapters (Env.ProviderChapters), mangal's own
	// providers when nil.
	Find func(ctx context.Context, provider, id, query, sel string) (*libmangal.Client, mangadata.Manga, []mangadata.Chapter, func(), error)
	// Finally runs once each download ends (the TUI's desktop notification).
	Finally func(ctx context.Context, j jobs.Job)
}

// NewJobs are the jobs of the process's manager m.
func NewJobs(m *jobs.Manager, opts JobsOptions) Jobs { return localJobs{m: m, opts: opts} }

func (l localJobs) Download(ctx context.Context, req DownloadReq) (Job, error) {
	spec, err := l.downloadSpec(ctx, req)
	if err != nil {
		return Job{}, err
	}
	return JobOf(l.m.Add(spec)), nil
}

// downloadSpec is the job of the request: what serve's POST /downloads and the TUI's
// chapters screen queue.
func (l localJobs) downloadSpec(ctx context.Context, req DownloadReq) (jobs.Spec, error) {
	have := req.Have
	if have == nil {
		switch {
		case req.MangaID == "" && req.Query == "":
			return jobs.Spec{}, invalid("manga_id or query is needed")
		case req.Chapters == "" && len(req.ChapterRefs) == 0:
			return jobs.Spec{}, invalid("chapters: a chapter selector is needed (all, last, 12, 10-20, 30-)")
		}
	}
	options := DownloadOptions(req.Read)
	if have != nil {
		options.SearchMetadata = false
	} else {
		var err error
		if have, err = l.resolve(ctx, req); err != nil {
			return jobs.Spec{}, err
		}
		// a bound manga gets its metadata now, a search would replace it
		if bound, err := client.ApplyBinding(ctx, have.Client, have.Manga); err != nil {
			log.Warn("binding of %q: %s", have.Manga, err)
		} else if bound {
			options.SearchMetadata = false
		}
	}
	title := fmt.Sprintf("%s: %s", have.Manga.Info().Title, chaptersTitle(have.Chapters))
	spec := jobs.DownloadSpec(have.Client, have.Manga, have.Chapters, options, title, req.Origin)
	spec.Read = req.Read
	if options.Directory == path.Temp() {
		run := spec.Run
		spec.Run = func(ctx context.Context, p *jobs.Progress) error {
			if _, err := path.EnsureTemp(); err != nil {
				return err
			}
			return run(ctx, p)
		}
	}
	spec.Finally = l.opts.Finally
	return spec, nil
}

// resolve finds the request's manga and chapters.
func (l localJobs) resolve(ctx context.Context, req DownloadReq) (*DownloadHave, error) {
	find := l.opts.Find
	if find == nil {
		find = providerChapters
	}
	c, manga, all, release, err := find(ctx, req.Provider, req.MangaID, req.Query, req.Selector)
	if err != nil {
		return nil, err
	}
	// the job holds the provider's client itself while it runs
	defer release()
	var chapters []mangadata.Chapter
	if req.Chapters != "" {
		if chapters, err = selector.Chapters(req.Chapters, all); err != nil {
			return nil, invalid("chapters: %s", err)
		}
		if len(chapters) == 0 {
			return nil, invalid("chapters: %q matches none", req.Chapters)
		}
	} else {
		keys := map[string]bool{}
		for _, r := range req.ChapterRefs {
			keys[r.key()] = true
		}
		found := map[string]bool{}
		for _, ch := range all {
			if k := jobs.ChapterKey(ch); keys[k] && !found[k] {
				found[k] = true
				chapters = append(chapters, ch)
			}
		}
		if len(found) != len(keys) {
			return nil, invalid("chapters: %d of the %d asked aren't listed by %s", len(keys)-len(found), len(keys), req.Provider)
		}
	}
	return &DownloadHave{Client: c, Manga: manga, Chapters: chapters}, nil
}

// chaptersTitle is "chapter 3" or "chapters 1-5 (4)".
func chaptersTitle(chapters []mangadata.Chapter) string {
	first, last := chapters[0].Info().Number, chapters[len(chapters)-1].Info().Number
	if len(chapters) == 1 {
		return "chapter " + stringutil.FormatFloat32(first)
	}
	return fmt.Sprintf("chapters %s-%s (%d)", stringutil.FormatFloat32(first), stringutil.FormatFloat32(last), len(chapters))
}

func (l localJobs) MangaStatus(provider, mangaID string) (jobs.Status, bool) {
	return l.m.MangaStatus(provider, mangaID)
}

func (l localJobs) ChapterStatus(provider, mangaID string, chapter ChapterRef) (jobs.Status, bool) {
	return l.m.KeyStatus(provider, mangaID, chapter.key())
}

func (l localJobs) Subscribe(ctx context.Context, after uint64) (<-chan JobEvent, error) {
	ch := make(chan JobEvent, 16)
	go func() {
		defer close(ch)
		first := true
		for {
			changed := l.m.Changed()
			events, seq := l.m.Events(after)
			after = seq
			for _, e := range events {
				kind := JobAdded
				if e.Kind == jobs.EventFinished {
					kind = JobFinished
				}
				select {
				case ch <- JobEvent{Seq: e.Seq, Kind: kind, Job: JobOf(e.Job)}:
				case <-ctx.Done():
					return
				}
			}
			if len(events) == 0 && !first {
				select {
				case ch <- JobEvent{Seq: seq, Kind: JobChanged}:
				default:
				}
			}
			first = false
			select {
			case <-changed:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}
