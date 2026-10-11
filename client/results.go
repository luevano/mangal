package client

import (
	"github.com/luevano/libmangal/mangadata"
	"github.com/luevano/libmangal/metadata"
)

// Result is a chapter of Download: what it downloaded, why it failed or why it's unavailable.
type Result struct {
	Chapter mangadata.Chapter
	Down    *metadata.DownloadedChapter
	Err     error
	// Unavailable is why the chapter wasn't tried (the provider can't download it).
	Unavailable string
}

// ToDownload is true while it's yet to be downloaded.
func (r Result) ToDownload() bool {
	return !r.Failed() && r.Down == nil && r.Unavailable == ""
}

// Succeed is true once it downloaded.
func (r Result) Succeed() bool {
	return !r.Failed() && r.Down != nil
}

// Failed is true if it failed to download.
func (r Result) Failed() bool {
	return r.Err != nil
}

// Results are the chapters of a download.
type Results []*Result

// NewResults are the chapters, all to download.
func NewResults(chapters []mangadata.Chapter) Results {
	results := make(Results, len(chapters))
	for i, ch := range chapters {
		results[i] = &Result{Chapter: ch}
	}
	return results
}

// GetEach returns the chapters to download, the downloaded and the failed ones (the
// unavailable ones are in none).
func (r Results) GetEach() (d, s, f Results) {
	for _, ch := range r {
		switch {
		case ch.ToDownload():
			d = append(d, ch)
		case ch.Succeed():
			s = append(s, ch)
		case ch.Failed():
			f = append(f, ch)
		}
	}
	return d, s, f
}

// ToDownload returns the chapters yet to be downloaded.
func (r Results) ToDownload() Results {
	return r.filter(Result.ToDownload)
}

// Succeed returns the chapters that downloaded.
func (r Results) Succeed() Results {
	return r.filter(Result.Succeed)
}

// Existent returns the downloaded chapters that already existed.
func (r Results) Existent() Results {
	return r.filter(func(ch Result) bool {
		return ch.Succeed() && ch.Down.ChapterStatus == metadata.DownloadStatusExists
	})
}

// Failed returns the chapters that failed to download.
func (r Results) Failed() Results {
	return r.filter(Result.Failed)
}

// Unavailable returns the chapters that weren't tried.
func (r Results) Unavailable() Results {
	return r.filter(func(ch Result) bool { return ch.Unavailable != "" })
}

func (r Results) filter(keep func(Result) bool) (out Results) {
	for _, ch := range r {
		if keep(*ch) {
			out = append(out, ch)
		}
	}
	return out
}
