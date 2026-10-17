package watch

import (
	"errors"

	"github.com/luevano/mangal/v5/storage"
)

// The JSON bodies of the `mangal serve` API (serve/), besides Watch, Report, CheckRun and
// Status.

// AddRequest is POST /api/watches, AddOptions in JSON.
type AddRequest struct {
	Offline   bool     `json:"offline,omitempty"`
	Title     string   `json:"title,omitempty"`
	Enabled   *bool    `json:"enabled,omitempty"`
	Provider  string   `json:"provider"`
	MangaID   string   `json:"manga_id,omitempty"`
	Query     string   `json:"query,omitempty"`
	Selector  string   `json:"selector,omitempty"`
	AnilistID int      `json:"anilist_id,omitempty"`
	From      *float32 `json:"from,omitempty"`
	Gaps      *bool    `json:"gaps,omitempty"`
	Schedule  string   `json:"schedule,omitempty"`
}

func (r AddRequest) Options() AddOptions {
	return AddOptions(r)
}

func addRequest(o AddOptions) AddRequest {
	return AddRequest(o)
}

// PatchRequest is PATCH /api/watches/{id}: Enabled pauses or resumes, the rest are
// UpdateOptions. Nil keeps the value.
type PatchRequest struct {
	Enabled *bool `json:"enabled,omitempty"`
	// Schedule is a cron schedule, "" for watch.schedule.
	Schedule  *string  `json:"schedule,omitempty"`
	Gaps      *bool    `json:"gaps,omitempty"`
	AnilistID *int     `json:"anilist_id,omitempty"`
	From      *float32 `json:"from,omitempty"`
	// Skip replaces the chapter numbers never to download, [] clears them.
	Skip *[]float32 `json:"skip,omitempty"`
}

// Update are the UpdateOptions of the request, false when it changes none.
func (r PatchRequest) Update() (UpdateOptions, bool) {
	opts := UpdateOptions{Schedule: r.Schedule, Gaps: r.Gaps, AnilistID: r.AnilistID, From: r.From, Skip: r.Skip}
	return opts, opts != UpdateOptions{}
}

func patchRequest(o UpdateOptions) PatchRequest {
	return PatchRequest{Schedule: o.Schedule, Gaps: o.Gaps, AnilistID: o.AnilistID, From: o.From, Skip: o.Skip}
}

// CheckRequest is POST /api/check: the watches with the IDs (every enabled one if none).
// A dry run answers 200 with the Report, a check 202 with a CheckResponse (409 when every
// provider is being checked by another run) and runs in the server: GET /api/runs/{run} has it, finished once its
// finished time is set.
type CheckRequest struct {
	IDs    []int64 `json:"ids,omitempty"`
	Gaps   bool    `json:"gaps,omitempty"`
	DryRun bool    `json:"dry_run,omitempty"`
}

// CheckResponse is the check run a CheckRequest started, and the providers it skipped
// because another check has them.
type CheckResponse struct {
	Run  int64    `json:"run"`
	Busy []string `json:"busy,omitempty"`
}

// Error codes of an ErrorResponse, for the errors callers match.
const (
	CodeNotFound = "not_found"
	CodeExists   = "exists"
	CodeBusy     = "busy"
	CodeHeld     = "held"
	// CodeRunNotFound and CodeRunUnfinished are a not found and a busy too.
	CodeRunNotFound   = "run_not_found"
	CodeRunUnfinished = "run_unfinished"
)

// ErrorResponse is the body of an API error.
type ErrorResponse struct {
	Error string `json:"error"`
	// Code is set for the errors that match ErrNotFound, ErrExists, ErrBusy and ErrHeld (and the
	// storage run errors).
	Code string `json:"code,omitempty"`
}

// ErrorCode is the Code of err, empty if none.
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, storage.ErrRunNotFound):
		return CodeRunNotFound
	case errors.Is(err, storage.ErrRunUnfinished):
		return CodeRunUnfinished
	case errors.Is(err, ErrNotFound):
		return CodeNotFound
	case errors.Is(err, ErrExists):
		return CodeExists
	case errors.Is(err, ErrBusy):
		return CodeBusy
	case errors.Is(err, ErrHeld):
		return CodeHeld
	}
	return ""
}

// NotFound and Busy are true for the codes of a not found and of a busy.
func NotFound(code string) bool { return code == CodeNotFound || code == CodeRunNotFound }
func Busy(code string) bool     { return code == CodeBusy || code == CodeRunUnfinished }

// APIError is an error answered by the API, it matches the error of its Code.
type APIError struct {
	Status  int
	Message string
	Code    string
}

func (e *APIError) Error() string { return e.Message }

func (e *APIError) Is(target error) bool {
	switch e.Code {
	case CodeRunNotFound:
		return target == storage.ErrRunNotFound || target == ErrNotFound
	case CodeRunUnfinished:
		return target == storage.ErrRunUnfinished || target == ErrBusy
	case CodeNotFound:
		return target == ErrNotFound
	case CodeExists:
		return target == ErrExists
	case CodeBusy:
		return target == ErrBusy
	case CodeHeld:
		return target == ErrHeld
	}
	return false
}
